package adminauthhttp

import (
	"bytes"
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

type revokeTestTOTP struct{ TOTPService }

func (revokeTestTOTP) VerifyChallengeCode(uint, string) error { return nil }

type revokeTestAuth struct {
	AuthService
	completed bool
}

func (*revokeTestAuth) ParseChallengeToken(string) (*ChallengeClaims, error) {
	return &ChallengeClaims{AdminID: 42, JTI: "test-jti"}, nil
}
func (*revokeTestAuth) GetAdminUsername(uint) (string, error) { return "fixture-admin", nil }
func (s *revokeTestAuth) CompleteLoginAfter2FA(uint) (*AuthLoginResult, error) {
	s.completed = true
	return &AuthLoginResult{}, nil
}

type revokeTestStore struct{}

func (revokeTestStore) IsRevoked(context.Context, string) bool  { return false }
func (revokeTestStore) BumpFails(context.Context, string) int64 { return 0 }
func (revokeTestStore) Revoke(context.Context, string) bool     { return false }
func TestVerifyAdmin2FARejectsUnconfirmedChallengeConsumption(t *testing.T) {
	gin.SetMode(gin.TestMode)
	auth := &revokeTestAuth{}
	handler := NewAdmin2FAHandler(revokeTestTOTP{}, auth, revokeTestStore{}, nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/verify", bytes.NewBufferString(`{"challenge_token":"challenge","code":"123456"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.Verify2FA(c)
	if auth.completed {
		t.Fatal("admin login must not mint a JWT without confirmed shared challenge consumption")
	}
	if w.Body.String() == "" {
		t.Fatal("failed consumption must return an error")
	}
}
