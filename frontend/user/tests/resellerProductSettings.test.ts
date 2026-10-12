import test from 'node:test'
import assert from 'node:assert/strict'
import {
  buildResellerProductSettingPayload,
  getResellerPricingModeLabelKey,
  isResellerProductSettingDetail,
  normalizeResellerProductSettingsPagination,
  normalizeResellerProductSettingForm,
  summarizeEffectivePrice,
} from '../src/utils/resellerProductSettings.ts'

test('pricing mode label keys are stable', () => {
  assert.equal(getResellerPricingModeLabelKey('inherit'), 'inherit')
  assert.equal(getResellerPricingModeLabelKey('markup_percent'), 'markupPercent')
  assert.equal(getResellerPricingModeLabelKey('fixed_markup'), 'fixedMarkup')
  assert.equal(getResellerPricingModeLabelKey('fixed_price'), 'fixedPrice')
  assert.equal(getResellerPricingModeLabelKey('bad'), 'unknown')
})

test('form normalization defaults money fields and listed state', () => {
  assert.deepEqual(normalizeResellerProductSettingForm({ sku_id: 3 }), {
    sku_id: 3,
    is_listed: true,
    pricing_mode: 'inherit',
    markup_percent: '0.00',
    fixed_markup_amount: '0.00',
    fixed_price_amount: '0.00',
    sort_order: 0,
  })
})

test('payload builder trims empty rows by explicit sku scope', () => {
  const payload = buildResellerProductSettingPayload([
    { sku_id: 0, is_listed: true, pricing_mode: 'markup_percent', markup_percent: ' 12.50 ', fixed_markup_amount: '', fixed_price_amount: '', sort_order: 0 },
  ])
  assert.deepEqual(payload, {
    settings: [
      { sku_id: 0, is_listed: true, pricing_mode: 'markup_percent', markup_percent: '12.50', fixed_markup_amount: '0.00', fixed_price_amount: '0.00', sort_order: 0 },
    ],
  })
})

test('effective price summary prefers explicit effective price', () => {
  assert.equal(summarizeEffectivePrice({ effective_price_amount: '128.00', pricing_mode: 'fixed_price' }), '128.00')
  assert.equal(summarizeEffectivePrice(null), '-')
})

test('reset response is not treated as product setting detail', () => {
  assert.equal(isResellerProductSettingDetail({ deleted: true }), false)
  assert.equal(
    isResellerProductSettingDetail({
      product: { id: 9, title: { 'zh-CN': '商品' }, slug: 'demo', price_amount: '10.00', is_active: true },
      skus: [],
    }),
    true,
  )
})

test('product settings pagination normalizes API pagination payloads', () => {
  assert.deepEqual(
    normalizeResellerProductSettingsPagination({ page: '2', page_size: '30', total: '61', total_page: '3' }),
    { page: 2, page_size: 30, total: 61, total_page: 3 },
  )
  assert.deepEqual(
    normalizeResellerProductSettingsPagination(null, { page: 4, page_size: 20, total: 80, total_page: 4 }),
    { page: 4, page_size: 20, total: 80, total_page: 4 },
  )
})

// Money constraints must reject rounding and allow a visibility-only unlist.
import { validateRetailPrice, buildRetailSetting, getInitialRetailDraft, hasSavedRetailPrice } from '../src/utils/resellerSupplyPricing.ts'
test('fixed retail respects supply floor and capped price', () => {
 const quote = { supply_price_amount: '80.01', min_sale_price_amount: '85.00', max_sale_price_amount: '88.01', error_code: '' }
 assert.equal(validateRetailPrice('85', quote), '')
 assert.equal(validateRetailPrice('84.99', quote), 'below_minimum')
 assert.equal(validateRetailPrice('88.02', quote), 'markup_exceeded')
 assert.equal(validateRetailPrice('85.001', quote), 'invalid')
 assert.equal(validateRetailPrice('85', { ...quote, error_code: 'supply_missing' }), 'supply_missing')
 assert.deepEqual(buildRetailSetting(1, { price: 'broken draft', listed: false, revision: 'r' }, true), { sku_id: 1, is_listed: false })
 assert.deepEqual(buildRetailSetting(1, { price: ' 87.00 ', listed: true, revision: 'r' }), { sku_id: 1, is_listed: true, pricing_mode: 'fixed_price', fixed_price_amount: '87.00', pricing_revision: 'r' })
})

test('unpriced variants start with minimum retail and remain unlisted until saved', () => {
 const quote = { supply_price_amount: '80.01', sale_price_amount: '0.00', min_sale_price_amount: '85.00', draft_price_amount: '120.00' }
 assert.deepEqual(getInitialRetailDraft(quote), { price: '85.00', listed: false })
 assert.equal(hasSavedRetailPrice(quote), false)
 assert.deepEqual(getInitialRetailDraft({ ...quote, setting: { pricing_mode: 'inherit', is_listed: false } }), { price: '85.00', listed: false })
 assert.deepEqual(getInitialRetailDraft({ ...quote, setting: { pricing_mode: 'fixed_price', is_listed: true } }), { price: '85.00', listed: false })
 assert.deepEqual(getInitialRetailDraft({ ...quote, supply_price_amount: null }), { price: '', listed: false })
 assert.deepEqual(getInitialRetailDraft({ ...quote, min_sale_price_amount: '80.01' }), { price: '80.01', listed: false })
 assert.deepEqual(buildRetailSetting(1, { price: '85.00', listed: false, revision: 'r' }), { sku_id: 1, is_listed: false, pricing_mode: 'fixed_price', fixed_price_amount: '85.00', pricing_revision: 'r' })
})

test('saved retail remains unchanged after unlisting or a supply quote increase', () => {
 const quote = { supply_price_amount: '90.00', sale_price_amount: '87.00', min_sale_price_amount: '95.00', setting: { pricing_mode: 'fixed_price', is_listed: false } }
 assert.equal(hasSavedRetailPrice(quote), true)
 assert.deepEqual(getInitialRetailDraft(quote), { price: '87.00', listed: false })
 assert.deepEqual(getInitialRetailDraft({ ...quote, setting: { ...quote.setting, is_listed: true } }), { price: '87.00', listed: true })
 assert.deepEqual(getInitialRetailDraft({ ...quote, supply_price_amount: null }), { price: '87.00', listed: false })
})
