package migrations

import (
	"testing"
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestAutoMigrateRestoresGlobalProductSlugUniqueness(t *testing.T) {
	db := setupSKUMigrationTestDB(t)
	if err := db.AutoMigrate(&productdomain.Product{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP INDEX idx_products_slug").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX idx_products_active_slug ON products (slug) WHERE deleted_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	deleted := productdomain.Product{CategoryID: 1, Slug: "chatgpt-plus", TitleJSON: jsonmap.JSON{"zh-CN": "test"}, DeletedAt: &now}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := AutoMigrate(); err != nil {
			t.Fatalf("upgrade and repeated migration: %v", err)
		}
		if db.Migrator().HasIndex(&productdomain.Product{}, "idx_products_active_slug") {
			t.Fatal("previous active-only index must be removed")
		}
		duplicate := productdomain.Product{CategoryID: 1, Slug: deleted.Slug, TitleJSON: deleted.TitleJSON}
		if err := db.Create(&duplicate).Error; err == nil {
			t.Fatal("deleted product must retain its globally unique slug")
		}
	}
	var historical productdomain.Product
	if err := db.First(&historical, deleted.ID).Error; err != nil || historical.DeletedAt == nil || historical.Slug != deleted.Slug {
		t.Fatalf("migration must preserve the deleted product: %+v err=%v", historical, err)
	}
}
