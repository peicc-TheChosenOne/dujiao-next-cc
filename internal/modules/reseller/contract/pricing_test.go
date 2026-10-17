package contract

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"

	"testing"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/shopspring/decimal"
)

func TestApplySelfDealingRiskOwnerMatch(t *testing.T) {
	ctx := &OrderPricingContext{
		ResellerUserID: 9,
		BuyerUserID:    9,
		ProfitEligible: true,
	}
	ApplySelfDealingRisk(ctx, &resellerdomain.Profile{UserID: 9}, false)
	if ctx.ProfitEligible || ctx.ProfitBlockReason != ProfitBlockOwner {
		t.Fatalf("unexpected risk: eligible=%v reason=%s", ctx.ProfitEligible, ctx.ProfitBlockReason)
	}
}

func TestBuildSettingIndexes(t *testing.T) {
	byProduct, bySKU := BuildSettingIndexes([]resellerdomain.ProductSetting{
		{ProductID: 1, SKUID: 0, IsListed: true},
		{ProductID: 1, SKUID: 2, IsListed: false},
	})
	if byProduct[1] == nil || !byProduct[1].IsListed {
		t.Fatalf("expected product setting")
	}
	if bySKU[SettingKey{ProductID: 1, SKUID: 2}] == nil || bySKU[SettingKey{ProductID: 1, SKUID: 2}].IsListed {
		t.Fatalf("expected sku setting hidden")
	}
}

func TestMoneyString(t *testing.T) {
	if got := MoneyString(decimal.RequireFromString("1.2")); got != "1.20" {
		t.Fatalf("unexpected money string: %s", got)
	}
}

func TestSupplyQuoteFloorCapFixedPriceAndDefaultDraft(t *testing.T) {
	amount := func(value string) *money.Amount { a := money.FromDecimal(decimal.RequireFromString(value)); return &a }
	profile := &resellerdomain.Profile{Status: resellerdomain.ProfileStatusActive, DefaultMarkupPercent: *amount("20"), MaxMarkupPercent: *amount("10")}
	sku := productdomain.ProductSKU{ID: 1, IsActive: true, PriceAmount: *amount("500"), CostPriceAmount: *amount("60"), SupplyPriceAmount: amount("80.01"), MinSalePriceAmount: amount("85")}
	setting := &resellerdomain.ProductSetting{IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice, FixedPriceAmount: *amount("87")}
	q := ResolveSupplyQuote(profile, sku, nil, setting)
	if !q.CanSell || q.Minimum.String() != "85.00" || q.Maximum.String() != "88.01" || q.Profit.String() != "6.99" {
		t.Fatalf("quote mismatch %+v", q)
	}
	before := q.Revision
	profile.DefaultMarkupPercent = *amount("50")
	sku.PriceAmount = *amount("999")
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.Retail.String() != "87.00" || q.Draft.String() != "85.00" || q.Revision != before {
		t.Fatal("defaults/main retail must not reprice saved retail", q)
	}
	sku.MinSalePriceAmount = nil
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.Minimum.String() != "80.01" {
		t.Fatal(q)
	}
	setting.FixedPriceAmount = *amount("88.02")
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.ErrorCode != "markup_exceeded" {
		t.Fatal(q)
	}
	profile.MaxMarkupPercent = *amount("0")
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if !q.CanSell || q.Maximum != nil {
		t.Fatal(q)
	}
	sku.SupplyPriceAmount = amount("90")
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.ErrorCode != "below_minimum" || q.Retail.String() != "88.02" {
		t.Fatal("quote increase must pause without rewriting retail", q)
	}
	sku.SupplyPriceAmount = nil
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.ErrorCode != "supply_missing" || q.CanSell {
		t.Fatal(q)
	}
	sku.SupplyPriceAmount = amount("50")
	sku.MinSalePriceAmount = amount("55")
	q = ResolveSupplyQuote(profile, sku, nil, setting)
	if q.ErrorCode != "supply_below_cost" || q.CanSell || q.Minimum.String() != "55.00" || q.Retail.String() != "88.02" {
		t.Fatal("cost increases must preserve retail and the displayed floor", q)
	}
}

func TestUnpricedSupplyQuotePrefillsMinimumWithoutListing(t *testing.T) {
	amount := func(value string) *money.Amount { a := money.FromDecimal(decimal.RequireFromString(value)); return &a }
	profile := &resellerdomain.Profile{Status: resellerdomain.ProfileStatusActive, DefaultMarkupPercent: *amount("50"), MaxMarkupPercent: *amount("20")}
	sku := productdomain.ProductSKU{ID: 1, IsActive: true, SupplyPriceAmount: amount("80.01"), MinSalePriceAmount: amount("85")}
	for _, setting := range []*resellerdomain.ProductSetting{
		nil,
		{IsListed: false, PricingMode: resellerdomain.PricingModeInherit},
		{IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice},
	} {
		q := ResolveSupplyQuote(profile, sku, nil, setting)
		if q.Draft.String() != "85.00" || q.Retail.String() != "0.00" || q.CanSell || q.Valid || q.ErrorCode != "unpriced" {
			t.Fatalf("minimum must remain an unsaved draft: %+v", q)
		}
	}
	sku.MinSalePriceAmount = nil
	q := ResolveSupplyQuote(profile, sku, nil, nil)
	if q.Draft.String() != "80.01" || q.Draft.String() != q.Minimum.String() {
		t.Fatal("an empty minimum must prefill supply price", q)
	}
	sku.SupplyPriceAmount = nil
	q = ResolveSupplyQuote(profile, sku, nil, nil)
	if q.Draft.String() != "0.00" || q.ErrorCode != "supply_missing" || q.CanSell {
		t.Fatal("missing supply must not produce a sellable draft", q)
	}
}
