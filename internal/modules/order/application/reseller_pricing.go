package application

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"sort"

	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"strconv"
)

const resellerRuleSourceSKU = resellercontract.RuleSourceSKU

// ResellerPricingResolver resolves reseller-facing prices before order transactions.
type ResellerPricingResolver struct {
	repo resellerPricingStore
}

type resellerPricingStore interface {
	GetProfileByID(id uint) (*resellerdomain.Profile, error)
	ListProductSettingsForPricing(resellerID uint, productIDs, skuIDs []uint) ([]resellerdomain.ProductSetting, error)
	IsActiveRelatedAccount(resellerID, userID uint) (bool, error)
}

func NewResellerPricingResolver(repo resellerPricingStore) *ResellerPricingResolver {
	return &ResellerPricingResolver{repo: repo}
}

func (r *ResellerPricingResolver) ApplyToOrderBuildResult(tenant resellercontract.TenantContext, buyerUserID uint, result *orderBuildResult) (*resellercontract.OrderPricingContext, error) {
	if !isResellerOrderContext(tenant) {
		return nil, nil
	}
	if r == nil || r.repo == nil || result == nil || tenant.ResellerID == nil {
		return nil, ErrResellerProductNotListed
	}
	profile, err := r.loadActiveProfile(*tenant.ResellerID)
	if err != nil {
		return nil, err
	}
	productIDs, skuIDs := collectOrderPlanIDs(result.Plans)
	settings, err := r.repo.ListProductSettingsForPricing(*tenant.ResellerID, productIDs, skuIDs)
	if err != nil {
		return nil, err
	}
	settingsByProduct, settingsBySKU := buildSettingIndexes(settings)

	ctx := &resellercontract.OrderPricingContext{
		ResellerID:     *tenant.ResellerID,
		Domain:         resellerSnapshotDomain(tenant),
		Currency:       result.Currency,
		ResellerUserID: profile.UserID,
		BuyerUserID:    buyerUserID,
		ProfitEligible: true,
		Items:          make([]resellercontract.OrderPricingItem, 0, len(result.Plans)),
	}

	for i := range result.Plans {
		plan := &result.Plans[i]
		if plan == nil || plan.Product == nil || plan.SKU == nil {
			return nil, ErrProductSKUInvalid
		}
		productSetting := settingsByProduct[plan.Product.ID]
		skuSetting := settingsBySKU[resellercontract.SettingKey{ProductID: plan.Product.ID, SKUID: plan.SKU.ID}]
		if productSetting != nil && !productSetting.IsListed {
			return nil, ErrResellerProductNotListed
		}
		if skuSetting != nil && !skuSetting.IsListed {
			return nil, ErrResellerProductNotListed
		}

		quote := resellercontract.ResolveSupplyQuote(profile, *plan.SKU, productSetting, skuSetting)
		if err := quote.ValidationError(); err != nil {
			return nil, err
		}
		if !quote.CanSell || !plan.Product.IsActive {
			return nil, ErrResellerProductNotListed
		}
		baseUnit := quote.Supply.Decimal
		resellerUnit := quote.Retail.Decimal
		quantity := decimal.NewFromInt(int64(plan.Item.Quantity))
		baseTotal := baseUnit.Mul(quantity).Round(2)
		resellerTotal := resellerUnit.Mul(quantity).Round(2)
		profit := resellerTotal.Sub(baseTotal).Round(2)

		zeroMoney := money.FromDecimal(decimal.Zero)
		plan.TotalAmount = resellerTotal
		plan.CouponDiscount = decimal.Zero
		plan.MemberDiscount = decimal.Zero
		plan.PromotionDiscount = decimal.Zero
		plan.WholesaleDiscount = decimal.Zero
		plan.Item.OriginalUnitPrice = money.FromDecimal(resellerUnit)
		plan.Item.UnitPrice = money.FromDecimal(resellerUnit)
		plan.Item.OriginalTotalPrice = money.FromDecimal(resellerTotal)
		plan.Item.TotalPrice = money.FromDecimal(resellerTotal)
		plan.Item.MemberDiscount = zeroMoney
		plan.Item.CouponDiscount = zeroMoney
		plan.Item.PromotionDiscount = zeroMoney
		plan.Item.WholesaleDiscount = zeroMoney
		plan.Item.PromotionID = nil

		ctx.BaseAmount = ctx.BaseAmount.Add(baseTotal).Round(2)
		ctx.ResellerAmount = ctx.ResellerAmount.Add(resellerTotal).Round(2)
		ctx.ProfitAmount = ctx.ProfitAmount.Add(profit).Round(2)
		ctx.Items = append(ctx.Items, resellercontract.OrderPricingItem{
			ProductID:           plan.Product.ID,
			SKUID:               plan.SKU.ID,
			Quantity:            plan.Item.Quantity,
			BaseUnitAmount:      baseUnit,
			ResellerUnitAmount:  resellerUnit,
			BaseTotalAmount:     baseTotal,
			ResellerTotalAmount: resellerTotal,
			ProfitAmount:        profit,
			PricingMode:         resellerdomain.PricingModeFixedPrice,
			RuleSource:          resellercontract.RuleSourceSKU,
			SettingID:           &skuSetting.ID,
			PricingRevision:     quote.Revision,
		})
	}

	result.OriginalAmount = decimal.Zero
	result.TotalAmount = decimal.Zero
	for _, plan := range result.Plans {
		result.OriginalAmount = result.OriginalAmount.Add(plan.TotalAmount).Round(2)
		result.TotalAmount = result.TotalAmount.Add(plan.TotalAmount.Sub(plan.CouponDiscount)).Round(2)
	}
	result.DiscountAmount = decimal.Zero
	result.MemberDiscountAmount = decimal.Zero
	result.PromotionDiscountAmount = decimal.Zero
	result.WholesaleDiscountAmount = decimal.Zero
	result.AppliedCoupon = nil
	result.OrderPromotionID = nil
	result.MemberLevelID = nil

	if err := r.applySelfDealingRisk(ctx, profile); err != nil {
		return nil, err
	}
	if ctx.ProfitEligible {
		ctx.EffectiveProfit = ctx.ProfitAmount
	} else {
		ctx.EffectiveProfit = decimal.Zero
	}
	ctx.PricingSnapshot = ctx.BuildPricingSnapshotJSON()
	ctx.RiskSnapshot = ctx.BuildRiskSnapshotJSON()
	return ctx, nil
}

func (r *ResellerPricingResolver) LoadDisplayPricingBatch(tenant resellercontract.TenantContext, products []productdomain.Product) (*resellercontract.DisplayPricingBatch, error) {
	if !isResellerOrderContext(tenant) {
		return nil, nil
	}
	if r == nil || r.repo == nil || tenant.ResellerID == nil {
		return nil, ErrResellerProductNotListed
	}
	profile, err := r.loadActiveProfile(*tenant.ResellerID)
	if err != nil {
		return nil, err
	}
	productIDs, skuIDs := collectProductIDs(products)
	settings, err := r.repo.ListProductSettingsForPricing(*tenant.ResellerID, productIDs, skuIDs)
	if err != nil {
		return nil, err
	}
	byProduct := make(map[uint][]resellerdomain.ProductSetting)
	for _, setting := range settings {
		byProduct[setting.ProductID] = append(byProduct[setting.ProductID], setting)
	}
	return &resellercontract.DisplayPricingBatch{
		Tenant:            tenant,
		Profile:           profile,
		SettingsByProduct: byProduct,
	}, nil
}

func (r *ResellerPricingResolver) ResolveDisplayPrices(tenant resellercontract.TenantContext, product *productdomain.Product, batch *resellercontract.DisplayPricingBatch) (*resellercontract.DisplayPriceResult, error) {
	if !isResellerOrderContext(tenant) {
		return nil, nil
	}
	if product == nil || batch == nil || batch.Profile == nil {
		return nil, ErrResellerProductNotListed
	}
	productSettings, skuSettings := buildSettingIndexes(batch.SettingsByProduct[product.ID])
	productSetting := productSettings[product.ID]
	if productSetting != nil && !productSetting.IsListed {
		return &resellercontract.DisplayPriceResult{Visible: false, ProductID: product.ID}, nil
	}

	result := &resellercontract.DisplayPriceResult{
		Visible:      false,
		ProductID:    product.ID,
		SKUPrices:    map[uint]money.Amount{},
		HiddenSKUIDs: map[uint]bool{},
	}
	for _, sku := range product.SKUs {
		if !sku.IsActive {
			continue
		}
		skuSetting := skuSettings[resellercontract.SettingKey{ProductID: product.ID, SKUID: sku.ID}]
		if skuSetting != nil && !skuSetting.IsListed {
			result.HiddenSKUIDs[sku.ID] = true
			continue
		}
		quote := resellercontract.ResolveSupplyQuote(batch.Profile, sku, productSetting, skuSetting)
		if !quote.CanSell {
			result.HiddenSKUIDs[sku.ID] = true
			continue
		}
		price := quote.Retail.Decimal
		money := money.FromDecimal(price)
		result.SKUPrices[sku.ID] = money
		if !result.Visible {
			result.Visible = true
			result.DisplaySKUID = sku.ID
			result.DisplayPrice = money
		}
	}
	return result, nil
}

func (r *ResellerPricingResolver) loadActiveProfile(resellerID uint) (*resellerdomain.Profile, error) {
	profile, err := r.repo.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if profile == nil || profile.Status != resellerdomain.ProfileStatusActive {
		return nil, ErrResellerProductNotListed
	}
	return profile, nil
}

func (r *ResellerPricingResolver) applySelfDealingRisk(ctx *resellercontract.OrderPricingContext, profile *resellerdomain.Profile) error {
	if ctx == nil || profile == nil {
		return nil
	}
	relatedMatch := false
	if ctx.BuyerUserID > 0 && ctx.BuyerUserID != profile.UserID {
		matched, err := r.repo.IsActiveRelatedAccount(ctx.ResellerID, ctx.BuyerUserID)
		if err != nil {
			return err
		}
		relatedMatch = matched
	}
	resellercontract.ApplySelfDealingRisk(ctx, profile, relatedMatch)
	return nil
}

func buildSettingIndexes(settings []resellerdomain.ProductSetting) (map[uint]*resellerdomain.ProductSetting, map[resellercontract.SettingKey]*resellerdomain.ProductSetting) {
	return resellercontract.BuildSettingIndexes(settings)
}

func collectOrderPlanIDs(plans []childOrderPlan) ([]uint, []uint) {
	productIDs := make([]uint, 0, len(plans))
	skuIDs := make([]uint, 0, len(plans))
	for _, plan := range plans {
		if plan.Product != nil {
			productIDs = append(productIDs, plan.Product.ID)
		} else if plan.Item.ProductID > 0 {
			productIDs = append(productIDs, plan.Item.ProductID)
		}
		if plan.SKU != nil {
			skuIDs = append(skuIDs, plan.SKU.ID)
		} else if plan.Item.SKUID > 0 {
			skuIDs = append(skuIDs, plan.Item.SKUID)
		}
	}
	return uniqueServiceUintSlice(productIDs), uniqueServiceUintSlice(skuIDs)
}

func collectProductIDs(products []productdomain.Product) ([]uint, []uint) {
	productIDs := make([]uint, 0, len(products))
	skuIDs := []uint{}
	for _, product := range products {
		if product.ID > 0 {
			productIDs = append(productIDs, product.ID)
		}
		for _, sku := range product.SKUs {
			if sku.ID > 0 {
				skuIDs = append(skuIDs, sku.ID)
			}
		}
	}
	return uniqueServiceUintSlice(productIDs), uniqueServiceUintSlice(skuIDs)
}

func uniqueServiceUintSlice(values []uint) []uint {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	result := make([]uint, 0, len(values))
	var last uint
	for i, value := range values {
		if value == 0 {
			continue
		}
		if i > 0 && value == last {
			continue
		}
		result = append(result, value)
		last = value
	}
	return result
}

func isResellerOrderContext(tenant resellercontract.TenantContext) bool {
	return tenant.IsReseller()
}

func resellerSnapshotDomain(tenant resellercontract.TenantContext) string {
	return resellercontract.SnapshotDomain(tenant)
}

func (s *OrderService) pricingRevision(ctx *resellercontract.OrderPricingContext) string {
	if ctx == nil {
		return ""
	}
	data, _ := json.Marshal([]interface{}{ctx.ResellerID, ctx.Domain, ctx.Currency, ctx.BuyerUserID, ctx.ProfitEligible, ctx.ProfitBlockReason, ctx.Items})
	mac := hmac.New(sha256.New, s.pricingSecret)
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *OrderService) confirmPricingInTransaction(tx ordercontract.Transaction, input orderCreateParams, result *orderBuildResult) (*resellercontract.OrderPricingContext, error) {
	store, ok := tx.ResellerOrders().(resellercontract.PricingStore)
	if !ok {
		return nil, resellercontract.ErrPricingChanged
	}
	products, _ := collectOrderPlanIDs(result.Plans)
	if err := store.LockPricingInputs(*input.Tenant.ResellerID, products); err != nil {
		return nil, err
	}
	for i := range result.Plans {
		plan := &result.Plans[i]
		sku, err := tx.ProductSKUs().GetByID(plan.SKU.ID)
		if err != nil {
			return nil, err
		}
		if sku == nil {
			return nil, ErrProductSKUInvalid
		}
		product, err := tx.Products().GetByID(strconv.FormatUint(uint64(plan.Product.ID), 10))
		if err != nil {
			return nil, err
		}
		if product == nil || !product.IsActive {
			return nil, ErrResellerProductNotListed
		}
		plan.SKU = sku
		plan.Product = product
	}
	ctx, err := NewResellerPricingResolver(store).ApplyToOrderBuildResult(input.Tenant, input.UserID, result)
	if err != nil {
		return nil, err
	}
	if !hmac.Equal([]byte(input.PricingRevision), []byte(s.pricingRevision(ctx))) {
		return nil, resellercontract.ErrPricingChanged
	}
	return ctx, nil
}
