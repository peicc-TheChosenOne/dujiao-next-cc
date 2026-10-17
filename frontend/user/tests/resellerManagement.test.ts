import test from 'node:test'
import assert from 'node:assert/strict'
import {
  getResellerDomainStatusKey,
  getResellerManagementState,
  getResellerProfileStatusKey,
  groupResellerDomains,
  isResellerProfileActive,
} from '../src/utils/resellerManagement.ts'
import type { ResellerDomainData } from '../src/api/types'

const domain = (id: number, name: string, overrides: Partial<ResellerDomainData> = {}): ResellerDomainData => ({
  id, domain: name, type: 'custom', status: 'active', verification_status: 'verified', is_primary: false,
  created_at: '', updated_at: '', ...overrides,
})

test('domain groups keep root and www together without losing individual states', () => {
  const www = domain(2, 'www.shop.example.com', { root_domain: 'shop.example.com', status: 'provisioning', provisioning_error: 'validation_failed' })
  const root = domain(1, 'shop.example.com', { root_domain: 'shop.example.com', is_primary: true })
  const independent = domain(3, 'other.example.com', { status: 'disabled' })
  const rows = [www, independent, root]
  const groups = groupResellerDomains(rows)
  assert.deepEqual(groups.map(group => [group.rootDomain, group.domains.map(row => row.id)]), [
    ['shop.example.com', [1, 2]], ['other.example.com', [3]],
  ])
  assert.equal(groups[0]?.domains[1]?.provisioning_error, 'validation_failed')
  assert.equal(groups[1]?.domains[0]?.status, 'disabled')
  assert.deepEqual(rows, [www, independent, root])
})

test('legacy www records group with their base domain and missing companions remain visible', () => {
  const rows = [domain(1, 'www.example.com'), domain(2, 'example.com'), domain(3, 'www.solo.example.com')]
  assert.deepEqual(groupResellerDomains(rows).map(group => [group.rootDomain, group.domains.map(row => row.id)]), [
    ['example.com', [2, 1]], ['solo.example.com', [3]],
  ])
  assert.deepEqual(groupResellerDomains([]), [])
})

test('user reseller profile status keys map backend values', () => {
  assert.equal(getResellerProfileStatusKey('pending_review'), 'pendingReview')
  assert.equal(getResellerProfileStatusKey('active'), 'active')
  assert.equal(getResellerProfileStatusKey('rejected'), 'rejected')
  assert.equal(getResellerProfileStatusKey('disabled'), 'disabled')
  assert.equal(getResellerProfileStatusKey('unexpected'), 'unknown')
})

test('user reseller domain status keys map backend values', () => {
  assert.equal(getResellerDomainStatusKey('provisioning'), 'provisioning')
  assert.equal(getResellerDomainStatusKey('pending_review'), 'pendingReview')
  assert.equal(getResellerDomainStatusKey('active'), 'active')
  assert.equal(getResellerDomainStatusKey('disabled'), 'disabled')
  assert.equal(getResellerDomainStatusKey('unexpected'), 'unknown')
})

test('user reseller management state drives onboarding and domain forms', () => {
  assert.deepEqual(getResellerManagementState(null), {
    canApply: false,
    canSubmitDomain: false,
    statusKey: 'unknown',
  })
  assert.deepEqual(getResellerManagementState({ opened: false, can_apply: true }), {
    canApply: true,
    canSubmitDomain: false,
    statusKey: 'notOpened',
  })
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: false,
      profile: { status: 'pending_review' },
    }),
    { canApply: false, canSubmitDomain: false, statusKey: 'pendingReview' },
  )
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: true,
      profile: { status: 'rejected' },
    }),
    { canApply: true, canSubmitDomain: false, statusKey: 'rejected' },
  )
  assert.deepEqual(
    getResellerManagementState({
      opened: true,
      can_apply: false,
      profile: { status: 'active' },
    }),
    { canApply: false, canSubmitDomain: true, statusKey: 'active' },
  )
})

test('active reseller profile check is strict', () => {
  assert.equal(isResellerProfileActive({ status: 'active' }), true)
  assert.equal(isResellerProfileActive({ status: 'pending_review' }), false)
  assert.equal(isResellerProfileActive(null), false)
})
