package application

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/config"
	"github.com/google/uuid"
)

const (
	// DomainUnavailableNotFound 表示域名未找到或分销站不可用。
	DomainUnavailableNotFound = "not_found"
)

// DomainResolver 将请求 Host 解析为主站或分销站上下文。
type DomainResolver struct {
	repo      resellercontract.DomainLookupRepository
	cfg       config.ResellerConfig
	mainHosts map[string]struct{}
}

// NewDomainResolver 创建域名解析器。
func NewDomainResolver(repo resellercontract.DomainLookupRepository, cfg config.ResellerConfig) *DomainResolver {
	mainHosts := make(map[string]struct{}, len(cfg.MainHosts))
	for _, host := range cfg.MainHosts {
		normalized := resellercontract.NormalizeHost(host)
		if normalized != "" {
			mainHosts[normalized] = struct{}{}
		}
	}
	return &DomainResolver{repo: repo, cfg: cfg, mainHosts: mainHosts}
}

// ResolveRequest 解析 HTTP 请求的租户上下文。
func (r *DomainResolver) ResolveRequest(ctx context.Context, req *http.Request) (resellercontract.TenantContext, error) {
	if r == nil {
		return resellercontract.MainTenantContext(""), nil
	}
	return r.ResolveHost(ctx, resellercontract.ResolveRequestHost(req, r.cfg))
}

// ResolveHost 按原始 Host 解析租户上下文。
func (r *DomainResolver) ResolveHost(ctx context.Context, rawHost string) (resellercontract.TenantContext, error) {
	host := resellercontract.NormalizeHost(rawHost)
	if r == nil || !r.cfg.Enabled {
		return resellercontract.MainTenantContext(host), nil
	}
	if host == "" {
		return resellercontract.MainTenantContext(host), nil
	}
	if _, ok := r.mainHosts[host]; ok {
		return resellercontract.MainTenantContext(host), nil
	}
	var cached cache.ResellerDomainCacheValue
	if hit, err := cache.GetResellerDomain(ctx, host, &cached); err == nil && hit {
		return resellercontract.ResellerTenantContext(host, cached.ResellerID, cached.ResellerUserID, cached.PrimaryDomain), nil
	}
	if hit, err := cache.GetResellerDomainNotFound(ctx, host); err == nil && hit {
		return resellercontract.UnavailableTenantContext(host, DomainUnavailableNotFound), nil
	}
	if r.repo == nil {
		return resellercontract.TenantContext{}, errors.New("reseller domain repository is nil")
	}
	domain, err := r.repo.FindActiveVerifiedDomain(host)
	if err != nil {
		return resellercontract.TenantContext{}, err
	}
	if domain == nil || domain.Profile == nil || domain.Profile.Status != resellerdomain.ProfileStatusActive {
		_ = cache.SetResellerDomainNotFound(ctx, host)
		return resellercontract.UnavailableTenantContext(host, DomainUnavailableNotFound), nil
	}
	primaryDomain := strings.TrimSpace(domain.Domain)
	value := cache.ResellerDomainCacheValue{
		ResellerID:         domain.ResellerID,
		ResellerUserID:     domain.Profile.UserID,
		Domain:             domain.Domain,
		PrimaryDomain:      primaryDomain,
		Status:             domain.Status,
		VerificationStatus: domain.VerificationStatus,
	}
	_ = cache.SetResellerDomain(ctx, host, value)
	return resellercontract.ResellerTenantContext(host, domain.ResellerID, domain.Profile.UserID, primaryDomain), nil
}

type DomainProvisioningService struct {
	store       resellercontract.DomainProvisioningStore
	provider    resellercontract.HostnameProvider
	cnameTarget string
	batchSize   int
	binding     config.ResellerDomainBindingConfig
}

func NewDomainProvisioningService(store resellercontract.DomainProvisioningStore, provider resellercontract.HostnameProvider, binding config.ResellerDomainBindingConfig) *DomainProvisioningService {
	binding = domainBindingPolicy(binding)
	return &DomainProvisioningService{store: store, provider: provider, cnameTarget: binding.CNAMETarget, batchSize: binding.SyncBatchSize, binding: binding}
}

func (s *DomainProvisioningService) SyncDueDomains(ctx context.Context) error {
	rows, err := s.store.ListDomainsForProvisioning(time.Now().Add(-time.Minute), time.Now().Add(-4*time.Hour), s.batchSize)
	if err != nil {
		return err
	}
	var failures []error
	for _, row := range rows {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.syncDomain(ctx, row.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s *DomainProvisioningService) syncDomain(ctx context.Context, id uint) error {
	now, token := time.Now(), uuid.NewString()
	claimed, err := s.store.ClaimDomainProvisioning(id, token, now, now.Add(-10*time.Minute))
	if err != nil || !claimed {
		return err
	}
	preview, err := s.store.GetDomainByID(id)
	if err != nil || preview == nil {
		return err
	}
	var snapshot *resellerdomain.Domain
	inactive := false
	err = s.store.WithinManagementTransaction(func(store resellercontract.ManagementStore) error {
		if preview.RootDomain == "" {
			if err := store.LockDomainCapacity(); err != nil {
				return err
			}
		}
		profile, err := store.GetProfileByIDForUpdate(preview.ResellerID)
		if err != nil {
			return err
		}
		row, err := store.GetDomainByIDForUpdate(id)
		if err != nil {
			return err
		}
		if row == nil || row.SyncToken != token {
			return nil
		}
		if row.Status != resellerdomain.DomainStatusActive && row.VerificationStatus != resellerdomain.DomainVerificationVerified && now.Sub(row.CreatedAt) > time.Duration(s.binding.PendingExpiryHours)*time.Hour {
			row.Status = resellerdomain.DomainStatusDisabled
			row.ProvisioningStatus = "deleting"
			if err := store.UpdateDomain(row); err != nil {
				return err
			}
		}
		inactive = profile == nil || profile.Status != resellerdomain.ProfileStatusActive || row.Status == resellerdomain.DomainStatusDisabled
		if !inactive && row.RootDomain == "" {
			root := strings.TrimPrefix(row.Domain, "www.")
			if !resellercontract.ValidProvisioningHost(root) || !resellercontract.ValidProvisioningHost("www."+root) {
				row.SyncToken = ""
				row.ProvisioningError = "validation_failed"
				return store.UpdateDomain(row)
			}
			additional := 0
			for _, host := range []string{root, "www." + root} {
				member, err := store.FindDomainByHost(host)
				if err != nil {
					return err
				}
				if member != nil && (member.ResellerID != row.ResellerID || member.Type != resellerdomain.DomainTypeCustom || member.ID != row.ID && member.Status == resellerdomain.DomainStatusActive) {
					row.SyncToken = ""
					row.ProvisioningError = "domain_conflict"
					return store.UpdateDomain(row)
				}
				if member == nil {
					additional++
				}
			}
			if err := checkDomainCapacity(store, row.ResellerID, root, additional, s.binding); err != nil {
				return err
			}
			for _, host := range []string{root, "www." + root} {
				member, err := store.FindDomainByHost(host)
				if err != nil {
					return err
				}
				if member == nil {
					_, err = store.UpsertDomain(resellerdomain.Domain{ResellerID: row.ResellerID, Domain: host, RootDomain: root, Type: resellerdomain.DomainTypeCustom, Status: resellerdomain.DomainStatusProvisioning, VerificationStatus: resellerdomain.DomainVerificationPending, ProvisioningStatus: "connecting", DNSRecordsJSON: domainDNSRecords(root, s.cnameTarget)})
				} else if member.ID != row.ID {
					member.RootDomain = root
					member.Status = resellerdomain.DomainStatusProvisioning
					member.VerificationStatus = resellerdomain.DomainVerificationPending
					member.DNSRecordsJSON = domainDNSRecords(root, s.cnameTarget)
					err = store.UpdateDomain(member)
				}
				if err != nil {
					return err
				}
			}
			row.RootDomain = root
			row.Status = resellerdomain.DomainStatusProvisioning
			row.DNSRecordsJSON = domainDNSRecords(root, s.cnameTarget)
			if err := store.UpdateDomain(row); err != nil {
				return err
			}
		}
		snapshot = row
		return nil
	})
	if err != nil || snapshot == nil {
		return err
	}
	var state resellercontract.HostnameState
	var providerErr error
	if inactive {
		providerErr = s.provider.Delete(ctx, snapshot.ExternalHostnameID)
	} else if snapshot.ExternalHostnameID == "" {
		state, providerErr = s.provider.Create(ctx, snapshot.Domain)
	} else {
		state, providerErr = s.provider.Get(ctx, snapshot.ExternalHostnameID)
		var coded interface{ ProvisioningCode() string }
		if errors.As(providerErr, &coded) && coded.ProvisioningCode() == "not_found" {
			state, providerErr = s.provider.Create(ctx, snapshot.Domain)
		}
	}
	if providerErr == nil && !inactive && (state.ID == "" || state.Hostname != snapshot.Domain) {
		providerErr = errors.New("unexpected hostname response")
	}
	return s.saveDomainProgress(ctx, snapshot, token, state, providerErr, inactive)
}

func (s *DomainProvisioningService) saveDomainProgress(ctx context.Context, snapshot *resellerdomain.Domain, token string, state resellercontract.HostnameState, providerErr error, deleting bool) error {
	var changedHosts []string
	err := s.store.WithinManagementTransaction(func(store resellercontract.ManagementStore) error {
		profile, err := store.GetProfileByIDForUpdate(snapshot.ResellerID)
		if err != nil {
			return err
		}
		row, err := store.GetDomainByIDForUpdate(snapshot.ID)
		if err != nil {
			return err
		}
		if row == nil || row.SyncToken != token {
			return nil
		}
		now := time.Now()
		row.LastSyncAt, row.SyncToken = &now, ""
		row.ProvisioningError = ""
		if providerErr != nil {
			row.ProvisioningError = "api_unavailable"
			var coded interface{ ProvisioningCode() string }
			if errors.As(providerErr, &coded) {
				switch code := coded.ProvisioningCode(); code {
				case "api_unavailable", "rate_limited", "access_denied", "validation_failed", "not_found", "quota_exceeded":
					row.ProvisioningError = code
				}
			}
			return store.UpdateDomain(row)
		}
		if deleting {
			row.ExternalHostnameID, row.HostnameStatus, row.SSLStatus = "", "deleted", ""
			if row.Status == resellerdomain.DomainStatusDisabled {
				row.ProvisioningStatus = "disabled"
			} else {
				row.ProvisioningStatus = "profile_inactive"
			}
			return store.UpdateDomain(row)
		}
		row.ExternalHostnameID, row.HostnameStatus, row.SSLStatus = state.ID, state.Status, state.SSL.Status
		// Persist late remote creates for cleanup, without reactivating a disabled shop.
		if row.Status == resellerdomain.DomainStatusDisabled || profile == nil || profile.Status != resellerdomain.ProfileStatusActive {
			row.ProvisioningStatus = "deleting"
			return store.UpdateDomain(row)
		}
		row.ProvisioningStatus = "dns_pending"
		if state.Status == "active" {
			row.ProvisioningStatus = "certificate_pending"
		}
		if hostnameReady(*row) {
			row.ProvisioningStatus = "ready"
			row.VerificationStatus = resellerdomain.DomainVerificationVerified
			row.VerifiedAt = &now
		} else if row.Status != resellerdomain.DomainStatusActive {
			row.VerificationStatus = resellerdomain.DomainVerificationPending
			row.Status = resellerdomain.DomainStatusProvisioning
		}
		if err := store.UpdateDomain(row); err != nil {
			return err
		}
		members, err := store.ListDomainsByResellerID(row.ResellerID)
		if err != nil {
			return err
		}
		pair := domainPair(members, row.RootDomain)
		if len(pair) == 2 && hostnameReady(pair[0]) && hostnameReady(pair[1]) {
			for _, member := range pair {
				if member.Status != resellerdomain.DomainStatusProvisioning {
					continue
				}
				member.Status = resellerdomain.DomainStatusPendingReview
				if err := store.UpdateDomain(&member); err != nil {
					return err
				}
				changedHosts = append(changedHosts, member.Domain)
			}
		}
		return nil
	})
	if err == nil {
		for _, host := range changedHosts {
			_ = cache.DelResellerDomain(ctx, host)
		}
	}
	return err
}
