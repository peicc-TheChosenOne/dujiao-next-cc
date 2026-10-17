package integrationtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/constants"
	externalidentitydomain "github.com/dujiao-next/internal/modules/identity/externalidentity/domain"
	telegramauthapp "github.com/dujiao-next/internal/modules/identity/telegramauth/application"
	userstore "github.com/dujiao-next/internal/modules/identity/user/infrastructure/gormstore"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"
	usertotpapp "github.com/dujiao-next/internal/modules/identity/userauth/totp/application"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellerstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pquerna/otp/totp"
)

type managedResellerReader struct{ userID uint }

func (r managedResellerReader) GetLoginSecurityResellerID(userID uint) (uint, error) {
	if userID == r.userID {
		return 1, nil
	}
	return 0, nil
}

func parseResellerAccess(t *testing.T, token string) *userauthapp.UserJWTClaims {
	t.Helper()
	claims := &userauthapp.UserJWTClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (interface{}, error) { return []byte("user-jwt-test-secret-2fa"), nil })
	if err != nil {
		t.Fatal(err)
	}
	return claims
}

func TestResellerEnrollmentAndAdminLifecycle(t *testing.T) {
	auth, verifier, repo, db := newUser2FATestServices(t)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	user := createActiveUser(t, repo, "reseller@example.test", "secret123")
	reader := managedResellerReader{user.ID}
	auth.SetResellerSecurityReader(reader)
	verifier.SetResellerSecurityReader(reader)
	legacy, _, err := auth.GenerateUserJWT(user, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.ValidateLoginSession(parseResellerAccess(t, legacy)); err == nil {
		t.Fatal("legacy session bypassed required verification")
	}
	login, err := auth.LoginStep1(user.Email, "secret123", true)
	if err != nil || !login.RequiresTOTP || !login.RequiresTOTPSetup || login.Token != "" {
		t.Fatalf("enrollment login: %+v %v", login, err)
	}
	if _, err := auth.ParseUserChallengeToken(login.ChallengeToken); err == nil {
		t.Fatal("enrollment token accepted by verification endpoint")
	}
	setup, err := auth.SetupLoginEnrollment(login.ChallengeToken)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := auth.SetupLoginEnrollment(login.ChallengeToken)
	if err != nil || retry.Secret != setup.Secret {
		t.Fatal("setup retry replaced the key", err)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	bound, err := auth.CompleteLoginEnrollment(login.ChallengeToken, code)
	if err != nil {
		t.Fatal(err)
	}
	if len(bound.RecoveryCodes) != 10 {
		t.Fatal("missing recovery codes")
	}
	claims := parseResellerAccess(t, bound.Login.Token)
	if !claims.TwoFactorVerified || auth.ValidateLoginSession(claims) != nil {
		t.Fatal("bound login is not authorized")
	}
	if _, err := auth.CompleteLoginEnrollment(login.ChallengeToken, code); err == nil {
		t.Fatal("binding was replayed")
	}
	challenge, err := auth.LoginStep1(user.Email, "secret123", false)
	if err != nil || !challenge.RequiresTOTP || challenge.RequiresTOTPSetup || challenge.Token != "" {
		t.Fatal("bound reseller skipped verification", err)
	}
	if _, err := verifier.Setup(user.ID); !errors.Is(err, usertotpapp.ErrManaged) {
		t.Fatal("self setup allowed", err)
	}
	if err := verifier.Disable(user.ID, code, false); !errors.Is(err, usertotpapp.ErrManaged) {
		t.Fatal("self disable allowed", err)
	}
	if _, err := verifier.AdminResetUser2FA(1, user.ID); !errors.Is(err, usertotpapp.ErrManaged) {
		t.Fatal("legacy admin reset bypassed policy", err)
	}
	paused, err := auth.UpdateResellerLoginSecurity(1, user.ID, false, false, "verified support request")
	if err != nil || paused.TOTPEnabledAt == nil || paused.RecoveryCodes == "" {
		t.Fatal("pause removed binding", err)
	}
	if auth.ValidateLoginSession(claims) == nil {
		t.Fatal("pause failed to revoke the old session")
	}
	if _, err := auth.ParseUserChallengeToken(challenge.ChallengeToken); err == nil {
		t.Fatal("old challenge survived policy change")
	}
	status, err := verifier.GetStatus(user.ID)
	if err != nil || !status.PlatformManaged || status.VerificationRequired {
		t.Fatal("wrong paused status", err)
	}
	login, err = auth.LoginStep1(user.Email, "secret123", false)
	if err != nil || login.RequiresTOTP || login.Token == "" {
		t.Fatal("paused account still challenged", err)
	}
	pausedClaims := parseResellerAccess(t, login.Token)
	if pausedClaims.TwoFactorVerified || auth.ValidateLoginSession(pausedClaims) != nil {
		t.Fatal("paused session not usable")
	}
	if _, err := auth.UpdateResellerLoginSecurity(1, user.ID, true, false, "restore protection"); err != nil {
		t.Fatal(err)
	}
	if auth.ValidateLoginSession(pausedClaims) == nil {
		t.Fatal("pause-issued session survived reactivation")
	}
	reset, err := auth.UpdateResellerLoginSecurity(1, user.ID, false, true, "identity checked; authenticator lost")
	if err != nil || reset.ResellerTOTPExempt || reset.TOTPEnabledAt != nil || reset.RecoveryCodes != "" || reset.TOTPPendingSecret != "" {
		t.Fatal("reset did not clear and re-enable protection", err)
	}
	login, err = auth.LoginStep1(user.Email, "secret123", false)
	if err != nil || !login.RequiresTOTPSetup {
		t.Fatal("reset did not require enrollment", err)
	}
	setup, err = auth.SetupLoginEnrollment(login.ChallengeToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.UpdateResellerLoginSecurity(1, user.ID, true, true, "repeat reset during enrollment"); err != nil {
		t.Fatal(err)
	}
	code, _ = totp.GenerateCode(setup.Secret, time.Now())
	if _, err := auth.CompleteLoginEnrollment(login.ChallengeToken, code); err == nil {
		t.Fatal("enrollment survived admin reset")
	}
}

func TestResellerEnrollmentFailureBudgetAndExpiry(t *testing.T) {
	auth, _, repo, _ := newUser2FATestServices(t)
	user := createActiveUser(t, repo, "failures@example.test", "secret123")
	auth.SetResellerSecurityReader(managedResellerReader{user.ID})
	login, err := auth.LoginStep1(user.Email, "secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	setup, err := auth.SetupLoginEnrollment(login.ChallengeToken)
	if err != nil {
		t.Fatal(err)
	}
	correct, _ := totp.GenerateCode(setup.Secret, time.Now())
	wrong := "000000"
	if correct == wrong {
		wrong = "111111"
	}
	for i := 0; i < 5; i++ {
		if _, err := auth.CompleteLoginEnrollment(login.ChallengeToken, wrong); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if _, err := auth.CompleteLoginEnrollment(login.ChallengeToken, correct); err == nil {
		t.Fatal("failure budget did not revoke enrollment")
	}
	login, err = auth.LoginStep1(user.Email, "secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	claims := &userauthapp.UserChallengeClaims{}
	_, err = jwt.ParseWithClaims(login.ChallengeToken, claims, func(*jwt.Token) (interface{}, error) { return []byte("user-jwt-test-secret-2fa"), nil })
	if err != nil {
		t.Fatal(err)
	}
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("user-jwt-test-secret-2fa"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.SetupLoginEnrollment(expired); err == nil {
		t.Fatal("expired enrollment accepted")
	}
}

func TestResellerRecoveryCodeConsumedOnceConcurrently(t *testing.T) {
	auth, verifier, repo, db := newUser2FATestServices(t)
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	user := createActiveUser(t, repo, "recovery-race@example.test", "secret123")
	auth.SetResellerSecurityReader(managedResellerReader{user.ID})
	login, _ := auth.LoginStep1(user.Email, "secret123", false)
	setup, err := auth.SetupLoginEnrollment(login.ChallengeToken)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.GenerateCode(setup.Secret, time.Now())
	result, err := auth.CompleteLoginEnrollment(login.ChallengeToken, code)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- verifier.VerifyChallengeRecoveryCode(user.ID, result.RecoveryCodes[0])
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("recovery code succeeded %d times", successes)
	}
}

func TestApprovalActivatesPolicyForExistingSessions(t *testing.T) {
	auth, _, repo, db := newUser2FATestServices(t)
	if err := db.AutoMigrate(&resellerdomain.Profile{}); err != nil {
		t.Fatal(err)
	}
	store := resellerstore.New(db)
	auth.SetResellerSecurityReader(store)
	user := createActiveUser(t, repo, "approval@example.test", "secret123")
	profile := &resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusPendingReview}
	if err := store.CreateProfile(profile); err != nil {
		t.Fatal(err)
	}
	token, _, err := auth.GenerateUserJWT(user, 24)
	if err != nil {
		t.Fatal(err)
	}
	claims := parseResellerAccess(t, token)
	if err := auth.ValidateLoginSession(claims); err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{resellerdomain.ProfileStatusActive, resellerdomain.ProfileStatusDisabled} {
		if err := db.Model(profile).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		if err := auth.ValidateLoginSession(claims); err == nil {
			t.Fatal("unverified session accepted after status change", status)
		}
	}
}

func TestResellerGoogleLoginRequiresEnrollment(t *testing.T) {
	auth, _, db := setupTelegramOAuthTestService(t)
	user := createActiveUser(t, userstore.New(db), "reseller-google@gmail.com", "secret123")
	auth.SetResellerSecurityReader(managedResellerReader{user.ID})
	result, err := auth.LoginVerifiedGoogle(verifiedGoogleIdentity(user.Email, "reseller-google-subject", true))
	if err != nil || !result.RequiresTOTP || !result.RequiresTOTPSetup || result.Token != "" {
		t.Fatalf("Google bypassed enrollment: %+v %v", result, err)
	}
}

func TestResellerTelegramMiniAppRequiresEnrollmentWithoutRealEmail(t *testing.T) {
	telegram := telegramauthapp.NewService(config.TelegramAuthConfig{Enabled: true, BotToken: "test-reseller-bot", LoginExpireSeconds: 300, ReplayTTLSeconds: 300}, telegramauthapp.WithReplaySetNX(func(context.Context, string, interface{}, time.Duration) (bool, error) { return true, nil }))
	auth, _, db := setupTelegramOAuthTestService(t, telegram)
	user := createActiveUser(t, userstore.New(db), "telegram_4242@login.local", "secret123")
	if err := db.Create(&externalidentitydomain.Identity{UserID: user.ID, Provider: constants.UserOAuthProviderTelegram, ProviderUserID: "4242"}).Error; err != nil {
		t.Fatal(err)
	}
	auth.SetResellerSecurityReader(managedResellerReader{user.ID})
	result, err := auth.LoginWithTelegramMiniApp(userauthapp.LoginWithTelegramMiniAppInput{Context: context.Background(), InitData: buildUserAuthTestTelegramMiniAppInitData(t, "test-reseller-bot", time.Now().Unix(), `{"id":4242,"first_name":"Reseller"}`)})
	if err != nil || !result.RequiresTOTPSetup || result.Token != "" {
		t.Fatalf("Telegram bypassed enrollment: %+v %v", result, err)
	}
	if _, err := auth.SetupLoginEnrollment(result.ChallengeToken); err != nil {
		t.Fatal("virtual-email enrollment failed", err)
	}
}

func TestApprovalRevokesPreviouslyVerifiedOrdinarySession(t *testing.T) {
	auth, _, repo, db := newUser2FATestServices(t)
	if err := db.AutoMigrate(&resellerdomain.Profile{}); err != nil {
		t.Fatal(err)
	}
	user := createActiveUser(t, repo, "verified-approval@example.test", "secret123")
	now := time.Now()
	if err := db.Model(user).Update("totp_enabled_at", now).Error; err != nil {
		t.Fatal(err)
	}
	result, err := auth.CompleteLoginAfter2FA(user.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	claims := parseResellerAccess(t, result.Token)
	reviewed := time.Now()
	profile := &resellerdomain.Profile{UserID: user.ID, Status: resellerdomain.ProfileStatusActive, ReviewedAt: &reviewed}
	store := resellerstore.New(db)
	if err := store.CreateProfile(profile); err != nil {
		t.Fatal(err)
	}
	auth.SetResellerSecurityReader(store)
	if err := auth.ValidateLoginSession(claims); err == nil {
		t.Fatal("session from before approval remained usable")
	}
	for _, offset := range []int64{-1, 0, 1} {
		session := *claims
		session.AuthenticatedAt = reviewed.UnixNano() + offset
		err := auth.ValidateLoginSession(&session)
		if offset <= 0 && !errors.Is(err, userauthapp.ErrLoginSecurity) {
			t.Fatalf("approval cutoff offset %d: expected session rejection, got %v", offset, err)
		}
		if offset > 0 && err != nil {
			t.Fatalf("session authenticated after approval rejected: %v", err)
		}
	}
}
