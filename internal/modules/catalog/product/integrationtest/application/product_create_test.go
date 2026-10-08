package integrationtest

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/shopspring/decimal"
)

func TestProductServiceCreateRestoresDeletedSlug(t *testing.T) {
	for _, multiSKU := range []bool{false, true} {
		t.Run(fmt.Sprintf("multi_sku=%t", multiSKU), func(t *testing.T) {
			svc, db := newProductServiceForTest(t)
			category := categorydomain.Category{Slug: "slug-restore", NameJSON: jsonmap.JSON{"zh-CN": "test"}}
			if err := db.Create(&category).Error; err != nil {
				t.Fatal(err)
			}
			input := productwrite.CreateProductInput{
				CategoryID: category.ID, Slug: "chatgpt-plus",
				TitleJSON:       map[string]interface{}{"zh-CN": "Old title"},
				DescriptionJSON: map[string]interface{}{"zh-CN": "Old description"},
				Images:          []string{"old.png"}, SortOrder: 30,
				PriceAmount: decimal.NewFromInt(10), PurchaseType: constants.ProductPurchaseMember,
				FulfillmentType: constants.FulfillmentTypeManual,
			}
			if multiSKU {
				input.SKUs = []productwrite.ProductSKUInput{{SKUCode: "PLUS", PriceAmount: decimal.NewFromInt(10)}}
			}
			original, err := svc.Write.Create(input)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := svc.Admin.Delete(strconv.FormatUint(uint64(original.ID), 10)); err != nil {
					t.Fatal(err)
				}
				input.TitleJSON = map[string]interface{}{"zh-CN": "Updated title"}
				input.DescriptionJSON = nil
				input.Images = nil
				input.SortOrder = 0
				input.PriceAmount = decimal.NewFromInt(20)
				if multiSKU {
					input.SKUs[0].PriceAmount = decimal.NewFromInt(20)
				}
				restored, err := svc.Write.Create(input)
				if err != nil {
					t.Fatalf("restore deleted product: %v", err)
				}
				if restored.ID != original.ID || !restored.CreatedAt.Equal(original.CreatedAt) || restored.DeletedAt != nil || !restored.IsActive {
					t.Fatalf("restored product must preserve identity and creation time: %+v", restored)
				}
				if restored.TitleJSON["zh-CN"] != "Updated title" || len(restored.DescriptionJSON) != 0 || len(restored.Images) != 0 || restored.SortOrder != 0 || !restored.PriceAmount.Equal(decimal.NewFromInt(20)) {
					t.Fatalf("restore must apply new values including empty and zero fields: %+v", restored)
				}
				if len(restored.SKUs) != 1 || restored.SKUs[0].ProductID != original.ID || !restored.SKUs[0].PriceAmount.Equal(decimal.NewFromInt(20)) {
					t.Fatalf("restored product must have the requested SKU: %+v", restored.SKUs)
				}
				var count int64
				if err := db.Model(&productdomain.Product{}).Where("slug = ?", input.Slug).Count(&count).Error; err != nil || count != 1 {
					t.Fatalf("restore must not create another product: count=%d err=%v", count, err)
				}
				if _, err := svc.Write.Create(input); !errors.Is(err, productcontract.ErrSlugExists) {
					t.Fatalf("active duplicate must return ErrSlugExists, got %v", err)
				}
			}
		})
	}
}

func TestProductServiceRestoreRollsBackWhenSKUInputFails(t *testing.T) {
	svc, db := newProductServiceForTest(t)
	category := categorydomain.Category{Slug: "restore-rollback", NameJSON: jsonmap.JSON{"zh-CN": "test"}}
	if err := db.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	input := productwrite.CreateProductInput{
		CategoryID: category.ID, Slug: "rollback-product",
		TitleJSON: map[string]interface{}{"zh-CN": "Original"}, PriceAmount: decimal.NewFromInt(10),
		PurchaseType: constants.ProductPurchaseMember, FulfillmentType: constants.FulfillmentTypeManual,
	}
	original, err := svc.Write.Create(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Admin.Delete(strconv.FormatUint(uint64(original.ID), 10)); err != nil {
		t.Fatal(err)
	}
	input.TitleJSON = map[string]interface{}{"zh-CN": "Must not persist"}
	input.SKUs = []productwrite.ProductSKUInput{{ID: 999999, SKUCode: "INVALID", PriceAmount: decimal.NewFromInt(20)}}
	if _, err := svc.Write.Create(input); !errors.Is(err, productcontract.ErrProductSKUInvalid) {
		t.Fatalf("expected invalid SKU error, got %v", err)
	}
	var persisted productdomain.Product
	if err := db.First(&persisted, original.ID).Error; err != nil || persisted.DeletedAt == nil || persisted.TitleJSON["zh-CN"] != "Original" {
		t.Fatalf("failed restoration must leave the product deleted and unchanged: %+v err=%v", persisted, err)
	}
	var sku productdomain.ProductSKU
	if err := db.First(&sku, original.SKUs[0].ID).Error; err != nil || sku.DeletedAt == nil {
		t.Fatalf("failed restoration must retain the deleted SKU: %+v err=%v", sku, err)
	}
}

func TestProductServiceCreateRejectsParentCategoryWithChildren(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	parent := categorydomain.Category{
		Slug:     "games",
		NameJSON: jsonmap.JSON{"zh-CN": "games"},
	}
	if err := db.Create(&parent).Error; err != nil {
		t.Fatalf("create parent category failed: %v", err)
	}
	child := categorydomain.Category{
		ParentID: parent.ID,
		Slug:     "steam",
		NameJSON: jsonmap.JSON{"zh-CN": "steam"},
	}
	if err := db.Create(&child).Error; err != nil {
		t.Fatalf("create child category failed: %v", err)
	}

	_, err := svc.Write.Create(productwrite.CreateProductInput{
		CategoryID:      parent.ID,
		Slug:            "invalid-parent-product",
		TitleJSON:       map[string]interface{}{"zh-CN": "invalid-parent-product"},
		PriceAmount:     decimal.NewFromInt(10),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeManual,
		ManualStockTotal: func() *int {
			value := 1
			return &value
		}(),
	})
	if err != productcontract.ErrProductCategoryInvalid {
		t.Fatalf("expected productcontract.ErrProductCategoryInvalid, got %v", err)
	}
}

func TestProductServiceCreateFiltersUnavailablePaymentChannels(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "payment-channel-category",
		NameJSON: jsonmap.JSON{"zh-CN": "payment-channel-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	activeChannel := createProductTestPaymentChannel(t, db, "Active", true, false)
	inactiveChannel := createProductTestPaymentChannel(t, db, "Inactive", false, false)
	deletedChannel := createProductTestPaymentChannel(t, db, "Deleted", true, true)

	product, err := svc.Write.Create(productwrite.CreateProductInput{
		CategoryID:        category.ID,
		Slug:              "payment-channel-create",
		TitleJSON:         map[string]interface{}{"zh-CN": "payment-channel-create"},
		PriceAmount:       decimal.NewFromInt(10),
		PurchaseType:      constants.ProductPurchaseMember,
		FulfillmentType:   constants.FulfillmentTypeAuto,
		PaymentChannelIDs: []uint{deletedChannel.ID, inactiveChannel.ID, activeChannel.ID},
		IsActive: func() *bool {
			value := true
			return &value
		}(),
	})
	if err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	got := productdomain.DecodePaymentChannelIDs(product.PaymentChannelIDs)
	if len(got) != 1 || got[0] != activeChannel.ID {
		t.Fatalf("expected only active payment channel %d, got %v", activeChannel.ID, got)
	}
}

func TestProductServiceCreateRejectsInvalidPurchaseLimits(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	cat := categorydomain.Category{Slug: "test-purchase-limit", NameJSON: jsonmap.JSON{"zh-CN": "test"}}
	if err := db.Create(&cat).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	intPtr := func(v int) *int { return &v }

	_, err := svc.Write.Create(productwrite.CreateProductInput{
		CategoryID:          cat.ID,
		Slug:                "invalid-limit-product",
		TitleJSON:           map[string]interface{}{"zh-CN": "invalid-limit-product"},
		PriceAmount:         decimal.NewFromInt(10),
		PurchaseType:        constants.ProductPurchaseMember,
		FulfillmentType:     constants.FulfillmentTypeManual,
		ManualStockTotal:    intPtr(1),
		MinPurchaseQuantity: intPtr(10),
		MaxPurchaseQuantity: intPtr(5),
	})
	if !errors.Is(err, productcontract.ErrProductPurchaseLimitInvalid) {
		t.Fatalf("expected productcontract.ErrProductPurchaseLimitInvalid, got %v", err)
	}
}

func TestProductServiceCreateRollsBackProductAndSKUWhenWholesaleValidationFails(t *testing.T) {
	svc, db := newProductServiceForTest(t)
	category := categorydomain.Category{
		Slug:     "write-rollback-category",
		NameJSON: jsonmap.JSON{"zh-CN": "write-rollback-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	_, err := svc.Write.Create(productwrite.CreateProductInput{
		CategoryID:      category.ID,
		Slug:            "write-rollback-product",
		TitleJSON:       map[string]interface{}{"zh-CN": "write-rollback-product"},
		PriceAmount:     decimal.NewFromInt(10),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		WholesalePrices: &[]productdomain.WholesalePriceInput{{
			MinQuantity: 0,
			UnitPrice:   decimal.NewFromInt(8),
		}},
	})
	if !errors.Is(err, productdomain.ErrWholesalePriceInvalid) {
		t.Fatalf("expected productdomain.ErrWholesalePriceInvalid, got %v", err)
	}

	var productCount int64
	if err := db.Model(&productdomain.Product{}).Where("slug = ?", "write-rollback-product").Count(&productCount).Error; err != nil {
		t.Fatalf("count products: %v", err)
	}
	if productCount != 0 {
		t.Fatalf("transaction must roll back product, got %d rows", productCount)
	}
	var skuCount int64
	if err := db.Model(&productdomain.ProductSKU{}).Count(&skuCount).Error; err != nil {
		t.Fatalf("count product SKUs: %v", err)
	}
	if skuCount != 0 {
		t.Fatalf("transaction must roll back default SKU, got %d rows", skuCount)
	}
}
