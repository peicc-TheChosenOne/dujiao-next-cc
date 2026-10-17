package application

import (
	"context"
	"errors"
	"fmt"
	urlpkg "net/url"
	"strings"
	"time"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/crypto"
	"github.com/dujiao-next/internal/logger"
	totpapp "github.com/dujiao-next/internal/modules/identity/totp/application"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	"github.com/dujiao-next/internal/modules/identity/userauth/challenge"
	"github.com/dujiao-next/internal/shared/mailbrand"
	"github.com/dujiao-next/internal/telegramidentity"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

var ErrLoginSecurity = errors.New("login security state changed or verification required")
var ErrManagedTwoFactor = errors.New("two-factor authentication is managed by the platform")

type ResellerSecurityReader interface {
	GetLoginSecurityResellerID(userID uint) (uint, error)
}

func (s *Service) SetResellerSecurityReader(reader ResellerSecurityReader) { s.securityReader = reader }

// LoginSecurity always reads the current role. Losing a cache cannot grant access.
func (s *Service) LoginSecurity(userID uint) (managed, required bool, err error) {
	if s.securityReader == nil {
		return false, false, nil
	}
	id, err := s.securityReader.GetLoginSecurityResellerID(userID)
	if err != nil || id == 0 {
		return false, false, err
	}
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return false, false, err
	}
	if user == nil {
		return false, false, ErrNotFound
	}
	return true, !user.ResellerTOTPExempt, nil
}

func (s *Service) ValidateLoginSession(claims *UserJWTClaims) error {
	if s.securityReader == nil {
		return nil
	}
	var id uint
	var reviewedAt *time.Time
	var err error
	if reader, ok := s.securityReader.(interface {
		GetResellerLoginRole(uint) (uint, *time.Time, error)
	}); ok {
		id, reviewedAt, err = reader.GetResellerLoginRole(claims.UserID)
	} else {
		id, err = s.securityReader.GetLoginSecurityResellerID(claims.UserID)
	}
	if err != nil {
		return err
	}
	if id == 0 {
		return nil
	}
	if reviewedAt != nil && claims.AuthenticatedAt <= reviewedAt.UnixNano() {
		return ErrLoginSecurity
	}
	user, err := s.userRepo.GetByID(claims.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.Status != constants.UserStatusActive || user.TokenVersion != claims.TokenVersion {
		return ErrLoginSecurity
	}
	if !user.ResellerTOTPExempt && (!claims.TwoFactorVerified || user.TOTPEnabledAt == nil) {
		return ErrLoginSecurity
	}
	return nil
}

func (s *Service) prepareSecondFactor(user *userdomain.User, remember bool, source string) (*UserLoginResult, error) {
	managed, required, err := s.LoginSecurity(user.ID)
	if err != nil {
		return nil, err
	}
	if !required && (managed || user.TOTPEnabledAt == nil) {
		return nil, nil
	}
	setup := user.TOTPEnabledAt == nil
	token, jti, expires, err := s.issueUserChallenge(user, remember, source, setup)
	if err != nil {
		return nil, err
	}
	return &UserLoginResult{RequiresTOTP: true, RequiresTOTPSetup: setup, User: user,
		ChallengeToken: token, ChallengeJTI: jti, ChallengeExpiresAt: expires}, nil
}

func (s *Service) CompleteLoginAfter2FAVersion(userID uint, remember bool, version uint64) (*UserLoginResult, error) {
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.TokenVersion != version || user.TOTPEnabledAt == nil || user.Status != constants.UserStatusActive {
		return nil, ErrLoginSecurity
	}
	if store, ok := s.userRepo.(usercontract.LoginSecurityStore); ok {
		now := time.Now()
		changed, err := store.TouchLoginIfCurrent(userID, version, now)
		if err != nil {
			return nil, err
		}
		if !changed {
			return nil, ErrLoginSecurity
		}
		user.LastLoginAt = &now
	}
	_ = cache.DelUserAuthState(context.Background(), userID)
	hours := resolveUserJWTExpireHours(s.cfg.UserJWT)
	if remember {
		hours = resolveRememberMeExpireHours(s.cfg.UserJWT)
	}
	token, expires, err := s.generateUserJWT(user, hours, true)
	if err != nil {
		return nil, err
	}
	return &UserLoginResult{User: user, Token: token, ExpiresAt: expires}, nil
}

func (s *Service) UpdateResellerLoginSecurity(adminID, userID uint, required, reset bool, reason string) (*userdomain.User, error) {
	reason = strings.TrimSpace(reason)
	if adminID == 0 || reason == "" || len([]rune(reason)) > 500 {
		return nil, ErrLoginSecurity
	}
	managed, _, err := s.LoginSecurity(userID)
	if err != nil {
		return nil, err
	}
	if !managed {
		return nil, ErrManagedTwoFactor
	}
	user, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, ErrNotFound
	}
	action := "require"
	if !required {
		action = "pause"
	}
	if reset {
		action = "reset"
	}
	if user.ResellerSecurityAction == action && user.ResellerSecurityReason == reason && user.ResellerSecurityAdminID == adminID && (!reset || (user.TOTPEnabledAt == nil && user.TOTPPendingSecret == "" && user.RecoveryCodes == "")) {
		return user, nil
	}
	store, ok := s.userRepo.(usercontract.LoginSecurityStore)
	if !ok {
		return nil, ErrLoginSecurity
	}
	changed, err := store.UpdateResellerLoginSecurity(userID, user.TokenVersion, required, reset, adminID, reason, time.Now())
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrLoginSecurity
	}
	_ = cache.DelUserAuthState(context.Background(), userID)
	updated, err := s.userRepo.GetByID(userID)
	if err != nil {
		return nil, err
	}
	if updated != nil && updated.EmailVerifiedAt != nil && !telegramidentity.IsPlaceholderEmail(updated.Email) {
		if sender, ok := s.emailService.(interface {
			SendLoginSecurityNotice(string, string, string, mailbrand.Brand) error
		}); ok {
			if err := sender.SendLoginSecurityNotice(updated.Email, updated.ResellerSecurityAction, updated.Locale, mailbrand.Brand{}); err != nil {
				logger.Warnw("reseller_security_notification_failed", "user_id", updated.ID)
			}
		}
	}
	return updated, nil
}

type EnrollmentSetup struct {
	Secret     string    `json:"secret"`
	OtpauthURL string    `json:"otpauth_url"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type EnrollmentResult struct {
	Login         *UserLoginResult
	RecoveryCodes []string
}

func (s *Service) enrollmentSubject(token string) (*UserChallengeClaims, *userdomain.User, error) {
	claims, err := s.parseLoginChallenge(token, true)
	if err != nil {
		return nil, nil, err
	}
	if cache.HasChallengeFlag(context.Background(), challenge.RevocationKey(claims.JTI)) {
		return nil, nil, ErrLoginSecurity
	}
	managed, required, err := s.LoginSecurity(claims.UserID)
	if err != nil {
		return nil, nil, err
	}
	if !managed || !required {
		return nil, nil, ErrLoginSecurity
	}
	user, err := s.userRepo.GetByID(claims.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || user.TokenVersion != claims.TokenVersion || user.TOTPEnabledAt != nil {
		return nil, nil, ErrLoginSecurity
	}
	return claims, user, nil
}

func (s *Service) SetupLoginEnrollment(token string) (*EnrollmentSetup, error) {
	claims, user, err := s.enrollmentSubject(token)
	if err != nil {
		return nil, err
	}
	issuer := strings.TrimSpace(s.cfg.App.TOTPIssuer)
	if issuer == "" {
		issuer = "AI大玩家"
	}
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: user.Email, Period: 30, Digits: otp.DigitsSix})
	if err != nil {
		return nil, err
	}
	secret := key.Secret()
	if user.TOTPPendingChallenge == claims.JTI && user.TOTPPendingSecret != "" && user.TOTPPendingExpiresAt != nil && time.Now().Before(*user.TOTPPendingExpiresAt) {
		secret, err = crypto.Decrypt(crypto.DeriveKey(s.cfg.App.SecretKey), user.TOTPPendingSecret)
		if err != nil {
			return nil, err
		}
	} else {
		enc, err := crypto.Encrypt(crypto.DeriveKey(s.cfg.App.SecretKey), secret)
		if err != nil {
			return nil, err
		}
		store, ok := s.userRepo.(usercontract.LoginSecurityStore)
		if !ok {
			return nil, ErrLoginSecurity
		}
		changed, err := store.SetEnrollmentPending(user.ID, claims.TokenVersion, claims.JTI, enc, claims.ExpiresAt.Time)
		if err != nil {
			return nil, err
		}
		if !changed {
			return nil, ErrLoginSecurity
		}
	}
	// Build the URI locally; no secret is sent to an external QR service.
	uri, err := otp.NewKeyFromURL(key.URL())
	if err != nil {
		return nil, err
	}
	url := uri.URL()
	if secret != key.Secret() {
		parsed, err := urlpkg.Parse(url)
		if err != nil {
			return nil, err
		}
		query := parsed.Query()
		query.Set("secret", secret)
		parsed.RawQuery = query.Encode()
		url = parsed.String()
	}
	return &EnrollmentSetup{Secret: secret, OtpauthURL: url, ExpiresAt: claims.ExpiresAt.Time}, nil
}

func (s *Service) CompleteLoginEnrollment(token, code string) (*EnrollmentResult, error) {
	claims, user, err := s.enrollmentSubject(token)
	if err != nil {
		return nil, err
	}
	if user.TOTPPendingChallenge != claims.JTI {
		return nil, ErrLoginSecurity
	}
	prepared, err := totpapp.PrepareEnable(totpapp.EnableInput{
		AccountID: user.ID, EncryptionKey: crypto.DeriveKey(s.cfg.App.SecretKey), Code: code, RecoveryCodeCount: 10,
		PendingSecret: user.TOTPPendingSecret, PendingExpiresAt: user.TOTPPendingExpiresAt,
		VerifyCode: func(secret, value string) bool {
			valid, _ := totp.ValidateCustom(strings.TrimSpace(value), secret, time.Now(), totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix})
			return valid
		},
	})
	if err != nil {
		if cache.IncrChallengeCounter(context.Background(), challenge.FailureKey(claims.JTI), 10*time.Minute) >= 5 {
			cache.SetChallengeFlag(context.Background(), challenge.RevocationKey(claims.JTI), 10*time.Minute)
		}
		return nil, err
	}
	store, ok := s.userRepo.(usercontract.LoginSecurityStore)
	if !ok {
		return nil, ErrLoginSecurity
	}
	changed, err := store.CommitEnrollment(user.ID, claims.TokenVersion, claims.JTI, user.TOTPPendingSecret, prepared.EncryptedSecret, prepared.RecoveryCodesJSON, prepared.EnabledAt)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, ErrLoginSecurity
	}
	_ = cache.DelUserAuthState(context.Background(), user.ID)
	cache.SetChallengeFlag(context.Background(), challenge.RevocationKey(claims.JTI), 10*time.Minute)
	login, err := s.CompleteLoginAfter2FAVersion(user.ID, claims.RememberMe, claims.TokenVersion+1)
	if err != nil {
		return nil, fmt.Errorf("complete enrollment login: %w", err)
	}
	login.LoginSource = claims.LoginSource
	return &EnrollmentResult{Login: login, RecoveryCodes: prepared.RecoveryCodes}, nil
}
