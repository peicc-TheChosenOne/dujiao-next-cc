import test from 'node:test'
import assert from 'node:assert/strict'
import {
  getResellerDomainActionState,
  getResellerProfileActionState,
  getResellerProfileStatusKey,
  approveResellerProfiles,
} from '../src/utils/resellerManagement.ts'

test('admin reseller profile status keys map backend values', () => {
  assert.equal(getResellerProfileStatusKey('pending_review'), 'pendingReview')
  assert.equal(getResellerProfileStatusKey('active'), 'active')
  assert.equal(getResellerProfileStatusKey('rejected'), 'rejected')
  assert.equal(getResellerProfileStatusKey('disabled'), 'disabled')
  assert.equal(getResellerProfileStatusKey('other'), 'unknown')
})

test('batch approval continues after failure, reports progress, and skips duplicate IDs', async () => {
  const calls: number[] = []
  const progress: number[] = []
  let active = 0
  const results = await approveResellerProfiles([1, 2, 3, 1], async (id) => {
    assert.equal(active++, 0, 'approvals must run sequentially')
    await Promise.resolve()
    active--
    calls.push(id)
    if (id === 2) throw new Error('Domain already assigned')
    return { alreadyApproved: id === 3, domain: `r${id}.example.test` }
  }, (results) => progress.push(results.length))
  assert.deepEqual(calls, [1, 2, 3])
  assert.deepEqual(progress, [1, 2, 3])
  assert.deepEqual(results.map((result) => result.status), ['approved', 'failed', 'alreadyApproved'])
  assert.equal(results[1]?.message, 'Domain already assigned')
  const retried = await approveResellerProfiles(results.filter((result) => result.status === 'failed').map((result) => result.id), async (id) => ({ domain: `r${id}.example.test` }), () => {})
  assert.deepEqual(retried.map((result) => result.id), [2])
  assert.equal(retried[0]?.status, 'approved')
})

test('admin reseller profile actions follow review state', () => {
  assert.deepEqual(getResellerProfileActionState('pending_review'), { canApprove: true, canReject: true, canDisable: true, canRestore: false })
  assert.deepEqual(getResellerProfileActionState('active'), { canApprove: false, canReject: false, canDisable: true, canRestore: false })
  assert.deepEqual(getResellerProfileActionState('disabled'), { canApprove: false, canReject: false, canDisable: false, canRestore: true })
})

test('admin reseller domain actions follow domain state', () => {
  assert.deepEqual(getResellerDomainActionState('pending_review'), { canApprove: true, canDisable: true })
  assert.deepEqual(getResellerDomainActionState('active'), { canApprove: false, canDisable: true })
  assert.deepEqual(getResellerDomainActionState('disabled'), { canApprove: true, canDisable: false })
})
