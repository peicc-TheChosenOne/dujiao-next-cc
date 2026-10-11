package productwrite

import (
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func applySupplyQuote(sku *productdomain.ProductSKU, supply, minimum, suggested money.OptionalAmount) error {
	for _, patch := range []money.OptionalAmount{supply, minimum, suggested} {
		if patch.Set && patch.Value != nil && (!patch.Value.GreaterThan(decimal.Zero) || !patch.Value.Equal(patch.Value.Round(2))) {
			return productcontract.ErrProductPriceInvalid
		}
	}
	if supply.Set && supply.Value != nil && (sku.SupplyPriceAmount == nil || !supply.Value.Equal(sku.SupplyPriceAmount.Decimal)) && sku.CostPriceAmount.GreaterThan(decimal.Zero) && supply.Value.LessThan(sku.CostPriceAmount.Decimal) {
		return productcontract.ErrProductPriceInvalid
	}
	sku.SupplyPriceAmount = supply.Apply(sku.SupplyPriceAmount)
	sku.MinSalePriceAmount = minimum.Apply(sku.MinSalePriceAmount)
	sku.SuggestedSalePriceAmount = suggested.Apply(sku.SuggestedSalePriceAmount)
	return nil
}

func (s *WriteService) saveSingleSupplyQuote(repo SKURepository, productID uint, input CreateProductInput) error {
	if !input.SupplyPriceAmount.Set && !input.MinSalePriceAmount.Set && !input.SuggestedSalePriceAmount.Set {
		return nil
	}
	rows, err := repo.ListByProduct(productID, false)
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return productcontract.ErrProductSKUInvalid
	}
	if err := applySupplyQuote(&rows[0], input.SupplyPriceAmount, input.MinSalePriceAmount, input.SuggestedSalePriceAmount); err != nil {
		return err
	}
	return persistSupplyQuote(repo, &rows[0])
}

func persistSupplyQuote(repo SKURepository, sku *productdomain.ProductSKU) error {
	if writer, ok := repo.(interface {
		UpdateSupplyQuote(*productdomain.ProductSKU) error
	}); ok {
		return writer.UpdateSupplyQuote(sku)
	}
	return repo.Update(sku)
}
