package integrationtest

import (
	"context"
	"errors"
	"fmt"
	"github.com/dujiao-next/internal/config"
	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"sync"
	"testing"
	"time"
)

func domainCapacityManagement(db *gorm.DB, limit int) *resellerapp.ManagementService {
	return resellerapp.NewManagementService(gormstore.New(db), config.ResellerConfig{Enabled: true, SelfApplyEnabled: true, DomainBinding: config.ResellerDomainBindingConfig{Enabled: true, CNAMETarget: "entry.example.test", MaxHostnames: limit}})
}
func TestDomainSubmissionStopsAtPerResellerLimitBeforeRemoteCreate(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	fake.ready = false
	for i := 0; i < 5; i++ {
		if _, err := management.SubmitUserCustomDomain(profile.UserID, fmt.Sprintf("limit-%d.example.test", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := management.SubmitUserCustomDomain(profile.UserID, "excess.example.test"); !errors.Is(err, resellercontract.ErrDomainLimit) {
		t.Fatalf("sixth root must be rejected, got %v", err)
	}
	sweepDomains(t, db, worker)
	if len(fake.creates) != 10 {
		t.Fatalf("created %d hosts, want exactly 5 pairs", len(fake.creates))
	}
}
func runConcurrentDomainCapacity(t *testing.T, db *gorm.DB) {
	t.Helper()
	management := domainCapacityManagement(db, 4)
	ctx := context.Background()
	users := make([]userdomain.User, 3)
	for i := range users {
		users[i] = userdomain.User{Email: fmt.Sprintf("capacity-%d-%d@example.test", time.Now().UnixNano(), i), PasswordHash: "unused"}
		if err := db.Create(&users[i]).Error; err != nil {
			t.Fatal(err)
		}
		profile, err := management.ApplyUserReseller(users[i].ID, resellerapp.ResellerApplyInput{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = management.ApproveProfile(ctx, 9, profile.ID, resellerapp.ResellerApproveInput{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := management.SubmitUserCustomDomain(users[0].ID, "initial.example.test"); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 1; i < 3; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := management.SubmitUserCustomDomain(users[i].ID, fmt.Sprintf("concurrent-%d.example.test", i))
			results <- err
		}(i)
	}
	wg.Wait()
	close(results)
	accepted, limited := 0, 0
	for err := range results {
		if err == nil {
			accepted++
		} else if errors.Is(err, resellercontract.ErrDomainLimit) {
			limited++
		} else {
			t.Fatalf("unexpected submission error: %v", err)
		}
	}
	count, err := gormstore.New(db).CountReservedCustomHostnames()
	if err != nil || accepted != 1 || limited != 1 || count != 4 {
		t.Fatalf("accepted=%d limited=%d reserved=%d err=%v", accepted, limited, count, err)
	}
}
func TestDomainGlobalCapacitySerializesConcurrentSQLiteReservations(t *testing.T) {
	db, _, _, _, _ := onboardingFixture(t)
	runConcurrentDomainCapacity(t, db)
}
func TestDomainGlobalCapacitySerializesConcurrentPostgresReservations(t *testing.T) {
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
	if err := db.AutoMigrate(&userdomain.User{}, &resellerdomain.Profile{}, &resellerdomain.Domain{}, &resellerdomain.DomainCapacity{}); err != nil {
		t.Fatal(err)
	}
	runConcurrentDomainCapacity(t, db)
}
func TestDomainCapacityIsHeldUntilRemoteCleanup(t *testing.T) {
	db, _, worker, profile, fake := onboardingFixture(t)
	management := domainCapacityManagement(db, 2)
	fake.ready = false
	root, err := management.SubmitUserCustomDomain(profile.UserID, "held.example.test")
	if err != nil {
		t.Fatal(err)
	}
	sweepDomains(t, db, worker)
	if _, err := management.DisableDomain(context.Background(), 9, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := management.SubmitUserCustomDomain(profile.UserID, "blocked.example.test"); !errors.Is(err, resellercontract.ErrDomainLimit) {
		t.Fatalf("remote cleanup must retain quota, got %v", err)
	}
	sweepDomains(t, db, worker)
	if len(fake.deleted) != 2 {
		t.Fatalf("deleted %d hosts, want both", len(fake.deleted))
	}
	if _, err := management.SubmitUserCustomDomain(profile.UserID, "replacement.example.test"); err != nil {
		t.Fatalf("cleaned capacity must be reusable: %v", err)
	}
}
func TestUnverifiedDomainsExpireWithoutClosingApprovedStores(t *testing.T) {
	for _, approved := range []bool{false, true} {
		t.Run(fmt.Sprint(approved), func(t *testing.T) {
			db, management, worker, profile, fake := onboardingFixture(t)
			fake.ready = approved
			root, err := management.SubmitUserCustomDomain(profile.UserID, "expiry.example.test")
			if err != nil {
				t.Fatal(err)
			}
			sweepDomains(t, db, worker)
			if approved {
				if _, err := management.ApproveDomain(context.Background(), 9, root.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Model(&resellerdomain.Domain{}).Where("type = ?", resellerdomain.DomainTypeCustom).Update("created_at", time.Now().Add(-169*time.Hour)).Error; err != nil {
				t.Fatal(err)
			}
			sweepDomains(t, db, worker)
			rows, err := gormstore.New(db).ListDomainsByResellerID(profile.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				if row.Type != resellerdomain.DomainTypeCustom {
					continue
				}
				if approved && row.Status != resellerdomain.DomainStatusActive {
					t.Fatal("expiry closed an approved store")
				}
				if !approved && (row.Status != resellerdomain.DomainStatusDisabled || row.ExternalHostnameID != "") {
					t.Fatalf("expired row not cleaned: %+v", row)
				}
			}
			if approved && len(fake.deleted) != 0 {
				t.Fatal("expiry deleted approved remote hostnames")
			}
			if !approved && len(fake.deleted) != 2 {
				t.Fatal("expiry must delete both pending hostnames")
			}
		})
	}
}
