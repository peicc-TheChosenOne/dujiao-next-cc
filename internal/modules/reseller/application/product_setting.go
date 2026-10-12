package application

import (
	"mime/multipart"
	"strings"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	uploadcontract "github.com/dujiao-next/internal/modules/upload/contract"
	"github.com/dujiao-next/internal/shared/jsonslice"
	"github.com/google/uuid"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

const ProductImageMaxSize int64 = 500 * 1024
const ProductImageMaxCount = 5

type ProductImageUploader interface {
	SaveFileWithMeta(file *multipart.FileHeader, scene string) (*uploadcontract.Result, error)
}

type ProductImageService struct {
	store    resellercontract.ProductImageStore
	uploader ProductImageUploader
}

type ProductImageState struct {
	Product productdomain.Product
	Config  resellerdomain.ProductImages
}

func NewProductImageService(store resellercontract.ProductImageStore, uploader ProductImageUploader) *ProductImageService {
	return &ProductImageService{store: store, uploader: uploader}
}

func (s *ProductImageService) activeProfile(userID uint) (*resellerdomain.Profile, error) {
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductImageService) ListUserProductImages(userID uint, input ProductSettingUserListInput) ([]ProductImageState, int64, error) {
	profile, err := s.activeProfile(userID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.store.ListProductsWithSettings(resellercontract.ProductSettingListFilter{ResellerID: profile.ID, OnlyActive: true, Page: input.Page, PageSize: input.PageSize, Keyword: input.Keyword})
	if err != nil {
		return nil, 0, err
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Product.ID)
	}
	configs, err := s.store.ListProductImages(profile.ID, ids)
	if err != nil {
		return nil, 0, err
	}
	byID := make(map[uint]resellerdomain.ProductImages, len(configs))
	for _, config := range configs {
		byID[config.ProductID] = config
	}
	states := make([]ProductImageState, 0, len(rows))
	for _, row := range rows {
		states = append(states, ProductImageState{Product: row.Product, Config: byID[row.Product.ID]})
	}
	return states, total, nil
}

func (s *ProductImageService) GetUserProductImages(userID, productID uint) (*ProductImageState, error) {
	profile, err := s.activeProfile(userID)
	if err != nil {
		return nil, err
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil || !row.Product.IsActive || !row.Product.Category.IsActive || row.Product.Category.DeletedAt != nil {
		return nil, productcontract.ErrNotFound
	}
	configs, err := s.store.ListProductImages(profile.ID, []uint{productID})
	if err != nil {
		return nil, err
	}
	config := resellerdomain.ProductImages{ResellerID: profile.ID, ProductID: productID, Images: jsonslice.Strings{}}
	if len(configs) > 0 {
		config = configs[0]
	}
	return &ProductImageState{Product: row.Product, Config: config}, nil
}

func (s *ProductImageService) saveImages(state *ProductImageState, images []string, revision uint64, operationID string) (*ProductImageState, error) {
	if _, err := uuid.Parse(operationID); err != nil || len(operationID) != 36 {
		return nil, resellercontract.ErrProductImagesInvalid
	}
	config := state.Config
	config.Images = append(jsonslice.Strings{}, images...)
	config.OperationID = operationID
	if err := s.store.SaveProductImages(config, revision); err != nil {
		return nil, err
	}
	config.Revision = revision + 1
	return &ProductImageState{Product: state.Product, Config: config}, nil
}

func (s *ProductImageService) UpdateUserProductImages(userID, productID uint, images []string, revision uint64, operationID string) (*ProductImageState, error) {
	state, err := s.GetUserProductImages(userID, productID)
	if err != nil {
		return nil, err
	}
	if state.Config.Revision != revision {
		return nil, resellercontract.ErrProductImagesChanged
	}
	if len(images) > ProductImageMaxCount {
		return nil, resellercontract.ErrProductImagesLimit
	}
	allowed := make(map[string]bool, len(state.Config.Images))
	for _, image := range state.Config.Images {
		allowed[image] = true
	}
	for _, image := range images {
		if !allowed[image] {
			return nil, resellercontract.ErrProductImagesInvalid
		}
		delete(allowed, image) // 不允许重复图片，也不能引入其他商品/店铺或外部地址。
	}
	return s.saveImages(state, images, revision, operationID)
}

func (s *ProductImageService) UploadUserProductImage(userID, productID uint, file *multipart.FileHeader, revision uint64, operationID string) (*ProductImageState, error) {
	state, err := s.GetUserProductImages(userID, productID)
	if err != nil {
		return nil, err
	}
	if state.Config.Revision != revision {
		return nil, resellercontract.ErrProductImagesChanged
	}
	if len(state.Config.Images) >= ProductImageMaxCount {
		return nil, resellercontract.ErrProductImagesLimit
	}
	result, err := s.uploadImageFile(file, operationID)
	if err != nil {
		return nil, err
	}
	images := append(append([]string{}, state.Config.Images...), result.URL)
	return s.saveImages(state, images, revision, operationID)
}

func (s *ProductImageService) uploadImageFile(file *multipart.FileHeader, operationID string) (*uploadcontract.Result, error) {
	if file == nil || file.Size <= 0 || file.Size > ProductImageMaxSize {
		return nil, resellercontract.ErrProductImagesInvalid
	}
	if _, err := uuid.Parse(operationID); err != nil || len(operationID) != 36 {
		return nil, resellercontract.ErrProductImagesInvalid
	}
	// 上传器由装配层提供独立的 PNG/JPG/GIF/WebP 和 500KB 策略，不改变站点 Logo/Banner 策略。
	return s.uploader.SaveFileWithMeta(file, "reseller")
}

// UploadUserProductDetailImage 替换单张详情图，保留独立的商品主图。
func (s *ProductImageService) UploadUserProductDetailImage(userID, productID uint, file *multipart.FileHeader, revision uint64, operationID string) (*ProductImageState, error) {
	state, err := s.GetUserProductImages(userID, productID)
	if err != nil {
		return nil, err
	}
	if state.Config.Revision != revision {
		return nil, resellercontract.ErrProductImagesChanged
	}
	result, err := s.uploadImageFile(file, operationID)
	if err != nil {
		return nil, err
	}
	state.Config.DetailImage = result.URL
	return s.saveImages(state, state.Config.Images, revision, operationID)
}

func (s *ProductImageService) ResetUserProductDetailImage(userID, productID uint, revision uint64, operationID string) (*ProductImageState, error) {
	state, err := s.GetUserProductImages(userID, productID)
	if err != nil {
		return nil, err
	}
	if state.Config.Revision != revision {
		return nil, resellercontract.ErrProductImagesChanged
	}
	state.Config.DetailImage = ""
	return s.saveImages(state, state.Config.Images, revision, operationID)
}

// ProductSettingService 分销商品改价/上架配置用例。
type ProductSettingService struct {
	store       resellercontract.ProductSettingStore
	productRepo productcontract.Repository
}

func NewProductSettingService(store resellercontract.ProductSettingStore, productRepo productcontract.Repository) *ProductSettingService {
	return &ProductSettingService{store: store, productRepo: productRepo}
}

type ProductSettingInput struct {
	SKUID             uint
	IsListed          bool
	PricingMode       string
	MarkupPercent     decimal.Decimal
	FixedMarkupAmount decimal.Decimal
	FixedPriceAmount  decimal.Decimal
	SortOrder         int
	PricingRevision   string
	VisibilityOnly    bool
}

type ProductSettingSaveInput struct {
	Settings []ProductSettingInput
}

type ProductSettingUserListInput struct {
	Page       int
	PageSize   int
	Keyword    string
	CategoryID uint
	Configured string
	Listed     string
}

type ProductSettingAdminListInput struct {
	Page        int
	PageSize    int
	ResellerID  uint
	UserID      uint
	ProductID   uint
	Keyword     string
	PricingMode string
	Configured  string
	Listed      string
}

type ProductSettingDetail struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

type ProductSettingListRow struct {
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

// ProductSettingPreviewItem 表示某个商品级（SKUID=0）或 SKU 级规则在「拟用配置」下的预览结果。
// 复用与保存/下单完全一致的 ResolveSupplyQuote，确保预览价与实际成交价零分歧。
type ProductSettingPreviewItem struct {
	SKUID          uint
	IsListed       bool
	BasePrice      decimal.Decimal
	EffectivePrice decimal.Decimal
	Valid          bool
	ErrorCode      string
}

func (s *ProductSettingService) ListUserProductSettings(userID uint, input ProductSettingUserListInput) ([]ProductSettingListRow, int64, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.store.ListProductsWithSettings(resellercontract.ProductSettingListFilter{
		Page:       input.Page,
		PageSize:   input.PageSize,
		ResellerID: profile.ID,
		CategoryID: input.CategoryID,
		Keyword:    input.Keyword,
		Configured: input.Configured,
		Listed:     input.Listed,
		OnlyActive: true,
	})
	if err != nil {
		return nil, 0, err
	}
	return s.decorateRows(profile, rows), total, nil
}

func (s *ProductSettingService) GetUserProductSetting(userID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewUserProductSettings 在不落库的前提下，按用户拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewUserProductSettings(userID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveUserProductSettings(userID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetUserProductSetting(userID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return err
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockPricingInputs(profile.ID, []uint{productID}); err != nil {
			return err
		}
		return store.DeleteSetting(profile.ID, productID, skuID)
	})
}

func (s *ProductSettingService) ListAdminSettings(input ProductSettingAdminListInput) ([]resellerdomain.ProductSetting, int64, error) {
	return s.store.ListAdminSettings(resellercontract.ProductSettingAdminListFilter(input))
}

func (s *ProductSettingService) SummarizeAdminSettings(resellerID uint) (resellercontract.ProductSettingSummary, error) {
	if s == nil || s.store == nil || resellerID == 0 {
		return resellercontract.ProductSettingSummary{}, nil
	}
	return s.store.SummarizeByResellerID(resellerID)
}

func (s *ProductSettingService) GetAdminProductSetting(resellerID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewAdminProductSettings 在不落库的前提下，按管理员拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetAdminProductSetting(resellerID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return err
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockPricingInputs(profile.ID, []uint{productID}); err != nil {
			return err
		}
		return store.DeleteSetting(profile.ID, productID, skuID)
	})
}

func (s *ProductSettingService) requireActiveProfileByUser(userID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) requireActiveProfileByID(resellerID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || resellerID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, productcontract.ErrNotFound
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) getDetail(profile *resellerdomain.Profile, productID uint) (*ProductSettingDetail, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	effective, rules, err := computeProductEffectivePrices(profile, row.Product, row.Settings)
	if err != nil {
		return nil, err
	}
	return &ProductSettingDetail{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules}, nil
}

func (s *ProductSettingService) decorateRows(profile *resellerdomain.Profile, rows []resellercontract.ProductSettingProductRow) []ProductSettingListRow {
	out := make([]ProductSettingListRow, 0, len(rows))
	for _, row := range rows {
		effective, rules, _ := computeProductEffectivePrices(profile, row.Product, row.Settings)
		out = append(out, ProductSettingListRow{Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules})
	}
	return out
}

func (s *ProductSettingService) saveSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) error {
	if profile == nil || productID == 0 {
		return productcontract.ErrNotFound
	}
	if len(input.Settings) == 0 {
		return resellercontract.ErrPricingModeInvalid
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		if err := store.LockPricingInputs(profile.ID, []uint{productID}); err != nil {
			return err
		}
		row, err := store.GetProductWithSettings(profile.ID, productID)
		if err != nil {
			return err
		}
		if row == nil {
			return productcontract.ErrNotFound
		}
		profile, err := store.GetProfileByID(profile.ID)
		if err != nil {
			return err
		}
		if profile == nil || profile.Status != resellerdomain.ProfileStatusActive {
			return resellercontract.ErrProfileInactive
		}
		parents, skus := resellercontract.BuildSettingIndexes(row.Settings)
		// 版本校验使用保存前的展示状态，避免本批父级修改使后续 SKU 误报冲突。
		originalParent := parents[productID]
		seen := map[uint]bool{}
		for _, item := range input.Settings {
			if seen[item.SKUID] {
				return resellercontract.ErrPricingModeInvalid
			}
			seen[item.SKUID] = true
			var current *resellerdomain.ProductSetting
			if item.SKUID == 0 {
				current = parents[productID]
			} else {
				current = skus[resellercontract.SettingKey{ProductID: productID, SKUID: item.SKUID}]
			}
			setting := resellerdomain.ProductSetting{ResellerID: profile.ID, ProductID: productID, SKUID: item.SKUID, IsListed: item.IsListed, PricingMode: resellerdomain.PricingModeInherit, SortOrder: item.SortOrder}
			if current != nil {
				setting = *current
				setting.IsListed = item.IsListed
			}
			if item.SKUID == 0 {
				if item.PricingMode != "" && item.PricingMode != resellerdomain.PricingModeInherit {
					return resellercontract.ErrPricingModeInvalid
				}
				setting.PricingMode = resellerdomain.PricingModeInherit
			} else {
				sku := findProductSKU(row.Product.SKUs, item.SKUID)
				if sku == nil {
					return productcontract.ErrProductSKUInvalid
				}
				quote := resellercontract.ResolveSupplyQuote(profile, *sku, originalParent, current)
				if !item.VisibilityOnly {
					if item.PricingMode != resellerdomain.PricingModeFixedPrice {
						return resellercontract.ErrPricingModeInvalid
					}
					if item.PricingRevision == "" || item.PricingRevision != quote.Revision {
						return resellercontract.ErrPricingChanged
					}
					if !item.FixedPriceAmount.Equal(item.FixedPriceAmount.Round(2)) || item.FixedPriceAmount.Abs().GreaterThanOrEqual(decimal.NewFromInt(1).Shift(18)) {
						return resellercontract.ErrPriceBelowBase
					}
					setting.PricingMode = resellerdomain.PricingModeFixedPrice
					setting.FixedPriceAmount = money.FromDecimal(item.FixedPriceAmount)
				}
				if !item.VisibilityOnly || setting.IsListed {
					if err := resellercontract.ResolveSupplyQuote(profile, *sku, parents[productID], &setting).ValidationError(); err != nil {
						return err
					}
				}
			}
			saved, err := store.UpsertSetting(setting)
			if err != nil {
				return err
			}
			if item.SKUID == 0 {
				parents[productID] = saved
			} else {
				skus[resellercontract.SettingKey{ProductID: productID, SKUID: item.SKUID}] = saved
			}
		}
		return nil
	})
}

func (s *ProductSettingService) previewSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	merged := append([]resellerdomain.ProductSetting(nil), row.Settings...)
	for _, item := range input.Settings {
		if item.SKUID != 0 && findProductSKU(row.Product.SKUs, item.SKUID) == nil {
			return nil, productcontract.ErrProductSKUInvalid
		}
		next, err := buildPreviewSetting(item)
		if err != nil {
			return nil, err
		}
		next.ProductID = productID
		replaced := false
		for i := range merged {
			if merged[i].SKUID == item.SKUID {
				if item.VisibilityOnly {
					next = merged[i]
					next.IsListed = item.IsListed
				}
				merged[i] = next
				replaced = true
				break
			}
		}
		if !replaced {
			merged = append(merged, next)
		}
	}
	parents, skus := resellercontract.BuildSettingIndexes(merged)
	out := make([]ProductSettingPreviewItem, 0, len(row.Product.SKUs))
	for _, sku := range row.Product.SKUs {
		quote := resellercontract.ResolveSupplyQuote(profile, sku, parents[productID], skus[resellercontract.SettingKey{ProductID: productID, SKUID: sku.ID}])
		base := decimal.Zero
		if quote.Supply != nil {
			base = quote.Supply.Decimal
		}
		out = append(out, ProductSettingPreviewItem{SKUID: sku.ID, IsListed: quote.CanSell, BasePrice: base, EffectivePrice: quote.Retail.Decimal, Valid: quote.Valid, ErrorCode: quote.ErrorCode})
	}
	return out, nil
}

func buildPreviewSetting(input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeFixedPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	return resellerdomain.ProductSetting{
		SKUID:             input.SKUID,
		IsListed:          input.IsListed,
		PricingMode:       mode,
		MarkupPercent:     money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount: money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:  money.FromDecimal(input.FixedPriceAmount.Round(2)),
		SortOrder:         input.SortOrder,
	}, nil
}

func computeProductEffectivePrices(profile *resellerdomain.Profile, product productdomain.Product, settings []resellerdomain.ProductSetting) (map[uint]decimal.Decimal, map[uint]string, error) {
	effective := map[uint]decimal.Decimal{}
	rules := map[uint]string{}
	parents, skus := resellercontract.BuildSettingIndexes(settings)
	for _, sku := range product.SKUs {
		quote := resellercontract.ResolveSupplyQuote(profile, sku, parents[product.ID], skus[resellercontract.SettingKey{ProductID: product.ID, SKUID: sku.ID}])
		if quote.Valid {
			effective[sku.ID] = quote.Retail.Decimal
			rules[sku.ID] = RuleSourceSKU
		}
	}
	return effective, rules, nil
}

func findProductSKU(items []productdomain.ProductSKU, skuID uint) *productdomain.ProductSKU {
	for i := range items {
		if items[i].ID == skuID {
			return &items[i]
		}
	}
	return nil
}
