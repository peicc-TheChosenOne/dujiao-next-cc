import { afterEach, expect, test, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import i18n from '@/i18n'
import ResellerProfiles from './ResellerProfiles.vue'

const mocks = vi.hoisted(() => ({ getProfiles: vi.fn(), approve: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { getResellerProfiles: mocks.getProfiles, approveResellerProfile: mocks.approve } }))
vi.mock('@/utils/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))

const flush = async () => {
  await new Promise((resolve) => setTimeout(resolve, 0))
  await nextTick()
}
let unmount = () => {}
afterEach(() => {
  unmount()
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

test('reseller review UI selects eligible profiles and retries only failed approvals', async () => {
  const rows = [
    { id: 1001, user_id: 1, status: 'pending_review', default_markup_percent: '0.00', max_markup_percent: '0.00' },
    { id: 1002, user_id: 2, status: 'pending_review', default_markup_percent: '0.00', max_markup_percent: '0.00' },
    { id: 1003, user_id: 3, status: 'active', default_markup_percent: '0.00', max_markup_percent: '0.00' },
  ]
  mocks.getProfiles.mockImplementation(async () => ({ data: { data: rows.map((row) => ({ ...row })), pagination: { page: 1, page_size: 20, total: 3, total_page: 1 } } }))
  let failSecond = true
  mocks.approve.mockImplementation(async (id: number) => {
    if (id === 1002 && failSecond) { failSecond = false; throw new Error('Domain conflict') }
    rows.find((row) => row.id === id)!.status = 'active'
    return { data: { data: { already_approved: false, system_domain: { domain: `r${id}.example.test` } } } }
  })
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: ResellerProfiles }] })
  await router.push('/')
  await router.isReady()
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(RouterView).use(router).use(i18n)
  app.mount(host)
  unmount = () => app.unmount()
  await flush()
  const checkboxes = Array.from(document.querySelectorAll<HTMLButtonElement>('[role="checkbox"]'))
  expect(checkboxes).toHaveLength(4)
  expect(checkboxes[3]!.disabled).toBe(true)
  checkboxes[0]!.click()
  await flush()
  const clickButton = async (text: string) => {
    const button = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find((button) => button.textContent?.trim() === text)
    expect(button, text).toBeDefined()
    button!.click()
    await flush()
  }
  await clickButton('批量通过')
  await clickButton('确认通过 2 位分销商')
  await flush()
  expect(mocks.approve.mock.calls.map((args) => args[0])).toEqual([1001, 1002])
  expect(document.body.textContent).toContain('Domain conflict')
  expect(document.body.textContent).toContain('r1001.example.test')
  await clickButton('重试失败项')
  await flush()
  expect(mocks.approve.mock.calls.map((args) => args[0])).toEqual([1001, 1002, 1002])
  expect(document.body.textContent).toContain('通过 2 位，已处理 0 位，失败 0 位')
  expect(document.body.textContent).toContain('r1002.example.test')
})
