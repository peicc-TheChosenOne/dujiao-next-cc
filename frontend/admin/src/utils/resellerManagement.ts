const RESELLER_PROFILE_STATUS_PENDING_REVIEW = 'pending_review'
const RESELLER_PROFILE_STATUS_ACTIVE = 'active'
const RESELLER_PROFILE_STATUS_REJECTED = 'rejected'
const RESELLER_PROFILE_STATUS_DISABLED = 'disabled'

const RESELLER_DOMAIN_STATUS_PENDING_REVIEW = 'pending_review'
const RESELLER_DOMAIN_STATUS_ACTIVE = 'active'
const RESELLER_DOMAIN_STATUS_DISABLED = 'disabled'

export const getResellerProfileStatusKey = (status?: string) => {
  if (status === RESELLER_PROFILE_STATUS_PENDING_REVIEW) return 'pendingReview'
  if (status === RESELLER_PROFILE_STATUS_ACTIVE) return 'active'
  if (status === RESELLER_PROFILE_STATUS_REJECTED) return 'rejected'
  if (status === RESELLER_PROFILE_STATUS_DISABLED) return 'disabled'
  return 'unknown'
}

export const getResellerProfileActionState = (status?: string) => ({
  canApprove: status === RESELLER_PROFILE_STATUS_PENDING_REVIEW || status === RESELLER_PROFILE_STATUS_REJECTED,
  canReject: status === RESELLER_PROFILE_STATUS_PENDING_REVIEW,
  canDisable:
    status === RESELLER_PROFILE_STATUS_PENDING_REVIEW ||
    status === RESELLER_PROFILE_STATUS_ACTIVE ||
    status === RESELLER_PROFILE_STATUS_REJECTED,
  canRestore: status === RESELLER_PROFILE_STATUS_DISABLED,
})

export const getResellerDomainActionState = (status?: string, domain?: { root_domain?: string; hostname_status?: string; ssl_status?: string; provisioning_error?: string; verification_status?: string }) => ({
  canApprove: domain?.root_domain
    ? status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW && domain.hostname_status === 'active' && domain.ssl_status === 'active' && domain.verification_status === 'verified' && !domain.provisioning_error
    : status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW || status === RESELLER_DOMAIN_STATUS_DISABLED,
  canDisable: status === 'provisioning' || status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW || status === RESELLER_DOMAIN_STATUS_ACTIVE,
})

export interface ResellerApprovalResult {
  id: number
  status: 'approved' | 'alreadyApproved' | 'failed'
  domain?: string
  message?: string
}

export const approveResellerProfiles = async (
  ids: number[],
  approve: (id: number) => Promise<{ alreadyApproved?: boolean; domain?: string }>,
  onProgress: (results: ResellerApprovalResult[]) => void,
): Promise<ResellerApprovalResult[]> => {
  const results: ResellerApprovalResult[] = []
  for (const id of new Set(ids)) {
    try {
      const result = await approve(id)
      results.push({ id, status: result.alreadyApproved ? 'alreadyApproved' : 'approved', domain: result.domain })
    } catch (error) {
      results.push({ id, status: 'failed', message: error instanceof Error ? error.message : '' })
    }
    onProgress([...results])
  }
  return results
}
