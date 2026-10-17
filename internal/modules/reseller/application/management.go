package application

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"

	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

type ManagementService struct {
	store              resellercontract.ManagementStore
	cfg                config.ResellerConfig
	domainSyncNotifier func()
}

func (s *ManagementService) SetDomainSyncNotifier(notify func()) { s.domainSyncNotifier = notify }
func (s *ManagementService) notifyDomainSync() {
	if s.domainSyncNotifier != nil {
		s.domainSyncNotifier()
	}
}

type ResellerApplyInput struct {
	Reason string
}

type ResellerApproveInput struct {
	DefaultMarkupPercent decimal.Decimal
	MaxMarkupPercent     decimal.Decimal
}

type ResellerProfileUpdateInput struct {
	DefaultMarkupPercent decimal.Decimal
	MaxMarkupPercent     decimal.Decimal
	SettlementStatus     string
	Reason               string
}

type ResellerSystemDomainInput struct {
	Subdomain string
}

type ResellerApproveResult struct {
	Profile         *resellerdomain.Profile
	SystemDomain    *resellerdomain.Domain
	AlreadyApproved bool
}

func NewManagementService(store resellercontract.ManagementStore, cfg config.ResellerConfig) *ManagementService {
	cfg.DomainBinding = domainBindingPolicy(cfg.DomainBinding)
	return &ManagementService{store: store, cfg: cfg}
}

func (s *ManagementService) DomainBindingInfo() (bool, string) {
	return s.cfg.DomainBinding.Enabled, s.cfg.DomainBinding.CNAMETarget
}

func (s *ManagementService) GetUserManagementSnapshot(userID uint) (*resellerdomain.Profile, []resellerdomain.Domain, bool, error) {
	if s == nil || s.store == nil || userID == 0 {
		return nil, []resellerdomain.Domain{}, false, nil
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, nil, false, err
	}
	if profile == nil {
		return nil, []resellerdomain.Domain{}, s.cfg.Enabled && s.cfg.SelfApplyEnabled, nil
	}
	domains, err := s.store.ListDomainsByResellerID(profile.ID)
	if err != nil {
		return nil, nil, false, err
	}
	canApply := profile.Status == resellerdomain.ProfileStatusRejected && s.cfg.Enabled && s.cfg.SelfApplyEnabled
	return profile, domains, canApply, nil
}

func (s *ManagementService) ApplyUserReseller(userID uint, input ResellerApplyInput) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	if !s.cfg.Enabled || !s.cfg.SelfApplyEnabled {
		return nil, resellercontract.ErrApplyDisabled
	}
	reason := strings.TrimSpace(input.Reason)
	existing, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		profile := &resellerdomain.Profile{
			UserID:           userID,
			Status:           resellerdomain.ProfileStatusPendingReview,
			ApplyReason:      reason,
			SettlementStatus: resellerdomain.SettlementStatusNormal,
		}
		if err := s.store.CreateProfile(profile); err != nil {
			return nil, err
		}
		return s.store.GetProfileByID(profile.ID)
	}
	switch existing.Status {
	case resellerdomain.ProfileStatusRejected:
		existing.Status = resellerdomain.ProfileStatusPendingReview
		existing.ApplyReason = reason
		existing.RejectReason = ""
		existing.ReviewedBy = nil
		existing.ReviewedAt = nil
		if err := s.store.UpdateProfile(existing); err != nil {
			return nil, err
		}
		return s.store.GetProfileByID(existing.ID)
	case resellerdomain.ProfileStatusPendingReview, resellerdomain.ProfileStatusActive:
		return existing, nil
	case resellerdomain.ProfileStatusDisabled:
		return nil, resellercontract.ErrProfileInactive
	default:
		return nil, resellercontract.ErrProfileStatusInvalid
	}
}

func (s *ManagementService) ApproveProfile(ctx context.Context, adminID, profileID uint, input ResellerApproveInput) (*ResellerApproveResult, error) {
	if s == nil || s.store == nil || adminID == 0 || profileID == 0 {
		return nil, productcontract.ErrNotFound
	}
	if err := validateResellerProfileMarkup(input.DefaultMarkupPercent, input.MaxMarkupPercent); err != nil {
		return nil, err
	}
	var result *ResellerApproveResult
	err := s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		profile, err := repoTx.GetProfileByIDForUpdate(profileID)
		if err != nil {
			return err
		}
		if profile == nil {
			return productcontract.ErrNotFound
		}
		domains, err := repoTx.ListDomainsByResellerID(profile.ID)
		if err != nil {
			return err
		}
		var systemDomain *resellerdomain.Domain
		for i := range domains {
			if domains[i].Type == resellerdomain.DomainTypeSubdomain {
				systemDomain = &domains[i]
				break
			}
		}
		if profile.Status == resellerdomain.ProfileStatusActive {
			result = &ResellerApproveResult{Profile: profile, SystemDomain: systemDomain, AlreadyApproved: true}
			return nil
		}
		if profile.Status != resellerdomain.ProfileStatusPendingReview && profile.Status != resellerdomain.ProfileStatusRejected {
			return resellercontract.ErrProfileStatusInvalid
		}
		now := time.Now()
		profile.Status = resellerdomain.ProfileStatusActive
		profile.RejectReason = ""
		profile.DefaultMarkupPercent = money.FromDecimal(input.DefaultMarkupPercent)
		profile.MaxMarkupPercent = money.FromDecimal(input.MaxMarkupPercent)
		profile.SettlementStatus = resellerdomain.SettlementStatusNormal
		profile.ReviewedBy = &adminID
		profile.ReviewedAt = &now
		if err := repoTx.UpdateProfile(profile); err != nil {
			return err
		}
		if base := resellercontract.NormalizeHost(s.cfg.SubdomainBase); base != "" {
			nextDomain := ""
			if systemDomain != nil {
				nextDomain = systemDomain.Domain
			} else {
				nextDomain, err = normalizeAndValidateSystemSubdomain(fmt.Sprintf("r%d", profile.ID), base, s.cfg)
				if err != nil {
					return err
				}
			}
			if systemDomain == nil || systemDomain.Status != resellerdomain.DomainStatusActive || systemDomain.VerificationStatus != resellerdomain.DomainVerificationVerified {
				systemDomain, _, err = assignSystemSubdomain(repoTx, profile.ID, nextDomain, domains)
				if err != nil {
					return err
				}
			}
		}
		result = &ResellerApproveResult{Profile: profile, SystemDomain: systemDomain}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result != nil && result.SystemDomain != nil {
		_ = cache.DelResellerDomain(ctx, result.SystemDomain.Domain)
	}
	return result, nil
}

func (s *ManagementService) AssignSystemSubdomain(ctx context.Context, adminID, profileID uint, input ResellerSystemDomainInput) (*resellerdomain.Domain, error) {
	if s == nil || s.store == nil || adminID == 0 || profileID == 0 {
		return nil, productcontract.ErrNotFound
	}
	base := resellercontract.NormalizeHost(s.cfg.SubdomainBase)
	if base == "" {
		return nil, resellercontract.ErrSubdomainBaseMissing
	}
	nextDomain, err := normalizeAndValidateSystemSubdomain(input.Subdomain, base, s.cfg)
	if err != nil {
		return nil, err
	}
	var updated *resellerdomain.Domain
	var previousHost string
	err = s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		profile, err := repoTx.GetProfileByIDForUpdate(profileID)
		if err != nil {
			return err
		}
		if profile == nil {
			return productcontract.ErrNotFound
		}
		domains, err := repoTx.ListDomainsByResellerID(profile.ID)
		if err != nil {
			return err
		}
		updated, previousHost, err = assignSystemSubdomain(repoTx, profile.ID, nextDomain, domains)
		return err
	})
	if err != nil {
		return nil, err
	}
	for _, domain := range []string{previousHost, updated.Domain} {
		if domain != "" {
			_ = cache.DelResellerDomain(ctx, domain)
		}
	}
	return s.store.GetDomainByID(updated.ID)
}

func assignSystemSubdomain(repoTx resellercontract.ManagementStore, profileID uint, nextDomain string, domains []resellerdomain.Domain) (*resellerdomain.Domain, string, error) {
	existingHost, err := repoTx.FindDomainByHost(nextDomain)
	if err != nil {
		return nil, "", err
	}
	var systemDomain *resellerdomain.Domain
	hasPrimary := false
	for i := range domains {
		if domains[i].IsPrimary {
			hasPrimary = true
		}
		if domains[i].Type == resellerdomain.DomainTypeSubdomain && systemDomain == nil {
			systemDomain = &domains[i]
		}
	}
	if existingHost != nil && (existingHost.ResellerID != profileID || (systemDomain != nil && existingHost.ID != systemDomain.ID)) {
		return nil, "", resellercontract.ErrDomainConflict
	}
	now := time.Now()
	shouldBePrimary := !hasPrimary || (systemDomain != nil && systemDomain.IsPrimary)
	previousHost := ""
	if systemDomain == nil {
		systemDomain, err = repoTx.UpsertDomain(resellerdomain.Domain{
			ResellerID:         profileID,
			Domain:             nextDomain,
			Type:               resellerdomain.DomainTypeSubdomain,
			VerificationStatus: resellerdomain.DomainVerificationVerified,
			Status:             resellerdomain.DomainStatusActive,
			IsPrimary:          shouldBePrimary,
			VerifiedAt:         &now,
		})
	} else {
		previousHost = systemDomain.Domain
		systemDomain.Domain = nextDomain
		systemDomain.VerificationToken = ""
		systemDomain.VerificationStatus = resellerdomain.DomainVerificationVerified
		systemDomain.Status = resellerdomain.DomainStatusActive
		systemDomain.IsPrimary = shouldBePrimary
		systemDomain.VerifiedAt = &now
		err = repoTx.UpdateDomain(systemDomain)
	}
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "already exists") || strings.Contains(message, "unique") || strings.Contains(message, "duplicate") {
			err = resellercontract.ErrDomainConflict
		}
		return nil, "", err
	}
	return systemDomain, previousHost, nil
}

func (s *ManagementService) RejectProfile(adminID, profileID uint, reason string) (*resellerdomain.Profile, error) {
	return s.updateProfileReviewStatus(adminID, profileID, resellerdomain.ProfileStatusRejected, strings.TrimSpace(reason))
}

func (s *ManagementService) DisableProfile(adminID, profileID uint, reason string) (*resellerdomain.Profile, error) {
	return s.updateProfileReviewStatus(adminID, profileID, resellerdomain.ProfileStatusDisabled, strings.TrimSpace(reason))
}

func (s *ManagementService) RestoreProfile(adminID, profileID uint) (*resellerdomain.Profile, error) {
	return s.updateProfileReviewStatus(adminID, profileID, resellerdomain.ProfileStatusActive, "")
}

func (s *ManagementService) UpdateProfileOperationalConfig(adminID, profileID uint, input ResellerProfileUpdateInput) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || adminID == 0 || profileID == 0 {
		return nil, productcontract.ErrNotFound
	}
	if err := validateResellerProfileMarkup(input.DefaultMarkupPercent, input.MaxMarkupPercent); err != nil {
		return nil, err
	}
	settlementStatus := strings.TrimSpace(input.SettlementStatus)
	if settlementStatus != "" && settlementStatus != resellerdomain.SettlementStatusNormal && settlementStatus != resellerdomain.SettlementStatusFrozen {
		return nil, resellercontract.ErrProfileStatusInvalid
	}
	var updated *resellerdomain.Profile
	err := s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		profile, err := repoTx.GetProfileByID(profileID)
		if err != nil {
			return err
		}
		if profile == nil {
			return productcontract.ErrNotFound
		}
		now := time.Now()
		profile.DefaultMarkupPercent = money.FromDecimal(input.DefaultMarkupPercent)
		profile.MaxMarkupPercent = money.FromDecimal(input.MaxMarkupPercent)
		if settlementStatus != "" {
			profile.SettlementStatus = settlementStatus
		}
		profile.ReviewedBy = &adminID
		profile.ReviewedAt = &now
		if err := repoTx.UpdateProfile(profile); err != nil {
			return err
		}
		updated = profile
		return nil
	})
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, productcontract.ErrNotFound
	}
	return s.store.GetProfileByID(profileID)
}

func (s *ManagementService) SubmitUserCustomDomain(userID uint, rawDomain string) (*resellerdomain.Domain, error) {
	if s == nil || s.store == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	domain, err := normalizeAndValidateCustomDomain(rawDomain, s.cfg)
	if err != nil {
		return nil, err
	}
	if s.cfg.DomainBinding.Enabled {
		return s.submitDomainPair(profile, strings.TrimPrefix(domain, "www."))
	}
	// 未配置自动接入时保留人工审核流程。
	row, err := s.store.UpsertDomain(resellerdomain.Domain{
		ResellerID:         profile.ID,
		Domain:             domain,
		Type:               resellerdomain.DomainTypeCustom,
		VerificationStatus: resellerdomain.DomainVerificationPending,
		Status:             resellerdomain.DomainStatusPendingReview,
		IsPrimary:          false,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") {
			return nil, resellercontract.ErrDomainConflict
		}
		return nil, err
	}
	return row, nil
}

func (s *ManagementService) ApproveDomain(ctx context.Context, adminID, domainID uint) (*resellerdomain.Domain, error) {
	return s.updateDomainStatus(ctx, adminID, domainID, resellerdomain.DomainStatusActive)
}

func (s *ManagementService) DisableDomain(ctx context.Context, adminID, domainID uint) (*resellerdomain.Domain, error) {
	return s.updateDomainStatus(ctx, adminID, domainID, resellerdomain.DomainStatusDisabled)
}

func (s *ManagementService) SetPrimaryDomain(ctx context.Context, adminID, domainID uint) (*resellerdomain.Domain, error) {
	if s == nil || s.store == nil || adminID == 0 || domainID == 0 {
		return nil, productcontract.ErrNotFound
	}
	cacheDomains := make([]string, 0, 4)
	err := s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		target, err := repoTx.GetDomainByIDForUpdate(domainID)
		if err != nil {
			return err
		}
		if target == nil {
			return productcontract.ErrNotFound
		}
		if target.Status != resellerdomain.DomainStatusActive || target.VerificationStatus != resellerdomain.DomainVerificationVerified {
			return resellercontract.ErrDomainStatusInvalid
		}
		domains, err := repoTx.ListDomainsByResellerID(target.ResellerID)
		if err != nil {
			return err
		}
		for i := range domains {
			domain := domains[i]
			nextPrimary := domain.ID == target.ID
			if domain.IsPrimary == nextPrimary {
				if domain.Domain != "" {
					cacheDomains = append(cacheDomains, domain.Domain)
				}
				continue
			}
			domain.IsPrimary = nextPrimary
			if err := repoTx.UpdateDomain(&domain); err != nil {
				return err
			}
			if domain.Domain != "" {
				cacheDomains = append(cacheDomains, domain.Domain)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, domain := range cacheDomains {
		_ = cache.DelResellerDomain(ctx, domain)
	}
	return s.store.GetDomainByID(domainID)
}

func (s *ManagementService) updateProfileReviewStatus(adminID, profileID uint, targetStatus, reason string) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || adminID == 0 || profileID == 0 {
		return nil, productcontract.ErrNotFound
	}
	cacheDomains := make([]string, 0, 4)
	err := s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		profile, err := repoTx.GetProfileByIDForUpdate(profileID)
		if err != nil {
			return err
		}
		if profile == nil {
			return productcontract.ErrNotFound
		}
		switch targetStatus {
		case resellerdomain.ProfileStatusRejected:
			if profile.Status != resellerdomain.ProfileStatusPendingReview {
				return resellercontract.ErrProfileStatusInvalid
			}
		case resellerdomain.ProfileStatusDisabled:
			if profile.Status != resellerdomain.ProfileStatusPendingReview && profile.Status != resellerdomain.ProfileStatusActive && profile.Status != resellerdomain.ProfileStatusRejected {
				return resellercontract.ErrProfileStatusInvalid
			}
		case resellerdomain.ProfileStatusActive:
			if profile.Status != resellerdomain.ProfileStatusDisabled {
				return resellercontract.ErrProfileStatusInvalid
			}
		default:
			return resellercontract.ErrProfileStatusInvalid
		}
		domains, err := repoTx.ListDomainsByResellerID(profile.ID)
		if err != nil {
			return err
		}
		for i := range domains {
			if domains[i].Domain != "" {
				cacheDomains = append(cacheDomains, domains[i].Domain)
			}
			if domains[i].RootDomain != "" && domains[i].SyncToken == "" {
				domains[i].LastSyncAt = nil
				if targetStatus == resellerdomain.ProfileStatusDisabled {
					domains[i].ProvisioningStatus = "deleting"
				}
				if err := repoTx.UpdateDomain(&domains[i]); err != nil {
					return err
				}
			}
		}
		now := time.Now()
		profile.Status = targetStatus
		if targetStatus == resellerdomain.ProfileStatusRejected || targetStatus == resellerdomain.ProfileStatusDisabled {
			profile.RejectReason = strings.TrimSpace(reason)
		} else {
			profile.RejectReason = ""
		}
		profile.ReviewedBy = &adminID
		profile.ReviewedAt = &now
		return repoTx.UpdateProfile(profile)
	})
	if err != nil {
		return nil, err
	}
	for _, domain := range cacheDomains {
		_ = cache.DelResellerDomain(context.Background(), domain)
	}
	s.notifyDomainSync()
	return s.store.GetProfileByID(profileID)
}

func (s *ManagementService) updateDomainStatus(ctx context.Context, adminID, domainID uint, targetStatus string) (*resellerdomain.Domain, error) {
	if s == nil || s.store == nil || adminID == 0 || domainID == 0 {
		return nil, productcontract.ErrNotFound
	}
	preview, err := s.store.GetDomainByID(domainID)
	if err != nil {
		return nil, err
	}
	if preview != nil && preview.RootDomain != "" {
		return s.updateDomainPairStatus(ctx, preview, targetStatus)
	}
	if s.cfg.DomainBinding.Enabled && targetStatus == resellerdomain.DomainStatusActive && preview != nil && preview.Type == resellerdomain.DomainTypeCustom {
		return nil, resellercontract.ErrDomainStatusInvalid
	}
	cacheDomains := make([]string, 0, 4)
	err = s.store.WithinManagementTransaction(func(repoTx resellercontract.ManagementStore) error {
		domain, err := repoTx.GetDomainByIDForUpdate(domainID)
		if err != nil {
			return err
		}
		if domain == nil {
			return productcontract.ErrNotFound
		}
		domains, err := repoTx.ListDomainsByResellerID(domain.ResellerID)
		if err != nil {
			return err
		}
		for i := range domains {
			if domains[i].Domain != "" {
				cacheDomains = append(cacheDomains, domains[i].Domain)
			}
		}
		switch targetStatus {
		case resellerdomain.DomainStatusActive:
			if domain.Status != resellerdomain.DomainStatusPendingReview && domain.Status != resellerdomain.DomainStatusDisabled {
				return resellercontract.ErrDomainStatusInvalid
			}
			now := time.Now()
			domain.Status = resellerdomain.DomainStatusActive
			domain.VerificationStatus = resellerdomain.DomainVerificationVerified
			domain.VerifiedAt = &now
			if !hasActiveVerifiedPrimary(domains, domain.ID) {
				domain.IsPrimary = true
			}
		case resellerdomain.DomainStatusDisabled:
			if domain.Status != resellerdomain.DomainStatusPendingReview && domain.Status != resellerdomain.DomainStatusActive {
				return resellercontract.ErrDomainStatusInvalid
			}
			domain.Status = resellerdomain.DomainStatusDisabled
			wasPrimary := domain.IsPrimary
			domain.IsPrimary = false
			if wasPrimary {
				for i := range domains {
					candidate := domains[i]
					if candidate.ID == domain.ID || candidate.Status != resellerdomain.DomainStatusActive || candidate.VerificationStatus != resellerdomain.DomainVerificationVerified {
						continue
					}
					if !candidate.IsPrimary {
						candidate.IsPrimary = true
						if err := repoTx.UpdateDomain(&candidate); err != nil {
							return err
						}
					}
					break
				}
			}
		default:
			return resellercontract.ErrDomainStatusInvalid
		}
		if err := repoTx.UpdateDomain(domain); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, domain := range cacheDomains {
		_ = cache.DelResellerDomain(ctx, domain)
	}
	return s.store.GetDomainByID(domainID)
}

func validateResellerProfileMarkup(defaultMarkup, maxMarkup decimal.Decimal) error {
	if defaultMarkup.LessThan(decimal.Zero) || maxMarkup.LessThan(decimal.Zero) {
		return resellercontract.ErrProfileStatusInvalid
	}
	if maxMarkup.GreaterThan(decimal.Zero) && defaultMarkup.GreaterThan(maxMarkup) {
		return resellercontract.ErrProfileStatusInvalid
	}
	return nil
}

func hasActiveVerifiedPrimary(domains []resellerdomain.Domain, excludeID uint) bool {
	for i := range domains {
		domain := domains[i]
		if domain.ID == excludeID {
			continue
		}
		if domain.IsPrimary && domain.Status == resellerdomain.DomainStatusActive && domain.VerificationStatus == resellerdomain.DomainVerificationVerified {
			return true
		}
	}
	return false
}

func normalizeAndValidateCustomDomain(raw string, cfg config.ResellerConfig) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", resellercontract.ErrDomainInvalid
	}
	if strings.Contains(trimmed, "://") || strings.ContainsAny(trimmed, "/?#") {
		return "", resellercontract.ErrDomainInvalid
	}
	domain := resellercontract.NormalizeHost(trimmed)
	if cfg.DomainBinding.Enabled && (strings.Contains(trimmed, ":") || !resellercontract.ValidProvisioningHost(domain)) {
		return "", resellercontract.ErrDomainInvalid
	}
	if domain == "" {
		return "", resellercontract.ErrDomainInvalid
	}
	for _, mainHost := range cfg.MainHosts {
		if domain == resellercontract.NormalizeHost(mainHost) {
			return "", resellercontract.ErrDomainMainHostNotAllowed
		}
	}
	base := resellercontract.NormalizeHost(cfg.SubdomainBase)
	if base != "" && (domain == base || strings.HasSuffix(domain, "."+base)) {
		return "", resellercontract.ErrDomainMainHostNotAllowed
	}
	return domain, nil
}

func normalizeAndValidateSystemSubdomain(raw string, base string, cfg config.ResellerConfig) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", resellercontract.ErrDomainInvalid
	}
	if strings.Contains(trimmed, "://") || strings.Contains(trimmed, ":") || strings.ContainsAny(trimmed, "/?#") || strings.ContainsAny(trimmed, " \t\r\n") {
		return "", resellercontract.ErrDomainInvalid
	}
	base = resellercontract.NormalizeHost(base)
	if base == "" {
		return "", resellercontract.ErrSubdomainBaseMissing
	}
	normalized := resellercontract.NormalizeHost(trimmed)
	if normalized == "" {
		return "", resellercontract.ErrDomainInvalid
	}
	domain := normalized
	if !strings.Contains(normalized, ".") {
		if !isValidResellerSubdomainLabel(normalized) {
			return "", resellercontract.ErrDomainInvalid
		}
		domain = normalized + "." + base
	}
	if domain == base || !strings.HasSuffix(domain, "."+base) {
		return "", resellercontract.ErrDomainInvalid
	}
	label := strings.TrimSuffix(domain, "."+base)
	if strings.Contains(label, ".") || !isValidResellerSubdomainLabel(label) {
		return "", resellercontract.ErrDomainInvalid
	}
	for _, mainHost := range cfg.MainHosts {
		if domain == resellercontract.NormalizeHost(mainHost) {
			return "", resellercontract.ErrDomainMainHostNotAllowed
		}
	}
	return domain, nil
}

func isValidResellerSubdomainLabel(label string) bool {
	if label == "" || len(label) > 63 {
		return false
	}
	if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, ch := range label {
		switch {
		case ch >= 'a' && ch <= 'z':
		case ch >= '0' && ch <= '9':
		case ch == '-':
		default:
			return false
		}
	}
	return true
}

func domainDNSRecords(root, target string) jsonmap.JSON {
	encoded, _ := json.Marshal([]resellercontract.DomainDNSRecord{{Type: "CNAME", Name: root, Value: target}, {Type: "CNAME", Name: "www." + root, Value: target}})
	return jsonmap.JSON{"records": json.RawMessage(encoded)}
}

func (s *ManagementService) submitDomainPair(profile *resellerdomain.Profile, root string) (*resellerdomain.Domain, error) {
	for _, host := range []string{root, "www." + root} {
		if _, err := normalizeAndValidateCustomDomain(host, s.cfg); err != nil {
			return nil, err
		}
	}
	var submitted *resellerdomain.Domain
	records := domainDNSRecords(root, s.cfg.DomainBinding.CNAMETarget)
	err := s.store.WithinManagementTransaction(func(store resellercontract.ManagementStore) error {
		if err := store.LockDomainCapacity(); err != nil {
			return err
		}
		locked, err := store.GetProfileByIDForUpdate(profile.ID)
		if err != nil {
			return err
		}
		if locked == nil || locked.Status != resellerdomain.ProfileStatusActive {
			return resellercontract.ErrProfileInactive
		}
		if err := checkDomainCapacity(store, profile.ID, root, 2, s.cfg.DomainBinding); err != nil {
			return err
		}
		for _, host := range []string{root, "www." + root} {
			row, err := store.UpsertDomain(resellerdomain.Domain{ResellerID: profile.ID, Domain: host, RootDomain: root, Type: resellerdomain.DomainTypeCustom, Status: resellerdomain.DomainStatusProvisioning, VerificationStatus: resellerdomain.DomainVerificationPending, ProvisioningStatus: "connecting", DNSRecordsJSON: records})
			if err != nil {
				return err
			}
			if host == root {
				submitted = row
			}
		}
		return nil
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "already exists") || strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return nil, resellercontract.ErrDomainConflict
		}
		return nil, err
	}
	s.notifyDomainSync()
	return submitted, nil
}

func domainPair(rows []resellerdomain.Domain, root string) []resellerdomain.Domain {
	pair := make([]resellerdomain.Domain, 0, 2)
	for _, row := range rows {
		if row.RootDomain == root && row.Type == resellerdomain.DomainTypeCustom {
			pair = append(pair, row)
		}
	}
	return pair
}

func hostnameReady(row resellerdomain.Domain) bool {
	return row.ExternalHostnameID != "" && row.HostnameStatus == "active" && row.SSLStatus == "active" && row.ProvisioningError == ""
}

func (s *ManagementService) updateDomainPairStatus(ctx context.Context, target *resellerdomain.Domain, status string) (*resellerdomain.Domain, error) {
	var hosts []string
	err := s.store.WithinManagementTransaction(func(store resellercontract.ManagementStore) error {
		profile, err := store.GetProfileByIDForUpdate(target.ResellerID)
		if err != nil {
			return err
		}
		if profile == nil {
			return productcontract.ErrNotFound
		}
		rows, err := store.ListDomainsByResellerID(target.ResellerID)
		if err != nil {
			return err
		}
		pair := domainPair(rows, target.RootDomain)
		if len(pair) != 2 {
			return resellercontract.ErrDomainStatusInvalid
		}
		if status == resellerdomain.DomainStatusActive {
			if profile.Status != resellerdomain.ProfileStatusActive {
				return resellercontract.ErrProfileInactive
			}
			for _, row := range pair {
				if row.Status != resellerdomain.DomainStatusPendingReview || row.VerificationStatus != resellerdomain.DomainVerificationVerified || !hostnameReady(row) || row.SyncToken != "" || row.LastSyncAt == nil || row.LastSyncAt.Before(time.Now().Add(-10*time.Minute)) {
					return resellercontract.ErrDomainStatusInvalid
				}
			}
		}
		hasPrimary := hasActiveVerifiedPrimary(rows, 0)
		lostPrimary := false
		for i := range pair {
			row := pair[i]
			now := time.Now()
			if status == resellerdomain.DomainStatusActive {
				row.Status = status
				row.VerifiedAt = &now
				row.IsPrimary = !hasPrimary && row.Domain == row.RootDomain
			} else if status == resellerdomain.DomainStatusDisabled {
				lostPrimary = lostPrimary || row.IsPrimary
				row.Status = status
				row.IsPrimary = false
				row.ProvisioningStatus = "deleting"
				if row.SyncToken == "" {
					row.LastSyncAt = nil
				}
			} else {
				return resellercontract.ErrDomainStatusInvalid
			}
			if err := store.UpdateDomain(&row); err != nil {
				return err
			}
			hosts = append(hosts, row.Domain)
		}
		if lostPrimary {
			for _, row := range rows {
				if row.RootDomain != target.RootDomain && row.Status == resellerdomain.DomainStatusActive && row.VerificationStatus == resellerdomain.DomainVerificationVerified {
					row.IsPrimary = true
					if err := store.UpdateDomain(&row); err != nil {
						return err
					}
					hosts = append(hosts, row.Domain)
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, host := range hosts {
		_ = cache.DelResellerDomain(ctx, host)
	}
	s.notifyDomainSync()
	return s.store.GetDomainByID(target.ID)
}

func (s *ManagementService) RefreshDomain(ctx context.Context, adminID, domainID uint) (*resellerdomain.Domain, error) {
	if adminID == 0 {
		return nil, productcontract.ErrNotFound
	}
	return s.refreshDomain(ctx, 0, domainID)
}

func (s *ManagementService) RefreshUserDomain(ctx context.Context, userID, domainID uint) (*resellerdomain.Domain, error) {
	if userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	return s.refreshDomain(ctx, userID, domainID)
}

func (s *ManagementService) refreshDomain(ctx context.Context, userID, id uint) (*resellerdomain.Domain, error) {
	if s == nil || s.store == nil || id == 0 {
		return nil, productcontract.ErrNotFound
	}
	if !s.cfg.DomainBinding.Enabled {
		return nil, resellercontract.ErrApplyDisabled
	}
	target, err := s.store.GetDomainByID(id)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Type != resellerdomain.DomainTypeCustom {
		return nil, productcontract.ErrNotFound
	}
	err = s.store.WithinManagementTransaction(func(store resellercontract.ManagementStore) error {
		profile, err := store.GetProfileByIDForUpdate(target.ResellerID)
		if err != nil {
			return err
		}
		if profile == nil || userID != 0 && profile.UserID != userID {
			return productcontract.ErrNotFound
		}
		if profile.Status != resellerdomain.ProfileStatusActive {
			return resellercontract.ErrProfileInactive
		}
		row, err := store.GetDomainByIDForUpdate(id)
		if err != nil {
			return err
		}
		if row == nil {
			return productcontract.ErrNotFound
		}
		if row.Status == resellerdomain.DomainStatusDisabled && (userID != 0 || row.ProvisioningStatus == "deleting") {
			return resellercontract.ErrDomainStatusInvalid
		}
		rows, err := store.ListDomainsByResellerID(target.ResellerID)
		if err != nil {
			return err
		}
		for _, candidate := range domainPair(rows, row.RootDomain) {
			if row.RootDomain != "" && candidate.Status == resellerdomain.DomainStatusDisabled && candidate.ProvisioningStatus == "deleting" {
				return resellercontract.ErrDomainStatusInvalid
			}
		}
		for _, candidate := range rows {
			if candidate.ID != row.ID && (row.RootDomain == "" || candidate.RootDomain != row.RootDomain) {
				continue
			}
			if candidate.SyncToken != "" {
				continue
			}
			candidate.LastSyncAt = nil
			if candidate.Status == resellerdomain.DomainStatusDisabled || candidate.RootDomain == "" {
				candidate.Status = resellerdomain.DomainStatusProvisioning
				candidate.VerificationStatus = resellerdomain.DomainVerificationPending
				candidate.ProvisioningStatus = "connecting"
			}
			if err := store.UpdateDomain(&candidate); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.notifyDomainSync()
	return s.store.GetDomainByID(id)
}

func domainBindingPolicy(binding config.ResellerDomainBindingConfig) config.ResellerDomainBindingConfig {
	if binding.MaxCustomDomainsPerReseller == 0 {
		binding.MaxCustomDomainsPerReseller = 5
	}
	if binding.MaxHostnames == 0 {
		binding.MaxHostnames = 100
	}
	if binding.PendingExpiryHours == 0 {
		binding.PendingExpiryHours = 168
	}
	if binding.SyncBatchSize == 0 {
		binding.SyncBatchSize = 50
	}
	return binding
}

// 调用方必须先占用全局额度锁，额度检查与域名保留在同一事务中完成。
func checkDomainCapacity(store resellercontract.ManagementStore, resellerID uint, root string, additional int, binding config.ResellerDomainBindingConfig) error {
	rows, err := store.ListDomainsByResellerID(resellerID)
	if err != nil {
		return err
	}
	roots := make(map[string]bool)
	for _, row := range rows {
		if row.Type != resellerdomain.DomainTypeCustom || row.Status == resellerdomain.DomainStatusDisabled && row.ExternalHostnameID == "" {
			continue
		}
		name := row.RootDomain
		if name == "" {
			name = strings.TrimPrefix(row.Domain, "www.")
		}
		roots[name] = true
	}
	if !roots[root] && len(roots) >= binding.MaxCustomDomainsPerReseller {
		return resellercontract.ErrDomainLimit
	}
	count, err := store.CountReservedCustomHostnames()
	if err != nil {
		return err
	}
	if count+int64(additional) > int64(binding.MaxHostnames) {
		return resellercontract.ErrDomainLimit
	}
	return nil
}
