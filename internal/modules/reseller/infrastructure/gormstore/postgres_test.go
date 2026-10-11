package gormstore

import (
	"fmt"
	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	"os"
	"strings"
	"testing"
	"time"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestResellerRepositoryPostgresActiveUniqueIndex(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("skip postgres integration test: TEST_POSTGRES_DSN is empty")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	if err := db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &resellerdomain.Domain{}, &resellerdomain.SiteConfig{}); err != nil {
		t.Fatalf("migrate postgres failed: %v", err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_domains_active_domain ON reseller_domains(domain) WHERE deleted_at IS NULL").Error; err != nil {
		t.Fatalf("create postgres active domain index failed: %v", err)
	}
	suffix := time.Now().Format("20060102150405")
	profile := seedResellerProfile(t, db, "pg-reseller-"+suffix+"@example.com")
	repo := New(db)
	first, err := repo.UpsertDomain(resellerdomain.Domain{
		ResellerID:         profile.ID,
		Domain:             "pg-" + suffix + ".example.test",
		Type:               resellerdomain.DomainTypeCustom,
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
	})
	if err != nil {
		t.Fatalf("create postgres domain failed: %v", err)
	}
	now := time.Now()
	if err := db.Model(&resellerdomain.Domain{}).Where("id = ?", first.ID).Update("deleted_at", &now).Error; err != nil {
		t.Fatalf("soft delete postgres domain failed: %v", err)
	}
	second, err := repo.UpsertDomain(resellerdomain.Domain{
		ResellerID:         profile.ID,
		Domain:             first.Domain,
		Type:               resellerdomain.DomainTypeCustom,
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
	})
	if err != nil {
		t.Fatalf("restore postgres domain failed: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected restore same row id=%d got id=%d", first.ID, second.ID)
	}
}

func TestResellerRepositoryPostgresWithdrawLocksSameRows(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	if err := db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &resellerdomain.LedgerEntry{}, &resellerdomain.BalanceAccount{}, &resellerdomain.WithdrawRequest{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	profile := seedResellerProfile(t, db, "pg-reseller-withdraw-"+suffix+"@example.com")
	now := time.Now()
	entry := resellerdomain.LedgerEntry{
		ResellerID:     profile.ID,
		Type:           resellerdomain.LedgerTypeOrderProfit,
		Amount:         money.FromDecimal(decimal.NewFromInt(100)),
		Currency:       "USD",
		IdempotencyKey: "pg-order-profit-" + suffix,
		Status:         resellerdomain.LedgerStatusAvailable,
		AvailableAt:    &now,
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create ledger failed: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		repoTx := New(tx)
		if _, err := repoTx.GetOrCreateBalanceAccountForUpdate(profile.ID, "USD"); err != nil {
			return err
		}
		rows, err := repoTx.ListAvailableLedgerEntriesForUpdate(profile.ID, "USD")
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].ID != entry.ID {
			t.Fatalf("expected locked ledger %d, got %+v", entry.ID, rows)
		}
		return nil
	}); err != nil {
		t.Fatalf("transaction failed: %v", err)
	}
}

func TestResellerSupplyQuotePostgresLocksAndPreservesConcurrentQuote(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN is empty")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("supply_lock_%d", time.Now().UnixNano())
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { base.Exec("DROP SCHEMA " + schema + " CASCADE"); conn, _ := base.DB(); conn.Close() })
	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	conn, _ := db.DB()
	conn.SetMaxOpenConns(3)
	t.Cleanup(func() { conn.Close() })
	if err := db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &categorydomain.Category{}, &productdomain.Product{}, &productdomain.ProductSKU{}, &resellerdomain.ProductSetting{}); err != nil {
		t.Fatal(err)
	}
	profile := seedResellerProfile(t, db, "supply-lock@example.test")
	product, skus := seedResellerPricingProduct(t, db, "lock-product", 1)
	locked := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- db.Transaction(func(tx *gorm.DB) error {
			if err := New(tx).LockPricingInputs(profile.ID, []uint{product.ID}); err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-firstDone:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("lock timeout")
	}
	started := make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		close(started)
		writeDone <- db.Model(&productdomain.ProductSKU{}).Where("id = ?", skus[0].ID).Update("supply_price_amount", "110.00").Error
	}()
	<-started
	select {
	case err := <-writeDone:
		close(release)
		t.Fatal("quote write escaped pricing lock", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quote write remained blocked")
	}
	var stored productdomain.ProductSKU
	if err := db.First(&stored, skus[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SupplyPriceAmount == nil || stored.SupplyPriceAmount.String() != "110.00" {
		t.Fatal(stored)
	}
}
