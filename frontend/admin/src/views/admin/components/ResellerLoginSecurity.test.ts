import { afterEach, expect, test, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import i18n from '@/i18n'
import ResellerLoginSecurity from './ResellerLoginSecurity.vue'
import type { AdminResellerProfile } from '@/api/types'

const mocks = vi.hoisted(() => ({ update: vi.fn(), reset: vi.fn(), writable: true }))
vi.mock('@/api/admin', () => ({ adminAPI: { updateResellerLoginSecurity: mocks.update, resetResellerLoginSecurity: mocks.reset } }))
vi.mock('@/stores/auth', () => ({ useAdminAuthStore: () => ({ hasPermission: () => mocks.writable }) }))
vi.mock('@/utils/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
let unmount = () => {}
const flush = async () => { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }
afterEach(() => { unmount(); document.body.innerHTML = ''; vi.clearAllMocks(); mocks.writable = true })

const mount = async () => {
  const host = document.createElement('div'); document.body.append(host)
  const profile = { id: 7, user_id: 11, status: 'active', login_security: { required: true, bound: true, binding_state: 'bound', action: '', reason: '', admin_id: 0 } } as AdminResellerProfile
  const app = createApp(ResellerLoginSecurity, { profile }).use(i18n)
  app.mount(host); unmount = () => app.unmount(); await flush()
}
const fillReason = async () => {
  const input = document.querySelector('textarea')!
  input.value = '已核验账号，设备丢失'; input.dispatchEvent(new Event('input', { bubbles: true })); await flush()
}

test('pausing requires a reason and submits only the pause policy', async () => {
  mocks.update.mockResolvedValue({ data: { data: {} } })
  await mount()
  document.querySelector<HTMLButtonElement>('[role="switch"]')!.click(); await flush()
  const submit = document.querySelector<HTMLButtonElement>('button[type="submit"]')!
  expect(submit.disabled).toBe(true)
  await fillReason(); submit.click(); await flush()
  expect(mocks.update).toHaveBeenCalledWith(7, { required: false, reason: '已核验账号，设备丢失' })
  expect(mocks.reset).not.toHaveBeenCalled()
})

test('reset requires explicit ownership verification and uses a separate operation', async () => {
  mocks.reset.mockResolvedValue({ data: { data: {} } })
  await mount()
  const reset = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.includes('重置验证器'))!
  reset.click(); await flush(); await fillReason()
  const submit = document.querySelector<HTMLButtonElement>('button[type="submit"]')!
  expect(submit.disabled).toBe(true)
  document.querySelector<HTMLInputElement>('input[type="checkbox"]')!.click(); await flush()
  expect(submit.disabled).toBe(false)
  submit.click(); await flush()
  expect(mocks.reset).toHaveBeenCalledWith(7, { reason: '已核验账号，设备丢失', identity_verified: true })
  expect(mocks.update).not.toHaveBeenCalled()
})

test('read-only administrators cannot change or reset login verification', async () => {
  mocks.writable = false
  await mount()
  expect(document.querySelector('[role="switch"]')).toBeNull()
  expect(document.querySelectorAll('button')).toHaveLength(0)
  expect(document.body.textContent).toContain('已绑定')
})
