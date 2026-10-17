package resellerbootstrap

import (
	"github.com/dujiao-next/internal/app/container"
	userauthapp "github.com/dujiao-next/internal/modules/identity/userauth/application"
	adminhttp "github.com/dujiao-next/internal/modules/reseller/transport/http/admin"
	userhttp "github.com/dujiao-next/internal/modules/reseller/transport/http/user"
)

type Handlers struct {
	User                *userhttp.UserHandler
	UserProductSetting  *userhttp.UserProductSettingHandler
	UserProductImages   *userhttp.UserProductImagesHandler
	UserFinance         *userhttp.UserFinanceHandler
	UserOrder           *userhttp.UserOrderHandler
	AdminManagement     *adminhttp.AdminManagementHandler
	AdminProfileDetail  *adminhttp.AdminProfileDetailHandler
	AdminSiteConfig     *adminhttp.AdminSiteConfigHandler
	AdminProductSetting *adminhttp.AdminProductSettingHandler
	AdminOperations     *adminhttp.AdminOperationsHandler
	AdminFinance        *adminhttp.AdminFinanceHandler
}

func New(c *container.Container) Handlers {
	productSettings := adminhttp.NewAdminProductSettingHandler(c.ResellerProductSettingService, c.AuthzAuditService)
	productSettings.SetProductImages(c.ResellerProductImageService)
	detail := adminhttp.NewAdminProfileDetailHandler(c.ResellerStore, c.ResellerProductSettingService, c.ResellerAccountingQuery, c.ResellerOrderService)
	detail.SetLoginSecurity(resellerSecurityAdapter{c.UserAuthService}, c.AuthzAuditService)

	return Handlers{
		User: userhttp.NewUserHandler(
			c.ResellerManagementService,
			c.ResellerSiteConfigService,
			c.UploadService,
		),
		UserProductSetting: userhttp.NewUserProductSettingHandler(c.ResellerProductSettingService),
		UserProductImages:  userhttp.NewUserProductImagesHandler(c.ResellerProductImageService),
		UserFinance: userhttp.NewUserFinanceHandler(
			c.ResellerAccountingQuery,
			c.ResellerAccountingWithdraw,
		),
		UserOrder: userhttp.NewUserOrderHandler(c.ResellerOrderService),
		AdminManagement: adminhttp.NewAdminManagementHandler(
			c.ResellerManagementService, c.ResellerStore, c.AuthzAuditService,
		),
		AdminProfileDetail: detail,
		AdminSiteConfig: adminhttp.NewAdminSiteConfigHandler(
			c.ResellerSiteConfigService, c.ResellerStore, c.AuthzAuditService,
		),
		AdminProductSetting: productSettings,
		AdminOperations:     adminhttp.NewAdminOperationsHandler(c.ResellerOperationsService),
		AdminFinance: adminhttp.NewAdminFinanceHandler(
			c.ResellerAccountingQuery,
			c.ResellerAccountingWithdraw,
		),
	}
}

type resellerSecurityAdapter struct{ auth *userauthapp.Service }

func (a resellerSecurityAdapter) UpdateResellerLoginSecurity(adminID, userID uint, required, reset bool, reason string) error {
	_, err := a.auth.UpdateResellerLoginSecurity(adminID, userID, required, reset, reason)
	return err
}
