package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	resellermodule "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func autoDomainFixture(t *testing.T) (*gorm.DB, *ResellerManagementService, *resellerdomain.Profile) {
	t.Helper()
	db := openResellerManagementServiceTestDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.Exec("CREATE UNIQUE INDEX idx_auto_domain ON reseller_domains(domain) WHERE deleted_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	svc := newResellerManagementServiceForTest(db)
	user := seedResellerManagementUser(t, db, "auto-domain@example.test")
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{})
	if err != nil {
		t.Fatal(err)
	}
	return db, svc, profile
}

func TestResellerAutoDomainPreservesUnconfiguredApproval(t *testing.T) {
	db, _, profile := autoDomainFixture(t)
	svc := NewResellerManagementService(resellergormstore.New(db), config.ResellerConfig{Enabled: true, SelfApplyEnabled: true})
	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{})
	if err != nil {
		t.Fatal(err)
	}
	if approved.SystemDomain != nil || approved.Profile.Status != resellerdomain.ProfileStatusActive {
		t.Fatalf("unexpected approval: %+v", approved)
	}
}

func TestResellerAutoDomainReusesAssignedDomain(t *testing.T) {
	_, svc, profile := autoDomainFixture(t)
	assigned, err := svc.AssignSystemSubdomain(context.Background(), 9, profile.ID, ResellerSystemDomainInput{Subdomain: "brand"})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{})
	if err != nil {
		t.Fatal(err)
	}
	if approved.SystemDomain == nil || approved.SystemDomain.ID != assigned.ID || approved.SystemDomain.Domain != assigned.Domain {
		t.Fatalf("assigned domain changed: %+v", approved)
	}
}

func TestResellerAutoDomainPreservesCustomPrimary(t *testing.T) {
	db, svc, profile := autoDomainFixture(t)
	custom := resellerdomain.Domain{ResellerID: profile.ID, Domain: "customer.example.test", Type: resellerdomain.DomainTypeCustom, Status: resellerdomain.DomainStatusActive, VerificationStatus: resellerdomain.DomainVerificationVerified, IsPrimary: true}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatal(err)
	}
	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{})
	if err != nil {
		t.Fatal(err)
	}
	if approved.SystemDomain == nil || approved.SystemDomain.IsPrimary {
		t.Fatalf("custom primary displaced: %+v", approved)
	}
	if err := db.First(&custom, custom.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !custom.IsPrimary {
		t.Fatal("custom domain lost primary status")
	}
}

func TestResellerAutoDomainFailureRollsBackApproval(t *testing.T) {
	for _, scenario := range []string{"conflict", "write_failure"} {
		t.Run(scenario, func(t *testing.T) {
			db, svc, profile := autoDomainFixture(t)
			failure := resellercontract.ErrDomainConflict
			if scenario == "conflict" {
				user := seedResellerManagementUser(t, db, "domain-owner@example.test")
				owner, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.AssignSystemSubdomain(context.Background(), 9, owner.ID, ResellerSystemDomainInput{Subdomain: fmt.Sprintf("r%d", profile.ID)}); err != nil {
					t.Fatal(err)
				}
			} else {
				failure = errors.New("domain write failed")
				if err := db.Callback().Create().Before("gorm:create").Register("fail_domain", func(tx *gorm.DB) {
					if tx.Statement.Table == "reseller_domains" {
						tx.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{DefaultMarkupPercent: decimal.NewFromInt(12)}); !errors.Is(err, failure) {
				t.Fatalf("expected %v, got %v", failure, err)
			}
			var loaded resellerdomain.Profile
			if err := db.First(&loaded, profile.ID).Error; err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&resellerdomain.Domain{}).Where("reseller_id = ?", profile.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if loaded.Status != resellerdomain.ProfileStatusPendingReview || loaded.ReviewedBy != nil || loaded.ReviewedAt != nil || !loaded.DefaultMarkupPercent.Decimal.IsZero() || count != 0 {
				t.Fatalf("approval not rolled back: profile=%+v domains=%d", loaded, count)
			}
		})
	}
}

func TestResellerAutoDomainConcurrentApprovals(t *testing.T) {
	db, svc, first := autoDomainFixture(t)
	runConcurrentAutoDomainApprovals(t, db, svc, first)
}

func TestResellerAutoDomainConcurrentApprovalsPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(8)
	t.Cleanup(func() { _ = pool.Close() })
	if err := db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &resellerdomain.Domain{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_domain ON reseller_domains(domain) WHERE deleted_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	svc := newResellerManagementServiceForTest(db)
	user := seedResellerManagementUser(t, db, fmt.Sprintf("pg-auto-%d@example.test", time.Now().UnixNano()))
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{})
	if err != nil {
		t.Fatal(err)
	}
	runConcurrentAutoDomainApprovals(t, db, svc, profile)
}

func runConcurrentAutoDomainApprovals(t *testing.T, db *gorm.DB, svc *ResellerManagementService, first *resellerdomain.Profile) {
	t.Helper()
	ids := []uint{first.ID}
	for i := 1; i < 4; i++ {
		user := seedResellerManagementUser(t, db, fmt.Sprintf("parallel-%d-%d@example.test", first.ID, i))
		profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, profile.ID)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	results := make(chan *resellermodule.ResellerApproveResult, 8)
	failures := make(chan error, 8)
	for _, id := range append(ids, ids...) {
		workers.Add(1)
		go func(id uint) {
			defer workers.Done()
			<-start
			result, err := svc.ApproveProfile(context.Background(), 9, id, ResellerApproveInput{DefaultMarkupPercent: decimal.NewFromInt(12)})
			if err != nil {
				failures <- err
				return
			}
			results <- result
		}(id)
	}
	close(start)
	workers.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	approved, repeated := 0, 0
	for result := range results {
		if result.SystemDomain == nil {
			t.Fatal("missing system domain")
		}
		if result.AlreadyApproved {
			repeated++
		} else {
			approved++
		}
	}
	if approved != 4 || repeated != 4 {
		t.Fatalf("approved=%d repeated=%d", approved, repeated)
	}
	var count int64
	if err := db.Model(&resellerdomain.Domain{}).Where("reseller_id IN ?", ids).Count(&count).Error; err != nil || count != 4 {
		t.Fatalf("domains=%d err=%v", count, err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 10, first.ID, ResellerApproveInput{DefaultMarkupPercent: decimal.NewFromInt(25)}); err != nil {
		t.Fatal(err)
	}
	profile, err := resellergormstore.New(db).GetProfileByID(first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if profile.DefaultMarkupPercent.Decimal.String() != "12" || profile.ReviewedBy == nil || *profile.ReviewedBy != 9 {
		t.Fatalf("repeat changed approval: %+v", profile)
	}
}
