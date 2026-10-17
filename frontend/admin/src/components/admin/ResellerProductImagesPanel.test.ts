import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import i18n from '@/i18n'
import ResellerProductImagesPanel from './ResellerProductImagesPanel.vue'
import type { AdminResellerProductImages } from '@/api/types'

const mocks = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), update: vi.fn(), reset: vi.fn(), upload: vi.fn(), writable: true }))
vi.mock('@/api/admin', () => ({ adminAPI: { getResellerProductImages: mocks.list, getResellerProductImagesDetail: mocks.detail, updateResellerProductImages: mocks.update, resetResellerProductDetailImage: mocks.reset, uploadResellerProductImage: mocks.upload } }))
vi.mock('@/stores/auth', () => ({ useAdminAuthStore: () => ({ hasPermission: () => mocks.writable }) }))
vi.mock('@/utils/notify', () => ({ notifyError: vi.fn(), notifySuccess: vi.fn() }))
vi.mock('@/utils/confirm', () => ({ confirmAction: async () => true }))

const flush = async () => { await new Promise(resolve => setTimeout(resolve, 0)); await nextTick() }
let unmount = () => {}
let row: AdminResellerProductImages
beforeEach(() => {
  mocks.writable = true
  row = { reseller_id: 7, user_id: 11, owner_label: 'reseller@example.test', profile_status: 'active', can_edit: true, product: { id: 12, slug: 'product', title: { 'zh-CN': '没有定价记录但自定义了图片的商品' } }, platform_images: ['/main.png'], custom_images: ['/one.png', '/two.png'], effective_images: ['/one.png', '/two.png'], image_source: 'custom', custom_detail_image: '/detail.png', detail_image_source: 'custom', platform_detail_content: { 'zh-CN': '<p>主站详情</p><script>bad()</script>' }, revision: 10, operation_id: '', updated_at: '2026-10-17T00:00:00Z' }
  mocks.list.mockImplementation(async () => ({ data: { data: [structuredClone(row)], pagination: { page: 1, page_size: 20, total: 1, total_page: 1 } } }))
  mocks.detail.mockImplementation(async () => ({ data: { data: structuredClone(row) } }))
  mocks.update.mockImplementation(async (_reseller, _product, payload) => {
    row = { ...row, revision: row.revision + 1, operation_id: payload.operation_id, custom_images: payload.images, effective_images: payload.images.length ? payload.images : row.platform_images, image_source: payload.images.length ? 'custom' : 'platform' }
    return { data: { data: structuredClone(row) } }
  })
  mocks.reset.mockImplementation(async (_reseller, _product, payload) => {
    row = { ...row, revision: row.revision + 1, operation_id: payload.operation_id, custom_detail_image: '', detail_image_source: 'platform' }
    return { data: { data: structuredClone(row) } }
  })
})
afterEach(() => { unmount(); document.body.innerHTML = ''; vi.clearAllMocks() })
async function mount() {
  const host = document.createElement('div'); document.body.append(host)
  const app = createApp(ResellerProductImagesPanel, { resellerId: '7' }).use(i18n)
  app.mount(host); unmount = () => app.unmount(); await flush()
}
async function click(text: string) {
  const button = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.trim() === text)
  expect(button, text).toBeDefined(); button!.click(); await flush()
}

test('lists image-only products and restores gallery and details independently', async () => {
  await mount()
  expect(mocks.list).toHaveBeenCalledWith(expect.objectContaining({ reseller_id: '7', scope: 'customized' }))
  expect(document.body.textContent).toContain('没有定价记录但自定义了图片的商品')
  await click('管理图片')
  expect(document.querySelectorAll('script')).toHaveLength(0)
  const move = Array.from(document.querySelectorAll<HTMLButtonElement>('button[aria-label="前移"]')).find(button => !button.disabled)!
  move.click(); await flush()
  expect(mocks.update).toHaveBeenLastCalledWith(7, 12, expect.objectContaining({ images: ['/two.png', '/one.png'], revision: 10, operation_id: expect.any(String) }))
  await click('恢复主站商品图')
  expect(row.custom_images).toEqual([])
  expect(row.custom_detail_image).toBe('/detail.png')
  await click('恢复主站详情')
  expect(row.custom_detail_image).toBe('')
  expect(row.image_source).toBe('platform')
  expect(mocks.reset).toHaveBeenCalledWith(7, 12, expect.objectContaining({ revision: 12 }))
})

test('failed mutations require a refresh before further writes', async () => {
  await mount(); await click('管理图片')
  mocks.update.mockRejectedValueOnce(new Error('conflict'))
  await click('恢复主站商品图')
  expect(document.body.textContent).toContain('conflict')
  const upload = Array.from(document.querySelectorAll<HTMLButtonElement>('button')).find(button => button.textContent?.trim() === '上传商品图')!
  expect(upload.disabled).toBe(true)
  await click('刷新最新配置')
  expect(upload.disabled).toBe(false)
  expect(mocks.detail).toHaveBeenCalledTimes(2)
})

test('inactive resellers and read-only admins can view but cannot mutate', async () => {
  row.can_edit = false; row.profile_status = 'disabled'
  await mount(); await click('管理图片')
  expect(document.body.textContent).toContain('仅可查看')
  expect(Array.from(document.querySelectorAll<HTMLInputElement>('input[type="file"]')).every(input => input.disabled)).toBe(true)
  expect(mocks.update).not.toHaveBeenCalled()
  expect(mocks.upload).not.toHaveBeenCalled()
})

test('read-only admin permissions disable writes for an active reseller', async () => {
  mocks.writable = false
  await mount(); await click('管理图片')
  expect(Array.from(document.querySelectorAll<HTMLInputElement>('input[type="file"]')).every(input => input.disabled)).toBe(true)
  expect(mocks.update).not.toHaveBeenCalled()
})

test('oversized uploads are rejected before sending a request', async () => {
  await mount(); await click('管理图片')
  const input = document.querySelector<HTMLInputElement>('input[aria-label="上传商品图"]')!
  const file = new File([new Uint8Array(500 * 1024 + 1)], 'large.png', { type: 'image/png' })
  Object.defineProperty(input, 'files', { configurable: true, value: [file] })
  input.dispatchEvent(new Event('change', { bubbles: true })); await flush()
  expect(mocks.upload).not.toHaveBeenCalled()
})
