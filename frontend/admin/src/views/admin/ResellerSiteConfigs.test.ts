import { afterEach, expect, test, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import i18n from '@/i18n'
import ResellerSiteConfigs from './ResellerSiteConfigs.vue'

const mocks = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), update: vi.fn() }))
vi.mock('@/api/admin', () => ({ adminAPI: { getResellerSiteConfigs: mocks.list, getResellerSiteConfig: mocks.get, updateResellerSiteConfig: mocks.update } }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: {} }) }))
vi.mock('@/utils/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('@/components/RichEditor.vue', () => ({ default: { template: '<div />' } }))
const flush = async () => { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }
let unmount = () => {}
afterEach(() => { unmount(); document.body.innerHTML = ''; vi.clearAllMocks() })

test('Banner shortcut opens the Banner tab and preserves unrelated site settings', async () => {
  const row = { id: 2, reseller_id: 7, site_name: '我的店铺', logo: '/logo.png', favicon: '/icon.png', profile: { status: 'active' }, banner: { mode: 'custom', image: '/banner.png', mobile_image: '', title: { 'zh-CN': '旧标题' }, subtitle: {}, link_type: 'none', link_value: '', open_in_new_tab: false }, announcement: { enabled: true, type: 'info', title: { 'zh-CN': '公告' }, content: { 'zh-CN': '<p>内容</p>' } }, support: { wechat: 'wechat-id', email: 'owner@example.test' }, seo: { title: { 'zh-CN': 'SEO' } }, footer_links: [{ name: { 'zh-CN': '链接' }, url: 'https://example.test' }], nav_config: { builtin: { blog: false, notice: true, about: true }, custom_items: [{ name: { 'zh-CN': '自定义导航' }, url: 'https://example.test/help' }] } }
  mocks.list.mockResolvedValue({ data: { data: [row], pagination: { page: 1, total: 1, total_page: 1, page_size: 20 } } })
  mocks.get.mockResolvedValue({ data: { data: row } })
  mocks.update.mockResolvedValue({ data: { data: row } })
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(ResellerSiteConfigs).use(i18n); app.mount(host); unmount = () => app.unmount(); await flush()
  const button = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.trim() === '管理 Banner')!
  expect(button).toBeDefined(); button.click(); await flush()
  const active = document.querySelector('[role="tab"][data-state="active"]')
  expect(active?.textContent).toContain('Banner')
  const title = document.querySelector<HTMLInputElement>('input[maxlength="120"]')!
  title.value = '新标题'; title.dispatchEvent(new Event('input', { bubbles: true })); await flush()
  const saveText = i18n.global.t('admin.resellerSiteConfigs.actions.save')
  Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.trim() === saveText)!.click(); await flush()
  expect(mocks.update).toHaveBeenCalledWith(7, expect.objectContaining({ site_name: row.site_name, logo: row.logo, favicon: row.favicon, banner: expect.objectContaining({ title: expect.objectContaining({ 'zh-CN': '新标题' }) }), support: expect.objectContaining(row.support), nav_config: row.nav_config, footer_links: [expect.objectContaining({ url: row.footer_links[0]!.url })] }))
})
