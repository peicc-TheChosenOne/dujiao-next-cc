package adminhttp

import (
	"errors"
	"strings"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"

	resellermodule "github.com/dujiao-next/internal/modules/reseller/contract"
	dto "github.com/dujiao-next/internal/modules/reseller/transport/http/presenter"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

// ProfileDetailDirectory 是管理端分销商运营详情所需的只读目录端口。
type ProfileDetailDirectory interface {
	GetProfileByID(id uint) (*resellerdomain.Profile, error)
	ListDomainsByResellerID(resellerID uint) ([]resellerdomain.Domain, error)
	GetSiteConfigByResellerID(resellerID uint) (*resellerdomain.SiteConfig, error)
}

// ProductSettingSummarizer 是管理端商品配置汇总端口。
type ProductSettingSummarizer interface {
	SummarizeAdminSettings(resellerID uint) (resellermodule.ProductSettingSummary, error)
}

// OrderAdminLister 是管理端分销订单只读列表端口。
type OrderAdminLister interface {
	ListAdminOrders(resellerID uint, input resellermodule.OrderListInput) ([]resellermodule.OrderListItem, int64, error)
}

// AdminProfileDetailHandler 处理后台分销商运营详情聚合。
type AdminProfileDetailHandler struct {
	security      LoginSecurityManager
	securityAudit AuditRecorder
	directory     ProfileDetailDirectory
	products      ProductSettingSummarizer
	finance       AdminFinanceQueryService
	orders        OrderAdminLister
}

type LoginSecurityManager interface {
	UpdateResellerLoginSecurity(adminID, userID uint, required, reset bool, reason string) error
}

func (h *AdminProfileDetailHandler) SetLoginSecurity(manager LoginSecurityManager, audit AuditRecorder) {
	h.security = manager
	h.securityAudit = audit
}

func (h *AdminProfileDetailHandler) UpdateLoginSecurity(c *gin.Context) {
	h.changeLoginSecurity(c, false)
}
func (h *AdminProfileDetailHandler) ResetLoginSecurity(c *gin.Context) {
	h.changeLoginSecurity(c, true)
}

func (h *AdminProfileDetailHandler) changeLoginSecurity(c *gin.Context, reset bool) {
	var req struct {
		Required         *bool  `json:"required"`
		Reason           string `json:"reason" binding:"required,max=500"`
		IdentityVerified bool   `json:"identity_verified"`
	}
	if c.ShouldBindJSON(&req) != nil || strings.TrimSpace(req.Reason) == "" || (!reset && req.Required == nil) || (reset && !req.IdentityVerified) {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	adminID, ok := ginutil.GetAdminID(c)
	if !ok {
		return
	}
	profile, err := h.directory.GetProfileByID(id)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.internal_error", err)
		return
	}
	if profile == nil {
		ginutil.RespondError(c, response.CodeNotFound, "error.user_not_found", nil)
		return
	}
	if h.security == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.internal_error", nil)
		return
	}
	required := true
	if req.Required != nil {
		required = *req.Required
	}
	if reset {
		required = true
	}
	err = h.security.UpdateResellerLoginSecurity(adminID, profile.UserID, required, reset, req.Reason)
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.totp_security_changed", nil)
		return
	}
	audit := &AdminManagementHandler{audit: h.securityAudit}
	audit.recordAudit(c, "reseller_login_security_change", c.FullPath(), gin.H{"reseller_id": id, "user_id": profile.UserID, "before": profile.LoginSecurity, "required": required, "reset": reset, "reason": req.Reason, "client_ip": c.ClientIP()})
	profile, err = h.directory.GetProfileByID(id)
	if err != nil || profile == nil {
		ginutil.RespondError(c, response.CodeInternal, "error.internal_error", nil)
		return
	}
	response.Success(c, profile.LoginSecurity)
}

func NewAdminProfileDetailHandler(
	directory ProfileDetailDirectory,
	products ProductSettingSummarizer,
	finance AdminFinanceQueryService,
	orders OrderAdminLister,
) *AdminProfileDetailHandler {
	if directory == nil {
		panic("reseller admin profile detail handler: directory is nil")
	}
	return &AdminProfileDetailHandler{
		directory: directory,
		products:  products,
		finance:   finance,
		orders:    orders,
	}
}

// GetProfileDetail 管理端分销商运营详情。
func (h *AdminProfileDetailHandler) GetProfileDetail(c *gin.Context) {
	id, err := ginutil.ParseParamUint(c, "id")
	if err != nil {
		ginutil.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	profile, err := h.directory.GetProfileByID(id)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	if profile == nil {
		ginutil.RespondError(c, response.CodeNotFound, "error.bad_request", nil)
		return
	}
	domains, err := h.directory.ListDomainsByResellerID(id)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	siteConfig, err := h.directory.GetSiteConfigByResellerID(id)
	if err != nil {
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
		return
	}
	productSummary := resellermodule.ProductSettingSummary{}
	if h.products != nil {
		productSummary, err = h.products.SummarizeAdminSettings(id)
		if err != nil {
			ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
			return
		}
	}
	balances := make([]resellerdomain.BalanceAccount, 0)
	recentLedgerEntries := make([]resellerdomain.LedgerEntry, 0)
	recentWithdraws := make([]resellerdomain.WithdrawRequest, 0)
	if h.finance != nil {
		balances, _, err = h.finance.ListAdminBalanceAccounts(resellermodule.AdminBalanceAccountListFilter{
			Page:       1,
			PageSize:   20,
			ResellerID: id,
		})
		if err != nil {
			ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
			return
		}
		recentLedgerEntries, _, err = h.finance.ListAdminLedgerEntries(resellermodule.AdminLedgerListFilter{
			Page:       1,
			PageSize:   10,
			ResellerID: id,
		})
		if err != nil {
			ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
			return
		}
		recentWithdraws, _, err = h.finance.ListAdminWithdrawRequests(resellermodule.AdminWithdrawListFilter{
			Page:       1,
			PageSize:   10,
			ResellerID: id,
		})
		if err != nil {
			ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
			return
		}
	}
	recentOrders := make([]resellermodule.OrderListItem, 0)
	if h.orders != nil {
		recentOrders, _, err = h.orders.ListAdminOrders(id, resellermodule.OrderListInput{
			Page:     1,
			PageSize: 10,
		})
		if err != nil {
			respondAdminProfileDetailError(c, err)
			return
		}
	}
	response.Success(c, dto.NewAdminResellerProfileDetailResp(profile, domains, siteConfig, productSummary, balances, recentOrders, recentLedgerEntries, recentWithdraws))
}

func respondAdminProfileDetailError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, productcontract.ErrNotFound):
		ginutil.RespondError(c, response.CodeNotFound, "error.bad_request", nil)
	default:
		ginutil.RespondError(c, response.CodeInternal, "error.user_fetch_failed", err)
	}
}
