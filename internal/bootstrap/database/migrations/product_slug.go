package migrations

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/platform/database/gormdb"
)

func ensureProductSlugUniqueIndex() error {
	// AutoMigrate creates idx_products_active_slug first, preserving uniqueness
	// for live products before releasing slugs held by historical deleted rows.
	migrator := gormdb.DB.Migrator()
	if migrator.HasIndex(&productdomain.Product{}, "idx_products_slug") {
		return migrator.DropIndex(&productdomain.Product{}, "idx_products_slug")
	}
	return nil
}
