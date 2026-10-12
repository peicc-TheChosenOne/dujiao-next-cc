package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

const RuleSourceSKU = "sku"

// SupplyQuote is shared by pricing editors, storefronts and order creation.
type SupplyQuote struct {
	Supply    *money.Amount `json:"supply_price_amount"`
	Minimum   money.Amount  `json:"min_sale_price_amount"`
	Maximum   *money.Amount `json:"max_sale_price_amount"`
	Retail    money.Amount  `json:"sale_price_amount"`
	Draft     money.Amount  `json:"draft_price_amount"`
	Profit    money.Amount  `json:"profit_amount"`
	Valid     bool          `json:"price_valid"`
	CanSell   bool          `json:"can_sell"`
	ErrorCode string        `json:"error_code"`
	Revision  string        `json:"pricing_revision"`
}

func ResolveSupplyQuote(profile *resellerdomain.Profile, sku productdomain.ProductSKU, parent, setting *resellerdomain.ProductSetting) SupplyQuote {
	q := SupplyQuote{Supply: sku.SupplyPriceAmount}
	if setting != nil && setting.PricingMode == resellerdomain.PricingModeFixedPrice {
		q.Retail = setting.FixedPriceAmount
	}
	switch {
	case q.Supply == nil || !q.Supply.GreaterThan(decimal.Zero):
		q.ErrorCode = "supply_missing"
	default:
		q.Minimum = *q.Supply
		if sku.MinSalePriceAmount != nil && sku.MinSalePriceAmount.GreaterThan(q.Minimum.Decimal) {
			q.Minimum = *sku.MinSalePriceAmount
		}
		q.Draft = q.Minimum
		if profile != nil {
			if profile.MaxMarkupPercent.GreaterThan(decimal.Zero) {
				maximum := money.FromDecimal(q.Supply.Mul(decimal.NewFromInt(100).Add(profile.MaxMarkupPercent.Decimal)).Div(decimal.NewFromInt(100)).Truncate(2))
				q.Maximum = &maximum
			}
		}
		q.Profit = money.FromDecimal(q.Retail.Sub(q.Supply.Decimal))
		switch {
		case sku.CostPriceAmount.GreaterThan(decimal.Zero) && q.Supply.LessThan(sku.CostPriceAmount.Decimal):
			q.ErrorCode = "supply_below_cost"
		case q.Maximum != nil && q.Minimum.GreaterThan(q.Maximum.Decimal):
			q.ErrorCode = "range_conflict"
		case setting == nil || setting.PricingMode != resellerdomain.PricingModeFixedPrice || !q.Retail.GreaterThan(decimal.Zero):
			q.ErrorCode = "unpriced"
		case q.Retail.LessThan(q.Minimum.Decimal):
			q.ErrorCode = "below_minimum"
		case q.Maximum != nil && q.Retail.GreaterThan(q.Maximum.Decimal):
			q.ErrorCode = "markup_exceeded"
		default:
			q.Valid = true
		}
	}
	if q.Valid {
		switch {
		case !sku.IsActive:
			q.ErrorCode = "sku_inactive"
		case profile == nil || profile.Status != resellerdomain.ProfileStatusActive:
			q.ErrorCode = "reseller_inactive"
		case parent != nil && !parent.IsListed:
			q.ErrorCode = "product_hidden"
		case setting == nil || !setting.IsListed:
			q.ErrorCode = "sku_hidden"
		default:
			q.CanSell = true
		}
	}
	// Only pricing and visibility participate; stock changes do not invalidate a quote.
	var maximum, status string
	if profile != nil {
		maximum = profile.MaxMarkupPercent.String()
		status = profile.Status
	}
	data, _ := json.Marshal([]interface{}{sku.ID, sku.SupplyPriceAmount, sku.MinSalePriceAmount, sku.CostPriceAmount, sku.IsActive, q.Retail, parent == nil || parent.IsListed, setting != nil && setting.IsListed, maximum, status})
	sum := sha256.Sum256(data)
	q.Revision = hex.EncodeToString(sum[:])
	return q
}

func (q SupplyQuote) ValidationError() error {
	if q.Valid {
		return nil
	}
	if q.ErrorCode == "markup_exceeded" {
		return ErrMarkupExceeded
	}
	return fmt.Errorf("%w: %s", ErrPriceBelowBase, q.ErrorCode)
}
