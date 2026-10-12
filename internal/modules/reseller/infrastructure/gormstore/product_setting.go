package gormstore

import (
	"errors"
	"strconv"
	"strings"
	"time"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/persistence/gormutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Store) ListProductImages(resellerID uint, productIDs []uint) ([]resellerdomain.ProductImages, error) {
	rows := []resellerdomain.ProductImages{}
	if resellerID == 0 || len(productIDs) == 0 {
		return rows, nil
	}
	err := r.db.Where("reseller_id = ? AND product_id IN ?", resellerID, productIDs).Find(&rows).Error
	return rows, err
}

// SaveProductImages 仅做短事务：重新校验范围并原子比较版本，不在事务中上传文件。
func (r *Store) SaveProductImages(config resellerdomain.ProductImages, expectedRevision uint64) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var profile resellerdomain.Profile
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND status = ? AND deleted_at IS NULL", config.ResellerID, resellerdomain.ProfileStatusActive).First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return resellercontract.ErrProfileInactive
			}
			return err
		}
		var product productdomain.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND is_active = ? AND deleted_at IS NULL", config.ProductID, true).
			Where("EXISTS (SELECT 1 FROM categories c WHERE c.id = products.category_id AND c.is_active = ? AND c.deleted_at IS NULL)", true).First(&product).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return productcontract.ErrNotFound
			}
			return err
		}
		config.Revision = expectedRevision + 1
		if expectedRevision == 0 {
			result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reseller_id"}, {Name: "product_id"}}, DoNothing: true}).Create(&config)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return resellercontract.ErrProductImagesChanged
			}
			return nil
		}
		result := tx.Model(&resellerdomain.ProductImages{}).
			Where("reseller_id = ? AND product_id = ? AND revision = ?", config.ResellerID, config.ProductID, expectedRevision).
			Updates(map[string]interface{}{"images": config.Images, "detail_image": config.DetailImage, "revision": config.Revision, "operation_id": config.OperationID, "updated_at": time.Now()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return resellercontract.ErrProductImagesChanged
		}
		return nil
	})
}

func (r *Store) ListProductsWithSettings(filter resellercontract.ProductSettingListFilter) ([]resellercontract.ProductSettingProductRow, int64, error) {
	if filter.ResellerID == 0 {
		return []resellercontract.ProductSettingProductRow{}, 0, nil
	}
	query := r.db.Model(&productdomain.Product{}).
		Where("products.deleted_at IS NULL").
		Preload("Category", "deleted_at IS NULL").
		Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			db = db.Where("deleted_at IS NULL")
			if filter.OnlyActive {
				db = db.Where("is_active = ?", true)
			}
			return db.Order("sort_order DESC, id ASC")
		})
	if filter.OnlyActive {
		query = query.Where("products.is_active = ?", true)
		query = query.Where("EXISTS (SELECT 1 FROM categories c WHERE c.id = products.category_id AND c.is_active = ? AND c.deleted_at IS NULL)", true)
	}
	if filter.CategoryID > 0 {
		query = query.Where("products.category_id = ?", filter.CategoryID)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		condition, argCount := gormutil.BuildLocalizedLikeCondition(r.db, []string{"products.slug"}, []string{"products.title_json", "products.description_json"})
		searchQuery := r.db.Where(condition, gormutil.RepeatLikeArgs(like, argCount)...)
		if numericID, err := strconv.ParseUint(keyword, 10, 64); err == nil && numericID > 0 {
			searchQuery = searchQuery.Or("products.id = ?", uint(numericID))
		}
		query = query.Where(searchQuery)
	}
	configured := strings.ToLower(strings.TrimSpace(filter.Configured))
	if configured == "configured" {
		query = query.Where("EXISTS (SELECT 1 FROM reseller_product_settings rps WHERE rps.reseller_id = ? AND rps.product_id = products.id AND rps.deleted_at IS NULL)", filter.ResellerID)
	} else if configured == "unconfigured" {
		query = query.Where("NOT EXISTS (SELECT 1 FROM reseller_product_settings rps WHERE rps.reseller_id = ? AND rps.product_id = products.id AND rps.deleted_at IS NULL)", filter.ResellerID)
	}
	listed := strings.ToLower(strings.TrimSpace(filter.Listed))
	if listed == "hidden" {
		query = query.Where("EXISTS (SELECT 1 FROM reseller_product_settings rps WHERE rps.reseller_id = ? AND rps.product_id = products.id AND rps.is_listed = ? AND rps.deleted_at IS NULL)", filter.ResellerID, false)
	} else if listed == "listed" {
		query = query.Where("NOT EXISTS (SELECT 1 FROM reseller_product_settings rps WHERE rps.reseller_id = ? AND rps.product_id = products.id AND rps.is_listed = ? AND rps.deleted_at IS NULL)", filter.ResellerID, false)
	}

	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var products []productdomain.Product
	if err := applyPagination(query.Session(&gorm.Session{}), filter.Page, filter.PageSize).
		Order("products.sort_order DESC, products.created_at DESC").
		Find(&products).Error; err != nil {
		return nil, 0, err
	}
	rows, err := r.attachSettings(filter.ResellerID, products)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *Store) GetProductWithSettings(resellerID, productID uint) (*resellercontract.ProductSettingProductRow, error) {
	if resellerID == 0 || productID == 0 {
		return nil, nil
	}
	var product productdomain.Product
	err := r.db.Preload("Category", "deleted_at IS NULL").
		Preload("SKUs", func(db *gorm.DB) *gorm.DB {
			return db.Where("deleted_at IS NULL AND is_active = ?", true).Order("sort_order DESC, id ASC")
		}).
		Where("products.deleted_at IS NULL").
		First(&product, productID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	rows, err := r.attachSettings(resellerID, []productdomain.Product{product})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func (r *Store) attachSettings(resellerID uint, products []productdomain.Product) ([]resellercontract.ProductSettingProductRow, error) {
	if len(products) == 0 {
		return []resellercontract.ProductSettingProductRow{}, nil
	}
	productIDs := make([]uint, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	var settings []resellerdomain.ProductSetting
	if err := r.db.Where("reseller_id = ? AND product_id IN ? AND deleted_at IS NULL", resellerID, productIDs).
		Order("product_id ASC, sku_id ASC").
		Find(&settings).Error; err != nil {
		return nil, err
	}
	settingsByProduct := make(map[uint][]resellerdomain.ProductSetting)
	for _, setting := range settings {
		settingsByProduct[setting.ProductID] = append(settingsByProduct[setting.ProductID], setting)
	}
	out := make([]resellercontract.ProductSettingProductRow, 0, len(products))
	for _, product := range products {
		out = append(out, resellercontract.ProductSettingProductRow{Product: product, Settings: settingsByProduct[product.ID]})
	}
	return out, nil
}

func (r *Store) UpsertSetting(setting resellerdomain.ProductSetting) (*resellerdomain.ProductSetting, error) {
	if setting.ResellerID == 0 || setting.ProductID == 0 {
		return nil, errors.New("reseller product setting scope is invalid")
	}
	var existing resellerdomain.ProductSetting
	err := r.db.Unscoped().
		Where("reseller_id = ? AND product_id = ? AND sku_id = ?", setting.ResellerID, setting.ProductID, setting.SKUID).
		First(&existing).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		isListed := setting.IsListed
		if err := r.db.Select("*").Create(&setting).Error; err != nil {
			return nil, err
		}
		if !isListed {
			if err := r.db.Model(&resellerdomain.ProductSetting{}).
				Where("id = ?", setting.ID).
				Update("is_listed", false).Error; err != nil {
				return nil, err
			}
			setting.IsListed = false
		}
		return &setting, nil
	}
	existing.IsListed = setting.IsListed
	existing.PricingMode = setting.PricingMode
	existing.MarkupPercent = setting.MarkupPercent
	existing.FixedMarkupAmount = setting.FixedMarkupAmount
	existing.FixedPriceAmount = setting.FixedPriceAmount
	existing.SortOrder = setting.SortOrder
	existing.DeletedAt = nil
	if err := r.db.Model(&resellerdomain.ProductSetting{}).
		Where("id = ?", existing.ID).
		Select("*").
		Updates(&existing).Error; err != nil {
		return nil, err
	}
	return &existing, nil
}

func (r *Store) DeleteSetting(resellerID, productID, skuID uint) error {
	if resellerID == 0 || productID == 0 {
		return nil
	}
	now := time.Now()
	return r.db.Model(&resellerdomain.ProductSetting{}).
		Where("reseller_id = ? AND product_id = ? AND sku_id = ? AND deleted_at IS NULL", resellerID, productID, skuID).
		Updates(map[string]interface{}{"deleted_at": &now, "updated_at": now}).Error
}

func (r *Store) ListAdminSettings(filter resellercontract.ProductSettingAdminListFilter) ([]resellerdomain.ProductSetting, int64, error) {
	query := r.db.Model(&resellerdomain.ProductSetting{}).
		Preload("Product", "deleted_at IS NULL").
		Preload("Profile", "deleted_at IS NULL").
		Preload("Profile.User", "deleted_at IS NULL").
		Where("reseller_product_settings.deleted_at IS NULL")
	if filter.ResellerID > 0 {
		query = query.Where("reseller_product_settings.reseller_id = ?", filter.ResellerID)
	}
	if filter.ProductID > 0 {
		query = query.Where("reseller_product_settings.product_id = ?", filter.ProductID)
	}
	if filter.UserID > 0 {
		query = query.Joins("JOIN reseller_profiles rp_filter ON rp_filter.id = reseller_product_settings.reseller_id AND rp_filter.deleted_at IS NULL").
			Where("rp_filter.user_id = ?", filter.UserID)
	}
	if mode := strings.TrimSpace(filter.PricingMode); mode != "" {
		query = query.Where("reseller_product_settings.pricing_mode = ?", mode)
	}
	if listed := strings.ToLower(strings.TrimSpace(filter.Listed)); listed == "hidden" {
		query = query.Where("reseller_product_settings.is_listed = ?", false)
	} else if listed == "listed" {
		query = query.Where("reseller_product_settings.is_listed = ?", true)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Joins("LEFT JOIN products p_keyword ON p_keyword.id = reseller_product_settings.product_id").
			Joins("LEFT JOIN reseller_profiles rp_keyword ON rp_keyword.id = reseller_product_settings.reseller_id").
			Joins("LEFT JOIN users u_keyword ON u_keyword.id = rp_keyword.user_id").
			Where("LOWER(p_keyword.slug) LIKE ? OR LOWER(u_keyword.email) LIKE ? OR LOWER(u_keyword.display_name) LIKE ? OR CAST(reseller_product_settings.reseller_id AS TEXT) = ?", like, like, like, keyword)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []resellerdomain.ProductSetting
	if err := applyPagination(query.Session(&gorm.Session{}), filter.Page, filter.PageSize).
		Order("reseller_product_settings.updated_at DESC, reseller_product_settings.id DESC").
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *Store) SummarizeByResellerID(resellerID uint) (resellercontract.ProductSettingSummary, error) {
	var summary resellercontract.ProductSettingSummary
	if resellerID == 0 {
		return summary, nil
	}
	scope := r.db.Model(&resellerdomain.ProductSetting{}).
		Where("reseller_id = ? AND deleted_at IS NULL", resellerID)
	if err := scope.Session(&gorm.Session{}).Distinct("product_id").Count(&summary.ConfiguredProducts).Error; err != nil {
		return summary, err
	}
	if err := scope.Session(&gorm.Session{}).Where("is_listed = ?", false).Distinct("product_id").Count(&summary.HiddenProducts).Error; err != nil {
		return summary, err
	}
	if err := scope.Session(&gorm.Session{}).Where("sku_id > 0").Count(&summary.SKUOverrides).Error; err != nil {
		return summary, err
	}
	if err := scope.Session(&gorm.Session{}).Where("pricing_mode <> ?", resellerdomain.PricingModeInherit).Count(&summary.PricingOverrides).Error; err != nil {
		return summary, err
	}
	return summary, nil
}
