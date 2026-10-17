import test from 'node:test'
import assert from 'node:assert/strict'
import {
  blankLocalizedText,
  hasLocalizedText,
  getLocalizedText,
  isResellerSiteSeoConfigured,
  normalizeFooterLinksForForm,
  canEditResellerSiteConfig,
  normalizeResellerBannerForForm,
  getResellerBannerOverride,
} from '../src/utils/resellerSiteConfig.ts'

test('blank localized text includes all supported storefront locales', () => {
  assert.deepEqual(blankLocalizedText(), { 'zh-CN': '', 'zh-TW': '', 'en-US': '' })
})

test('localized text fallback follows current locale then zh-CN then first non-empty', () => {
  const value = { 'zh-CN': '简体', 'zh-TW': '繁體', 'en-US': 'English' }
  assert.equal(getLocalizedText(value, 'en-US'), 'English')
  assert.equal(getLocalizedText({ 'zh-CN': '简体' }, 'zh-TW'), '简体')
  assert.equal(getLocalizedText({ 'en-US': 'English' }, 'zh-TW'), 'English')
})

test('footer links normalize missing localized names', () => {
  assert.deepEqual(
    normalizeFooterLinksForForm([{ name: { 'zh-CN': '客服' }, url: 'https://example.test' }]),
    [{ name: { 'zh-CN': '客服', 'zh-TW': '', 'en-US': '' }, url: 'https://example.test' }],
  )
})

test('site config editing requires opened and editable snapshot', () => {
  assert.equal(canEditResellerSiteConfig({ opened: true, can_edit: true }), true)
  assert.equal(canEditResellerSiteConfig({ opened: true, can_edit: false }), false)
  assert.equal(canEditResellerSiteConfig(null), false)
})

test('legacy site config inherits the main banner and form edits retain both images', () => {
  assert.equal(normalizeResellerBannerForForm(null).mode, 'inherit')
  const banner = normalizeResellerBannerForForm({ mode: 'custom', image: '/desktop.png', mobile_image: '/mobile.png', link_type: 'none' })
  assert.equal(banner.image, '/desktop.png')
  assert.equal(banner.mobile_image, '/mobile.png')
  assert.deepEqual(banner.title, blankLocalizedText())
})

test('only reseller tenants apply custom or hidden banners', () => {
  assert.equal(getResellerBannerOverride('main', { mode: 'custom', image: '/alice.png' }), null)
  assert.equal(getResellerBannerOverride('reseller', { mode: 'inherit' }), null)
  assert.equal(getResellerBannerOverride('reseller', null), null)
  assert.equal(getResellerBannerOverride('reseller', { mode: 'custom', image: '/alice.png' })?.image, '/alice.png')
  assert.equal(getResellerBannerOverride('reseller', { mode: 'hidden' })?.mode, 'hidden')
})

test('localized text helpers require at least one non-empty value', () => {
  assert.equal(hasLocalizedText({ 'zh-CN': '', 'zh-TW': ' ', 'en-US': '' }), false)
  assert.equal(hasLocalizedText({ 'zh-CN': '', 'zh-TW': '繁體', 'en-US': '' }), true)
})

test('site seo readiness ignores empty localized objects', () => {
  assert.equal(
    isResellerSiteSeoConfigured({
      title: { 'zh-CN': '', 'zh-TW': '', 'en-US': '' },
      keywords: { 'zh-CN': '', 'zh-TW': '', 'en-US': '' },
      description: { 'zh-CN': '', 'zh-TW': '', 'en-US': '' },
    }),
    false,
  )
  assert.equal(
    isResellerSiteSeoConfigured({
      title: { 'zh-CN': '品牌标题' },
      keywords: {},
      description: {},
    }),
    true,
  )
})
