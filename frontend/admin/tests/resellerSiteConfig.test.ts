import test from 'node:test'
import assert from 'node:assert/strict'
import {
  blankLocalizedText,
  canEditResellerSiteConfig,
  getLocalizedText,
  normalizeFooterLinksForForm,
  normalizeLocalizedTextForForm,
  normalizeResellerBannerForForm,
} from '../src/utils/resellerSiteConfig.ts'

test('admin reseller site config localized text falls back predictably', () => {
  assert.deepEqual(blankLocalizedText(), { 'zh-CN': '', 'zh-TW': '', 'en-US': '' })
  assert.equal(getLocalizedText({ 'zh-CN': '简体', 'en-US': 'English' }, 'zh-TW'), '简体')
  assert.equal(getLocalizedText({ 'en-US': 'English' }, 'zh-TW'), 'English')
  assert.equal(getLocalizedText({ title: 'Plain' }, 'zh-CN'), 'Plain')
})

test('admin reseller site config form normalizers keep supported locales only', () => {
  assert.deepEqual(normalizeLocalizedTextForForm({ 'zh-CN': 'A', fr: 'B' }), {
    'zh-CN': 'A',
    'zh-TW': '',
    'en-US': '',
  })
  assert.deepEqual(
    normalizeFooterLinksForForm([
      { name: { 'zh-CN': '帮助', 'en-US': 'Help' }, url: 'https://example.com/help' },
    ]),
    [
      {
        name: { 'zh-CN': '帮助', 'zh-TW': '', 'en-US': 'Help' },
        url: 'https://example.com/help',
      },
    ],
  )
})

test('admin reseller site config edit state follows reseller status', () => {
  assert.equal(canEditResellerSiteConfig({ status: 'active' }), true)
  assert.equal(canEditResellerSiteConfig({ status: 'pending_review' }), false)
  assert.equal(canEditResellerSiteConfig({ status: 'disabled' }), false)
})

test('admin form keeps reseller banner data while normalizing legacy configs', () => {
  assert.equal(normalizeResellerBannerForForm(null).mode, 'inherit')
  const banner = normalizeResellerBannerForForm({ mode: 'custom', image: '/desktop.png', mobile_image: '/mobile.png', title: { 'en-US': 'My store' }, link_type: 'internal', link_value: '/products', open_in_new_tab: true })
  assert.equal(banner.image, '/desktop.png')
  assert.equal(banner.mobile_image, '/mobile.png')
  assert.equal(banner.title['en-US'], 'My store')
  assert.equal(banner.link_value, '/products')
  assert.equal(banner.open_in_new_tab, true)
})
