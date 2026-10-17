import test from 'node:test'
import assert from 'node:assert/strict'
import { isPublicAuthEndpoint } from '../src/utils/authEndpoints.ts'

test('enrollment endpoints do not redirect the unfinished login on an expired challenge', () => {
  assert.equal(isPublicAuthEndpoint('/auth/login/2fa/setup'), true)
  assert.equal(isPublicAuthEndpoint('/auth/login/2fa/enable'), true)
  assert.equal(isPublicAuthEndpoint('/me/2fa/setup'), false)
  assert.equal(isPublicAuthEndpoint('/reseller/profile'), false)
})
