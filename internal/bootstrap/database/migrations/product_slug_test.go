package migrations

import (
	"testing"
	"time"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

func TestAutoMigrateReleasesLegacyDeletedProductSlugs(t *testing.T) {
	db := setupSKUMigrationTestDB(t)
	if err := db.AutoMigrate(&productdomain.Product{}); err != nil {
		t.Fatal(err)
	}
	// Reproduce the global unique index used by existing installations.
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_products_slug ON products (slug)").Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	deleted := productdomain.Product{CategoryID: 1, Slug: "chatgpt-plus", TitleJSON: jsonmap.JSON{"zh-CN": "test"}, DeletedAt: &now}
	if err := db.Create(&deleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(); err != nil {
		t.Fatalf("upgrade legacy schema: %v", err)
	}
	if db.Migrator().HasIndex(&productdomain.Product{}, "idx_products_slug") {
		t.Fatal("legacy global slug index must be removed")
	}
	replacement := productdomain.Product{CategoryID: 1, Slug: deleted.Slug, TitleJSON: deleted.TitleJSON}
	if err := db.Create(&replacement).Error; err != nil {
		t.Fatalf("reuse legacy deleted slug: %v", err)
	}
	if err := AutoMigrate(); err != nil {
		t.Fatalf("migration must remain safe after slug reuse: %v", err)
	}
	duplicate := productdomain.Product{CategoryID: 1, Slug: deleted.Slug, TitleJSON: deleted.TitleJSON}
	if err := db.Create(&duplicate).Error; err == nil {
		t.Fatal("active slug must remain unique after repeated migrations")
	}
	var historical productdomain.Product
	if err := db.First(&historical, deleted.ID).Error; err != nil || historical.DeletedAt == nil || historical.Slug != deleted.Slug {
		t.Fatalf("historical row must remain unchanged: %+v err=%v", historical, err)
	}
}
