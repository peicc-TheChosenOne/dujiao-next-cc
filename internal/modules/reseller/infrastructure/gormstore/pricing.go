package gormstore

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	productgormstore "github.com/dujiao-next/internal/modules/catalog/product/store/gormstore"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"gorm.io/gorm/clause"
	"sort"
)

// ListProductSettingsForPricing 批量获取分销定价所需的商品级与 SKU 级配置。
func (r *Store) ListProductSettingsForPricing(resellerID uint, productIDs []uint, skuIDs []uint) ([]resellerdomain.ProductSetting, error) {
	if resellerID == 0 || len(productIDs) == 0 {
		return []resellerdomain.ProductSetting{}, nil
	}
	productIDs = uniqueUintSlice(productIDs)
	skuIDs = uniqueUintSlice(skuIDs)

	query := r.db.Where("reseller_id = ? AND product_id IN ? AND deleted_at IS NULL", resellerID, productIDs)
	if len(skuIDs) > 0 {
		query = query.Where("(sku_id = 0 OR sku_id IN ?)", skuIDs)
	} else {
		query = query.Where("sku_id = 0")
	}

	var rows []resellerdomain.ProductSetting
	if err := query.Order("product_id ASC, sku_id ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListHiddenProductIDs 返回分销前台列表应在查询层排除的商品 ID。
func (r *Store) ListHiddenProductIDs(resellerID uint) ([]uint, error) {
	profile, err := r.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	var products []productdomain.Product
	if err := r.db.Select("id").Where("deleted_at IS NULL").Find(&products).Error; err != nil {
		return nil, err
	}
	var skus []productdomain.ProductSKU
	if err := r.db.Where("deleted_at IS NULL AND is_active = ?", true).Find(&skus).Error; err != nil {
		return nil, err
	}
	productIDs := make([]uint, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	skuIDs := make([]uint, 0, len(skus))
	for _, sku := range skus {
		skuIDs = append(skuIDs, sku.ID)
	}
	settings, err := r.ListProductSettingsForPricing(resellerID, productIDs, skuIDs)
	if err != nil {
		return nil, err
	}
	parents, skuSettings := resellercontract.BuildSettingIndexes(settings)
	visible := map[uint]bool{}
	for _, sku := range skus {
		quote := resellercontract.ResolveSupplyQuote(profile, sku, parents[sku.ProductID], skuSettings[resellercontract.SettingKey{ProductID: sku.ProductID, SKUID: sku.ID}])
		if quote.CanSell {
			visible[sku.ProductID] = true
		}
	}
	hidden := make([]uint, 0)
	for _, product := range products {
		if !visible[product.ID] {
			hidden = append(hidden, product.ID)
		}
	}
	return hidden, nil
}

func uniqueUintSlice(values []uint) []uint {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[uint]struct{}, len(values))
	result := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

// LockPricingInputs serializes quote, price and visibility changes with checkout.
func (s *Store) LockPricingInputs(resellerID uint, productIDs []uint) error {
	ids := append([]uint(nil), productIDs...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range uniqueUintSlice(ids) {
		if err := productgormstore.NewSKUStore(s.db).LockProduct(id); err != nil {
			return err
		}
	}
	var profile resellerdomain.Profile
	return s.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("deleted_at IS NULL").First(&profile, resellerID).Error
}
