package adminhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"

	resellermodule "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

type profileDetailDirectoryStub struct {
	profile *resellerdomain.Profile
}

func (s profileDetailDirectoryStub) GetProfileByID(id uint) (*resellerdomain.Profile, error) {
	return s.profile, nil
}
func (s profileDetailDirectoryStub) ListDomainsByResellerID(resellerID uint) ([]resellerdomain.Domain, error) {
	return nil, nil
}
func (s profileDetailDirectoryStub) GetSiteConfigByResellerID(resellerID uint) (*resellerdomain.SiteConfig, error) {
	return nil, nil
}

type orderAdminListerStub struct {
	err error
}

func (s orderAdminListerStub) ListAdminOrders(resellerID uint, input resellermodule.OrderListInput) ([]resellermodule.OrderListItem, int64, error) {
	return nil, 0, s.err
}

func TestAdminProfileDetailHandlerMapsOrderNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAdminProfileDetailHandler(
		profileDetailDirectoryStub{profile: &resellerdomain.Profile{ID: 1}},
		nil,
		nil,
		orderAdminListerStub{err: productcontract.ErrNotFound},
	)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/resellers/profiles/1", nil)
	c.Params = gin.Params{{Key: "id", Value: "1"}}

	h.GetProfileDetail(c)

	var resp struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.StatusCode != response.CodeNotFound {
		t.Fatalf("expected not found, body=%s", recorder.Body.String())
	}
}

type securityManagerStub struct{ called bool }

func (s *securityManagerStub) UpdateResellerLoginSecurity(uint, uint, bool, bool, string) error {
	s.called = true
	return nil
}
func TestResetLoginSecurityRequiresReasonAndVerifiedIdentity(t *testing.T) {
	for _, body := range []string{`{"reason":"lost"}`, `{"identity_verified":true,"reason":" "}`} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/admin/resellers/profiles/1/login-security/reset", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "1"}}
		c.Set("admin_id", uint(1))
		service := &securityManagerStub{}
		handler := NewAdminProfileDetailHandler(profileDetailDirectoryStub{profile: &resellerdomain.Profile{ID: 1, UserID: 42}}, nil, nil, nil)
		handler.SetLoginSecurity(service, nil)
		handler.ResetLoginSecurity(c)
		var payload struct {
			StatusCode int `json:"status_code"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if service.called || payload.StatusCode != response.CodeBadRequest {
			t.Fatalf("invalid reset accepted: %d", recorder.Code)
		}
	}
}
