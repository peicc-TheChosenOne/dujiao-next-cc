import { amountToCents } from './money.ts'

type RetailQuote = { supply_price_amount: string | null; sale_price_amount: string; min_sale_price_amount: string; setting?: { pricing_mode: string; is_listed: boolean } }
export const hasSavedRetailPrice = (quote: RetailQuote): boolean =>
  quote.setting?.pricing_mode === 'fixed_price' && (amountToCents(quote.sale_price_amount) ?? 0) > 0
export const getInitialRetailDraft = (quote: RetailQuote) => ({
  price: hasSavedRetailPrice(quote) ? quote.sale_price_amount : quote.supply_price_amount ? quote.min_sale_price_amount : '',
  listed: hasSavedRetailPrice(quote) && quote.setting?.is_listed === true,
})

type Constraints = { supply_price_amount: string | null; min_sale_price_amount: string; max_sale_price_amount: string | null; error_code: string }
export const validateRetailPrice = (price: string, quote: Constraints): string => {
  if (['supply_missing', 'supply_below_cost', 'range_conflict'].includes(quote.error_code)) return quote.error_code
  if (!/^\d+(\.\d{1,2})?$/.test(price.trim())) return 'invalid'
  const amount = amountToCents(price)
  const minimum = amountToCents(quote.min_sale_price_amount)
  const maximum = amountToCents(quote.max_sale_price_amount)
  if (amount === null || amount <= 0 || minimum === null) return 'invalid'
  if (amount < minimum) return 'below_minimum'
  if (maximum !== null && amount > maximum) return 'markup_exceeded'
  return ''
}
export const buildRetailSetting = (skuID: number, draft: { price: string; listed: boolean; revision: string }, visibilityOnly = false) => ({
  sku_id: skuID,
  is_listed: draft.listed,
  ...(visibilityOnly ? {} : { pricing_mode: 'fixed_price', fixed_price_amount: draft.price.trim(), pricing_revision: draft.revision }),
})
