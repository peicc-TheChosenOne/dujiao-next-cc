package integrationtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	emailverificationdomain "github.com/dujiao-next/internal/modules/identity/emailverification/domain"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const resetTestPassword = "NewPassw0rd!"

func seedAuthUser(t *testing.T, db *gorm.DB, email, password, status string, verified bool) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password failed: %v", err)
	}
	user := &userdomain.User{Email: email, PasswordHash: string(hash), Status: status}
	if verified {
		now := time.Now()
		user.EmailVerifiedAt = &now
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
}

func seedResetCode(t *testing.T, db *gorm.DB, email, code string) {
	t.Helper()
	now := time.Now()
	record := &emailverificationdomain.Code{
		Email: email, Purpose: constants.VerifyPurposeReset, Code: code,
		ExpiresAt: now.Add(10 * time.Minute), SentAt: now, CreatedAt: now,
	}
	if err := db.Create(record).Error; err != nil {
		t.Fatalf("create verification code failed: %v", err)
	}
}

func TestResetPasswordConcurrentGuessesRespectAttemptLimit(t *testing.T) {
	svc, _, db := newRegistrationDomainPolicyAuthService(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db failed: %v", err)
	}
	sqlDB.SetMaxOpenConns(1) // 与生产 SQLite 单连接一致
	seedAuthUser(t, db, "victim@example.com", "secret123", constants.UserStatusActive, true)
	seedResetCode(t, db, "victim@example.com", "123456")

	const guesses = 20
	results := make(chan error, guesses)
	var wg sync.WaitGroup
	for i := 0; i < guesses; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- svc.ResetPassword("victim@example.com", "000000", resetTestPassword)
		}()
	}
	wg.Wait()
	close(results)

	checked := 0
	for err := range results {
		switch {
		case errors.Is(err, userauthapp.ErrVerifyCodeInvalid):
			checked++
		case errors.Is(err, userauthapp.ErrVerifyCodeAttemptsExceeded):
		default:
			t.Fatalf("unexpected reset result: %v", err)
		}
	}
	if checked != 5 {
		t.Fatalf("concurrent guesses compared against the code %d times, want the 5-attempt limit", checked)
	}
	if err := svc.ResetPassword("victim@example.com", "123456", resetTestPassword); !errors.Is(err, userauthapp.ErrVerifyCodeAttemptsExceeded) {
		t.Fatalf("correct code after exhausting attempts = %v, want ErrVerifyCodeAttemptsExceeded", err)
	}
}

func TestResetPasswordCodeIsSingleUse(t *testing.T) {
	svc, _, db := newRegistrationDomainPolicyAuthService(t)
	seedAuthUser(t, db, "buyer@example.com", "secret123", constants.UserStatusActive, true)
	seedResetCode(t, db, "buyer@example.com", "123456")

	if err := svc.ResetPassword("buyer@example.com", "123456", resetTestPassword); err != nil {
		t.Fatalf("first reset failed: %v", err)
	}
	if err := svc.ResetPassword("buyer@example.com", "123456", resetTestPassword); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("reusing reset code = %v, want ErrVerifyCodeInvalid", err)
	}
}

func TestPasswordResetDoesNotRevealUnknownEmail(t *testing.T) {
	svc, _, db := newRegistrationDomainPolicyAuthService(t)

	if err := svc.SendVerifyCode(context.Background(), "ghost@example.com", constants.VerifyPurposeReset, constants.LocaleZhCN); err != nil {
		t.Fatalf("send reset code to unknown email = %v, want the same success as a known email", err)
	}
	var count int64
	if err := db.Model(&emailverificationdomain.Code{}).Where("email = ?", "ghost@example.com").Count(&count).Error; err != nil {
		t.Fatalf("count codes failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("unknown email must not get a reset code, got %d", count)
	}
	if err := svc.ResetPassword("ghost@example.com", "123456", resetTestPassword); !errors.Is(err, userauthapp.ErrVerifyCodeInvalid) {
		t.Fatalf("reset unknown email = %v, want ErrVerifyCodeInvalid", err)
	}
}

func TestPasswordResetSendResponsesRemainUniformDuringCooldown(t *testing.T) {
	svc, _, db := newRegistrationDomainPolicyAuthService(t)
	seedAuthUser(t, db, "known@example.test", "secret123", constants.UserStatusActive, true)
	for i := 0; i < 2; i++ {
		for _, email := range []string{"known@example.test", "unknown@example.test"} {
			if err := svc.SendVerifyCode(context.Background(), email, constants.VerifyPurposeReset, constants.LocaleZhCN); err != nil {
				t.Fatalf("reset send %d returned distinguishable error: %v", i, err)
			}
		}
	}
	var count int64
	if err := db.Model(&emailverificationdomain.Code{}).Where("purpose = ?", constants.VerifyPurposeReset).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("cooldown must suppress duplicate emails without creating unknown-email records: count=%d err=%v", count, err)
	}
}

func TestLoginStep1RevealsAccountStateOnlyAfterPassword(t *testing.T) {
	svc, _, db := newRegistrationDomainPolicyAuthService(t)
	seedAuthUser(t, db, "disabled@example.com", "secret123", constants.UserStatusDisabled, true)
	seedAuthUser(t, db, "unverified@example.com", "secret123", constants.UserStatusActive, false)

	for _, email := range []string{"disabled@example.com", "unverified@example.com"} {
		if _, err := svc.LoginStep1(email, "wrong-password", false); !errors.Is(err, userauthapp.ErrInvalidCredentials) {
			t.Errorf("wrong password for %s = %v, want ErrInvalidCredentials", email, err)
		}
	}
	if _, err := svc.LoginStep1("disabled@example.com", "secret123", false); !errors.Is(err, userauthapp.ErrUserDisabled) {
		t.Errorf("correct password for disabled user = %v, want ErrUserDisabled", err)
	}
	if _, err := svc.LoginStep1("unverified@example.com", "secret123", false); !errors.Is(err, userauthapp.ErrEmailNotVerified) {
		t.Errorf("correct password for unverified user = %v, want ErrEmailNotVerified", err)
	}
}
