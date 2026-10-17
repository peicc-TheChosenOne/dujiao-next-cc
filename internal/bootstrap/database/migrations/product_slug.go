package migrations

import (
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"github.com/dujiao-next/internal/platform/database/gormdb"
)

func ensureProductSlugUniqueIndex() error {
	// AutoMigrate restores the global index first. Creation now restores deleted
	// products in place, so remove the previous active-only uniqueness rule.
	migrator := gormdb.DB.Migrator()
	if migrator.HasIndex(&productdomain.Product{}, "idx_products_active_slug") {
		return migrator.DropIndex(&productdomain.Product{}, "idx_products_active_slug")
	}
	return nil
}
