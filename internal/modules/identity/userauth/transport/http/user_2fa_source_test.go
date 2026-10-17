package userauthhttp

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/constants"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/gin-gonic/gin"
)

type sourceTestTOTPService struct{}

func (sourceTestTOTPService) GetStatus(uint) (*UserTOTPStatus, error) {
	return &UserTOTPStatus{}, nil
}
func (sourceTestTOTPService) Setup(uint) (*UserTOTPSetupResult, error) {
	return &UserTOTPSetupResult{}, nil
}
func (sourceTestTOTPService) Enable(uint, string) (*UserTOTPEnableResult, error) {
	return &UserTOTPEnableResult{}, nil
}
func (sourceTestTOTPService) Disable(uint, string, bool) error { return nil }
func (sourceTestTOTPService) RegenerateRecoveryCodes(uint, string) ([]string, error) {
	return nil, nil
}
func (sourceTestTOTPService) VerifyChallengeCode(uint, string) error         { return nil }
func (sourceTestTOTPService) VerifyChallengeRecoveryCode(uint, string) error { return nil }

type sourceTest2FAAuth struct {
	source string
}

func (s sourceTest2FAAuth) ParseUserChallengeToken(string) (*UserChallengeClaims, error) {
	return &UserChallengeClaims{
		UserID:      42,
		JTI:         "source-test-jti",
		LoginSource: s.source,
	}, nil
}

func (sourceTest2FAAuth) CompleteLoginAfter2FA(uint, bool) (*AuthLoginResult, error) {
	now := time.Now()
	return &AuthLoginResult{
		User: &userdomain.User{
			ID:    42,
			Email: "buyer@example.com",
		},
		Token:     "access-token",
		ExpiresAt: now.Add(time.Hour),
	}, nil
}

func (sourceTest2FAAuth) GetUserEmail(uint) (string, error) {
	return "buyer@example.com", nil
}

type sourceTestChallengeStore struct{}

func (sourceTestChallengeStore) IsRevoked(context.Context, string) bool { return false }
func (sourceTestChallengeStore) BumpFails(context.Context, string) int64 {
	return 0
}
func (sourceTestChallengeStore) Revoke(context.Context, string) bool { return true }

type sourceTestLoginRecord struct {
	source string
	status string
}

type sourceTestLoginRecorder struct {
	records []sourceTestLoginRecord
}

func (r *sourceTestLoginRecorder) Record(_ string, _ uint, status, _, source, _, _, _ string) {
	r.records = append(r.records, sourceTestLoginRecord{source: source, status: status})
}

func TestVerifyUser2FAPreservesExternalLoginSource(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, source := range []string{constants.LoginLogSourceGoogle, constants.LoginLogSourceTelegram} {
		t.Run(source, func(t *testing.T) {
			loginRecorder := &sourceTestLoginRecorder{}
			handler := NewUser2FAHandler(
				sourceTestTOTPService{},
				sourceTest2FAAuth{source: source},
				sourceTestChallengeStore{},
				loginRecorder,
			)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)
			context.Request = httptest.NewRequest(
				http.MethodPost,
				"/api/v1/auth/login/verify-2fa",
				bytes.NewBufferString(`{"challenge_token":"challenge","code":"123456"}`),
			)
			context.Request.Header.Set("Content-Type", "application/json")

			handler.VerifyUser2FA(context)

			if len(loginRecorder.records) != 1 {
				t.Fatalf("login records = %d, want 1", len(loginRecorder.records))
			}
			if loginRecorder.records[0].source != source {
				t.Fatalf("login source = %q, want %q", loginRecorder.records[0].source, source)
			}
			if loginRecorder.records[0].status != constants.LoginLogStatusSuccess {
				t.Fatalf("login status = %q", loginRecorder.records[0].status)
			}
		})
	}
}

type rejectedChallengeStore struct{ sourceTestChallengeStore }

func (rejectedChallengeStore) Revoke(context.Context, string) bool { return false }

type observed2FAAuth struct {
	sourceTest2FAAuth
	completed bool
}

func (s *observed2FAAuth) CompleteLoginAfter2FA(uint, bool) (*AuthLoginResult, error) {
	s.completed = true
	return s.sourceTest2FAAuth.CompleteLoginAfter2FA(42, false)
}
func TestVerifyUser2FARejectsUnconfirmedChallengeConsumption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &observed2FAAuth{}
	handler := NewUser2FAHandler(sourceTestTOTPService{}, auth, rejectedChallengeStore{}, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/verify", bytes.NewBufferString(`{"challenge_token":"challenge","code":"123456"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.VerifyUser2FA(c)
	if auth.completed {
		t.Fatal("login must not mint a JWT without confirming shared challenge consumption")
	}
	if w.Body.String() == "" {
		t.Fatal("failed consumption must return an error")
	}
}
