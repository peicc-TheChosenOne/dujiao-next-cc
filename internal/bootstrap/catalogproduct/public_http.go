package catalogproductbootstrap

import (
	productapplication "github.com/dujiao-next/internal/modules/catalog/product/application"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	producthttp "github.com/dujiao-next/internal/modules/catalog/product/transport/http"
	promotionapp "github.com/dujiao-next/internal/modules/promotion/application"
	promotioncontract "github.com/dujiao-next/internal/modules/promotion/contract"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"html"
)

// PublicHTTPDependencies 是公开 Product HTTP 入口的显式装配依赖。
type PublicHTTPDependencies struct {
	Products     *productapplication.Service
	Hidden       productapplication.HiddenProductRepository
	Images       reseller.ProductImageReader
	Pricer       producthttp.ResellerDisplayPricer
	Promotions   promotioncontract.Repository
	MemberLevels producthttp.MemberLevelPricing
	Mappings     producthttp.LocalProductMappingReader
	SKUMappings  producthttp.SKUMappingLookup
	RelatedPosts producthttp.RelatedPostReader
}

// publicProductAdapter 将 Product 查询服务和租户隐藏策略组合成公开查询端口。
type publicProductAdapter struct {
	products *productapplication.Service
	hidden   productapplication.HiddenProductRepository
	images   reseller.ProductImageReader
}

func (adapter publicProductAdapter) ListPublicForTenant(
	tenant reseller.TenantContext,
	categoryID, search string,
	page, pageSize int,
) ([]productdomain.Product, int64, error) {
	products, total, err := adapter.products.ListPublicForTenant(tenant, adapter.hidden, categoryID, search, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	if err := adapter.applyImages(tenant, products, false); err != nil {
		return nil, 0, err
	}
	return products, total, nil
}

func (adapter publicProductAdapter) GetPublicBySlugForTenant(
	tenant reseller.TenantContext,
	slug string,
) (*productdomain.Product, error) {
	product, err := adapter.products.GetPublicBySlugForTenant(tenant, adapter.hidden, slug)
	if err != nil || product == nil {
		return product, err
	}
	copy := *product
	products := []productdomain.Product{copy}
	if err := adapter.applyImages(tenant, products, true); err != nil {
		return nil, err
	}
	return &products[0], nil
}

func (adapter publicProductAdapter) applyImages(tenant reseller.TenantContext, products []productdomain.Product, detail bool) error {
	if !tenant.IsReseller() || adapter.images == nil || len(products) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(products))
	for _, product := range products {
		ids = append(ids, product.ID)
	}
	configs, err := adapter.images.ListProductImages(*tenant.ResellerID, ids)
	if err != nil {
		return err
	}
	byID := make(map[uint]resellerdomain.ProductImages, len(configs))
	for _, config := range configs {
		byID[config.ProductID] = config
	}
	for i := range products {
		if config, ok := byID[products[i].ID]; ok {
			if len(config.Images) > 0 {
				products[i].Images = append([]string{}, config.Images...)
			}
			if detail && config.DetailImage != "" {
				content := `<p><img src="` + html.EscapeString(config.DetailImage) + `" alt="" /></p>`
				products[i].ContentJSON = jsonmap.JSON{"zh-CN": content, "zh-TW": content, "en-US": content}
			}
		}
	}
	return nil
}

func (adapter publicProductAdapter) ApplyAutoStockCounts(products []productdomain.Product) error {
	return adapter.products.ApplyAutoStockCounts(products)
}

// NewPublicHTTP 装配公开 Product HTTP Handler。
func NewPublicHTTP(dependencies PublicHTTPDependencies) *producthttp.PublicHandler {
	var promotions producthttp.ProductPromotionDecorator
	if dependencies.Promotions != nil {
		promotions = promotionapp.NewService(dependencies.Promotions)
	}
	return producthttp.NewPublicHandler(
		publicProductAdapter{products: dependencies.Products, hidden: dependencies.Hidden, images: dependencies.Images},
		dependencies.Pricer,
		promotions,
		dependencies.MemberLevels,
		dependencies.Mappings,
		dependencies.SKUMappings,
		dependencies.RelatedPosts,
	)
}
