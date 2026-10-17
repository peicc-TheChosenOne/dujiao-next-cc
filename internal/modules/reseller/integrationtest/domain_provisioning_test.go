package integrationtest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	resellerapp "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"
	"gorm.io/gorm"
)

type hostnameFake struct {
	mu       sync.Mutex
	creates  map[string]int
	deleted  []string
	ready    bool
	failure  error
	onCreate func(string)
}

func (f *hostnameFake) state(host string) (resellercontract.HostnameState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := resellercontract.HostnameState{ID: host, Hostname: host, Status: "pending"}
	if f.ready {
		state.Status = "active"
		state.SSL.Status = "active"
	}
	return state, f.failure
}
func (f *hostnameFake) Create(ctx context.Context, host string) (resellercontract.HostnameState, error) {
	f.mu.Lock()
	f.creates[host]++
	f.mu.Unlock()
	if f.onCreate != nil {
		f.onCreate(host)
	}
	return f.state(host)
}
func (f *hostnameFake) Get(ctx context.Context, id string) (resellercontract.HostnameState, error) {
	return f.state(id)
}
func (f *hostnameFake) Delete(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "" {
		f.deleted = append(f.deleted, id)
	}
	return f.failure
}

func onboardingFixture(t *testing.T) (*gorm.DB, *resellerapp.ManagementService, *resellerapp.DomainProvisioningService, *resellerdomain.Profile, *hostnameFake) {
	t.Helper()
	db, _, profile := autoDomainFixture(t)
	cfg := config.ResellerConfig{Enabled: true, SelfApplyEnabled: true, MainHosts: []string{"main.example.test", "entry.example.test"}, SubdomainBase: "shop.example.test", DomainBinding: config.ResellerDomainBindingConfig{Enabled: true, CNAMETarget: "entry.example.test"}}
	store := gormstore.New(db)
	management := resellerapp.NewManagementService(store, cfg)
	if _, err := management.ApproveProfile(context.Background(), 9, profile.ID, resellerapp.ResellerApproveInput{}); err != nil {
		t.Fatal(err)
	}
	fake := &hostnameFake{creates: make(map[string]int), ready: true}
	worker := resellerapp.NewDomainProvisioningService(store, fake, cfg.DomainBinding)
	return db, management, worker, profile, fake
}

func sweepDomains(t *testing.T, db *gorm.DB, worker *resellerapp.DomainProvisioningService) {
	t.Helper()
	if err := db.Model(&resellerdomain.Domain{}).Where("type = ?", resellerdomain.DomainTypeCustom).Update("last_sync_at", nil).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := worker.SyncDueDomains(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDomainOnboardingRequiresBothHostnamesThenApproval(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	root, err := management.SubmitUserCustomDomain(profile.UserID, "WWW.Customer.Example.Test")
	if err != nil {
		t.Fatal(err)
	}
	if root.Domain != "customer.example.test" || root.Status != resellerdomain.DomainStatusProvisioning {
		t.Fatalf("unexpected submission: %+v", root)
	}
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatalf("unverified approval accepted: %v", err)
	}
	fake.ready = false
	sweepDomains(t, db, worker)
	fake.ready = true
	sweepDomains(t, db, worker)
	rows, _ := gormstore.New(db).ListDomainsByResellerID(profile.ID)
	for _, row := range rows {
		if row.Type != resellerdomain.DomainTypeCustom {
			continue
		}
		if row.Status != resellerdomain.DomainStatusPendingReview || row.VerificationStatus != resellerdomain.DomainVerificationVerified {
			t.Fatalf("expected review after validation: %+v", row)
		}
		resolved, err := resellerapp.NewDomainResolver(gormstore.New(db), config.ResellerConfig{Enabled: true}).ResolveHost(context.Background(), row.Domain)
		if err != nil || resolved.IsReseller() {
			t.Fatalf("store opened before approval: %+v %v", resolved, err)
		}
	}
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ = gormstore.New(db).ListDomainsByResellerID(profile.ID)
	for _, row := range rows {
		if row.Type == resellerdomain.DomainTypeCustom && row.Status != resellerdomain.DomainStatusActive {
			t.Fatal("paired approval was not atomic")
		}
	}
	fake.failure = errors.New("temporary provider failure")
	sweepDomains(t, db, worker)
	updated, _ := gormstore.New(db).GetDomainByID(root.ID)
	if updated.Status != resellerdomain.DomainStatusActive || updated.VerificationStatus != resellerdomain.DomainVerificationVerified || updated.ProvisioningError != "api_unavailable" {
		t.Fatalf("outage disabled approved store: %+v", updated)
	}
}

func TestDomainPairConflictRollsBackAndRefreshChecksOwnership(t *testing.T) {
	db, management, _, profile, _ := onboardingFixture(t)
	user := seedResellerManagementUser(t, db, "different-owner@example.test")
	owner, err := management.ApplyUserReseller(user.ID, resellerapp.ResellerApplyInput{})
	if err != nil {
		t.Fatal(err)
	}
	conflicting := resellerdomain.Domain{ResellerID: owner.ID, Domain: "www.conflict.example.test", Type: resellerdomain.DomainTypeCustom}
	if err := db.Create(&conflicting).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := management.SubmitUserCustomDomain(profile.UserID, "conflict.example.test"); !errors.Is(err, resellercontract.ErrDomainConflict) {
		t.Fatalf("conflict accepted: %v", err)
	}
	var count int64
	db.Model(&resellerdomain.Domain{}).Where("domain = ?", "conflict.example.test").Count(&count)
	if count != 0 {
		t.Fatal("root was created despite www conflict")
	}
	root, err := management.SubmitUserCustomDomain(profile.UserID, "owned.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := management.RefreshUserDomain(context.Background(), user.ID, root.ID); err == nil {
		t.Fatal("another reseller refreshed this domain")
	}
}

func TestDisablingDuringRemoteCreatePreservesIDForCleanup(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	root, err := management.SubmitUserCustomDomain(profile.UserID, "late.example.test")
	if err != nil {
		t.Fatal(err)
	}
	fake.onCreate = func(host string) {
		if host == root.Domain {
			if _, err := management.DisableDomain(context.Background(), 9, root.ID); err != nil {
				t.Error(err)
			}
		}
	}
	sweepDomains(t, db, worker)
	stored, _ := gormstore.New(db).GetDomainByID(root.ID)
	if stored.Status != resellerdomain.DomainStatusDisabled || stored.ExternalHostnameID == "" || stored.ProvisioningStatus != "deleting" {
		t.Fatalf("late create was lost or reactivated: %+v", stored)
	}
	if _, err := management.RefreshDomain(context.Background(), 9, root.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatal("cleanup bypassed")
	}
	sweepDomains(t, db, worker)
	stored, _ = gormstore.New(db).GetDomainByID(root.ID)
	if stored.ExternalHostnameID != "" || stored.Status != resellerdomain.DomainStatusDisabled || len(fake.deleted) != 1 {
		t.Fatalf("cleanup failed: %+v %v", stored, fake.deleted)
	}
}

func TestConcurrentSyncDoesNotDuplicateRemoteCreates(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	root, err := management.SubmitUserCustomDomain(profile.UserID, "concurrent.example.test")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	fake.onCreate = func(host string) {
		if host == root.Domain {
			close(entered)
			<-release
		}
	}
	done := make(chan error, 1)
	go func() { done <- worker.SyncDueDomains(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not create hostname")
	}
	if err := worker.SyncDueDomains(context.Background()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if fake.creates[root.Domain] != 1 || fake.creates["www."+root.Domain] != 1 {
		t.Fatalf("duplicate creates: %+v", fake.creates)
	}
	_ = db
}

func TestLegacyDomainCannotSkipTechnicalValidation(t *testing.T) {
	db, management, worker, profile, _ := onboardingFixture(t)
	legacy := resellerdomain.Domain{ResellerID: profile.ID, Domain: "www.legacy.example.test", Type: resellerdomain.DomainTypeCustom, Status: resellerdomain.DomainStatusPendingReview}
	if err := db.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := management.ApproveDomain(context.Background(), 9, legacy.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatal("legacy domain bypassed technical validation")
	}
	sweepDomains(t, db, worker)
	sweepDomains(t, db, worker)
	if _, err := management.ApproveDomain(context.Background(), 9, legacy.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOneReadyHostnameAndStaleOrInFlightChecksCannotBeApproved(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	root, err := management.SubmitUserCustomDomain(profile.UserID, "partial.example.test")
	if err != nil {
		t.Fatal(err)
	}
	fake.onCreate = func(host string) { fake.ready = host == root.Domain }
	sweepDomains(t, db, worker)
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatal("one ready hostname opened the pair")
	}
	fake.onCreate = nil
	fake.ready = true
	sweepDomains(t, db, worker)
	old := time.Now().Add(-11 * time.Minute)
	if err := db.Model(&resellerdomain.Domain{}).Where("id = ?", root.ID).Update("last_sync_at", old).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatal("stale validation accepted")
	}
	if _, err := management.RefreshUserDomain(context.Background(), profile.UserID, root.ID); err != nil {
		t.Fatal(err)
	}
	sweepDomains(t, db, worker)
	if err := db.Model(&resellerdomain.Domain{}).Where("id = ?", root.ID).Update("sync_token", "in-flight").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); !errors.Is(err, resellercontract.ErrDomainStatusInvalid) {
		t.Fatal("in-flight validation accepted")
	}
}

func TestProfileDisableCleansUpAndRestoreRecreatesApprovedDomains(t *testing.T) {
	db, management, worker, profile, fake := onboardingFixture(t)
	root, err := management.SubmitUserCustomDomain(profile.UserID, "profile-disabled.example.test")
	if err != nil {
		t.Fatal(err)
	}
	sweepDomains(t, db, worker)
	if _, err := management.ApproveDomain(context.Background(), 9, root.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := management.DisableProfile(9, profile.ID, "disabled"); err != nil {
		t.Fatal(err)
	}
	sweepDomains(t, db, worker)
	stored, _ := gormstore.New(db).GetDomainByID(root.ID)
	if stored.ExternalHostnameID != "" || len(fake.deleted) != 2 {
		t.Fatal("profile disable did not remove remote bindings")
	}
	if _, err := management.RestoreProfile(9, profile.ID); err != nil {
		t.Fatal(err)
	}
	sweepDomains(t, db, worker)
	stored, _ = gormstore.New(db).GetDomainByID(root.ID)
	if stored.Status != resellerdomain.DomainStatusActive || stored.ExternalHostnameID == "" {
		t.Fatal("restored approved domain was not recreated")
	}
}
