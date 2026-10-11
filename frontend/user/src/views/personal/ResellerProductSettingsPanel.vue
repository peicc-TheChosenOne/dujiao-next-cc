<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, reactive, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { DialogRoot, DialogPortal, DialogOverlay, DialogContent, DialogTitle, DialogDescription } from 'reka-ui'
import { resellerAPI } from '../../api/reseller'
import type { ResellerProductSettingDetailData, ResellerProductSettingSKUData } from '../../api/types'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { useLocalized } from '../../composables/useProduct'
import { normalizeResellerProductSettingsPagination } from '../../utils/resellerProductSettings'
import { formatSkuSpecValues } from '../../utils/sku'
import { amountToCents, centsToAmount } from '../../utils/money'
import { validateRetailPrice, buildRetailSetting } from '../../utils/resellerSupplyPricing'

withDefaults(defineProps<{ embedded?: boolean }>(), { embedded: false })
const { t, locale } = useI18n()
const { getLocalizedText, formatPrice } = useLocalized()
const rows = ref<ResellerProductSettingDetailData[]>([])
const loading = ref(false)
const keyword = ref('')
const alert = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0, total_page: 1 })
type Draft = { price: string; listed: boolean; initialPrice: string; initialListed: boolean; revision: string; saving: boolean; message: string; failed: boolean }
const drafts = reactive<Record<number, Draft>>({})
const draftFor = (id: number): Draft => drafts[id]!
const flatRows = computed(() => rows.value.flatMap(product => product.skus.map(sku => ({ product, sku }))))
const dirty = (draft: Draft) => draft.price !== draft.initialPrice || draft.listed !== draft.initialListed
const hasDirty = computed(() => Object.values(drafts).some(dirty))
const busy = computed(() => Object.values(drafts).some(d => d.saving))
const dialogOpen = ref(false)
const resolving = ref(false)
let resolveNavigation: ((choice: string) => void) | undefined
const prefix = 'supplyPricing.'
const text = (key: string) => t(prefix + key)
const seed = (sku: ResellerProductSettingSKUData) => {
  const price = sku.setting?.pricing_mode === 'fixed_price' ? sku.sale_price_amount : sku.supply_price_amount ? sku.draft_price_amount : ''
  const listed = sku.setting?.is_listed === true
  drafts[sku.id] = { price, listed, initialPrice: price, initialListed: listed, revision: sku.pricing_revision, saving: false, message: '', failed: false }
}
const priceError = (sku: ResellerProductSettingSKUData) => validateRetailPrice(draftFor(sku.id)?.price || '', sku)
const quoteBlocked = (sku: ResellerProductSettingSKUData) => ['supply_missing', 'supply_below_cost', 'range_conflict'].includes(sku.error_code)
const profit = (sku: ResellerProductSettingSKUData) => {
  const price = amountToCents(draftFor(sku.id)?.price)
  const supply = amountToCents(sku.supply_price_amount)
  return price === null || supply === null ? '-' : formatPrice(centsToAmount(price - supply))
}
const specLabel = (product: ResellerProductSettingDetailData, sku: ResellerProductSettingSKUData) =>
  product.skus.length === 1 && sku.sku_code.toUpperCase() === 'DEFAULT' ? text('defaultSpec') : formatSkuSpecValues(sku.spec_values, String(locale.value)) || sku.sku_code
const status = (product: ResellerProductSettingDetailData, sku: ResellerProductSettingSKUData) => {
  if (!sku.is_active) return text('sku_inactive')
  if (product.product_setting?.is_listed === false) return text('product_hidden')
  if (quoteBlocked(sku)) return text(sku.error_code)
  if (priceError(sku)) return text(priceError(sku))
  if (dirty(draftFor(sku.id))) return text('unsaved')
  return sku.can_sell ? text('listed') : text('unlisted')
}
const loadRows = async (page: number) => {
  loading.value = true
  alert.value = ''
  try {
    const response = await resellerAPI.productSettings({ page, page_size: pagination.page_size, keyword: keyword.value.trim() })
    rows.value = response.data.data || []
    Object.keys(drafts).forEach(id => delete drafts[Number(id)])
    flatRows.value.forEach(({ sku }) => seed(sku))
    Object.assign(pagination, normalizeResellerProductSettingsPagination(response.data.pagination, pagination))
  } catch (err: any) { alert.value = err?.message || text('loadFailed') }
  finally { loading.value = false }
}
const updateProduct = (detail: ResellerProductSettingDetailData) => {
  const index = rows.value.findIndex(p => p.product.id === detail.product.id)
  if (index >= 0) rows.value[index] = detail
}
const saveRow = async (product: ResellerProductSettingDetailData, sku: ResellerProductSettingSKUData) => {
  const draft = draftFor(sku.id)
  if (draft.saving) return false
  const visibilityOnly = !draft.listed && (quoteBlocked(sku) || !!priceError(sku))
  if (!visibilityOnly && priceError(sku)) { draft.message = text(priceError(sku)); draft.failed = true; return false }
  draft.saving = true
  draft.message = ''
  try {
    const response = await resellerAPI.updateProductSettings(product.product.id, { settings: [buildRetailSetting(sku.id, draft, visibilityOnly)] })
    const detail = response.data.data as ResellerProductSettingDetailData
    const updated = detail.skus.find(s => s.id === sku.id)!
    updateProduct(detail)
    seed(updated)
    if (visibilityOnly) draftFor(sku.id).price = draft.price
    draftFor(sku.id).message = text('saved')
    return !dirty(draftFor(sku.id))
  } catch (err: any) {
    draft.failed = true
    draft.message = err?.message || text('saveFailed')
    // Preserve the user's draft while refreshing constraints for their next confirmation.
    try {
      const response = await resellerAPI.productSettingDetail(product.product.id)
      const detail = response.data.data as ResellerProductSettingDetailData
      updateProduct(detail)
      const fresh = detail.skus.find(s => s.id === sku.id)
      if (fresh) draft.revision = fresh.pricing_revision
    } catch { /* Keep the current constraints if refresh fails. */ }
    return false
  } finally { draft.saving = false }
}
const resolveChoice = (choice: string) => { resolveNavigation?.(choice); resolveNavigation = undefined; dialogOpen.value = false }
const confirmDrafts = async () => {
  if (busy.value || dialogOpen.value || resolving.value) return false
  if (!hasDirty.value) return true
  dialogOpen.value = true
  const choice = await new Promise<string>(resolve => { resolveNavigation = resolve })
  if (choice === 'discard') return true
  if (choice !== 'save') return false
  resolving.value = true
  try {
    for (const { product, sku } of flatRows.value) {
      if (dirty(draftFor(sku.id)) && !await saveRow(product, sku)) return false
    }
    return !hasDirty.value
  } finally { resolving.value = false }
}
const navigate = async (page: number) => { if (await confirmDrafts()) await loadRows(page) }
const restoreProduct = async (product: ResellerProductSettingDetailData) => {
  if (busy.value) return
  try {
    const response = await resellerAPI.updateProductSettings(product.product.id, { settings: [{ sku_id: 0, is_listed: true }] })
    updateProduct(response.data.data)
    // Only the parent visibility changed. Keep every SKU draft and listing choice.
    for (const sku of (response.data.data as ResellerProductSettingDetailData).skus) {
      const previous = product.skus.find(s => s.id === sku.id)
      if (previous?.sale_price_amount === sku.sale_price_amount && previous?.supply_price_amount === sku.supply_price_amount && previous?.min_sale_price_amount === sku.min_sale_price_amount && previous?.max_sale_price_amount === sku.max_sale_price_amount) draftFor(sku.id).revision = sku.pricing_revision
    }
  } catch (err: any) { alert.value = err?.message || text('saveFailed') }
}
const beforeUnload = (event: BeforeUnloadEvent) => { if (hasDirty.value) { event.preventDefault(); event.returnValue = '' } }
onBeforeRouteLeave(confirmDrafts)
onMounted(() => { window.addEventListener('beforeunload', beforeUnload); loadRows(1) })
onBeforeUnmount(() => { window.removeEventListener('beforeunload', beforeUnload); resolveNavigation?.('cancel') })
</script>

<template>
  <Card :class="embedded ? 'p-5 sm:p-6' : 'p-6 sm:p-7'">
    <Alert v-if="alert" variant="destructive" class="mb-4"><AlertDescription>{{ alert }}</AlertDescription></Alert>
    <div class="mb-4 flex flex-col gap-3 sm:flex-row">
      <Input v-model="keyword" :placeholder="t('personalCenter.reseller.productSettings.searchPlaceholder')" @keyup.enter="navigate(1)" />
      <Button :disabled="loading || busy || resolving" @click="navigate(1)">{{ t('orders.filters.search') }}</Button>
      <Button variant="outline" :disabled="loading || busy || resolving" @click="navigate(pagination.page)">{{ t('orders.filters.refresh') }}</Button>
    </div>
    <p v-if="loading" class="p-6 text-muted-foreground">{{ text('loading') }}</p>
    <p v-else-if="!flatRows.length" class="p-6 text-muted-foreground">{{ t('personalCenter.reseller.productSettings.empty') }}</p>
    <div v-else class="overflow-hidden rounded-xl border">
      <div class="hidden grid-cols-[minmax(180px,2fr)_minmax(100px,1fr)_minmax(160px,1.4fr)_minmax(100px,1fr)_minmax(110px,1fr)_80px] gap-4 border-b bg-muted/30 px-4 py-3 text-xs text-muted-foreground lg:grid">
        <span>{{ text('product') }}</span><span>{{ text('supply') }}</span><span>{{ text('retail') }}</span><span>{{ text('profit') }}</span><span>{{ text('listing') }}</span><span>{{ text('action') }}</span>
      </div>
      <div v-for="{ product, sku } in flatRows" :key="sku.id" class="grid gap-4 border-b px-4 py-4 last:border-b-0 lg:grid-cols-[minmax(180px,2fr)_minmax(100px,1fr)_minmax(160px,1.4fr)_minmax(100px,1fr)_minmax(110px,1fr)_80px] lg:items-center">
        <div class="min-w-0">
          <p class="truncate text-sm font-semibold" :title="getLocalizedText(product.product.title)">{{ getLocalizedText(product.product.title) }}</p>
          <p class="mt-1 text-xs text-muted-foreground">{{ specLabel(product, sku) }}</p>
          <Button v-if="product.product_setting?.is_listed === false" variant="link" size="sm" class="h-auto p-0 text-xs" @click="restoreProduct(product)">{{ text('restoreProduct') }}</Button>
        </div>
        <div><span class="mb-1 block text-xs text-muted-foreground lg:hidden">{{ text('supply') }}</span><div class="rounded-xl border bg-muted/20 px-3 py-2 font-mono text-sm">{{ formatPrice(sku.supply_price_amount) }}</div></div>
        <div>
          <label :for="`retail-${sku.id}`" class="mb-1 block text-xs text-muted-foreground lg:hidden">{{ text('retail') }}</label>
          <Input :id="`retail-${sku.id}`" v-model="draftFor(sku.id).price" inputmode="decimal" class="font-mono" :disabled="draftFor(sku.id).saving || quoteBlocked(sku)" />
          <p v-if="sku.supply_price_amount" class="mt-1 text-xs text-muted-foreground">{{ text('minimum') }} {{ formatPrice(sku.min_sale_price_amount) }}<span v-if="sku.max_sale_price_amount"> · {{ text('maximum') }} {{ formatPrice(sku.max_sale_price_amount) }}</span></p>
          <Button v-if="sku.suggested_sale_price_amount && !quoteBlocked(sku)" variant="link" size="sm" class="h-auto p-0 text-xs" :disabled="draftFor(sku.id).saving" @click="draftFor(sku.id).price = sku.suggested_sale_price_amount!">{{ text('adopt') }} {{ formatPrice(sku.suggested_sale_price_amount) }}</Button>
        </div>
        <div class="font-mono text-sm font-semibold" :class="priceError(sku) ? 'text-destructive' : 'text-success'"><span class="mr-2 text-xs font-normal text-muted-foreground lg:hidden">{{ text('profit') }}</span>{{ profit(sku) }}</div>
        <div><div class="flex items-center gap-2"><Switch v-model="draftFor(sku.id).listed" :disabled="draftFor(sku.id).saving || (!draftFor(sku.id).listed && (quoteBlocked(sku) || !sku.is_active))" :aria-label="text('listing')" /><span class="text-xs text-muted-foreground">{{ status(product, sku) }}</span></div></div>
        <div><Button size="sm" :disabled="draftFor(sku.id).saving || (!dirty(draftFor(sku.id)) && (!!sku.setting || quoteBlocked(sku))) || (draftFor(sku.id).listed && !!priceError(sku))" @click="saveRow(product, sku)">{{ text(draftFor(sku.id).saving ? 'saving' : 'save') }}</Button><p v-if="draftFor(sku.id).message" class="mt-1 text-xs" :class="draftFor(sku.id).failed ? 'text-destructive' : 'text-success'" role="status">{{ draftFor(sku.id).message }}</p></div>
      </div>
    </div>
    <div v-if="pagination.total_page > 1" class="mt-5 flex items-center justify-center gap-3">
      <Button variant="outline" size="sm" :disabled="pagination.page <= 1 || busy" @click="navigate(pagination.page - 1)">{{ t('orders.prevPage') }}</Button>
      <span class="text-sm text-muted-foreground">{{ t('orders.pageInfo', { page: pagination.page, total: pagination.total_page }) }}</span>
      <Button variant="outline" size="sm" :disabled="pagination.page >= pagination.total_page || busy" @click="navigate(pagination.page + 1)">{{ t('orders.nextPage') }}</Button>
    </div>
    <DialogRoot :open="dialogOpen" @update:open="(open) => { if (!open) resolveChoice('cancel') }">
      <DialogPortal><DialogOverlay class="fixed inset-0 z-[110] bg-black/40" /><DialogContent class="fixed left-1/2 top-1/2 z-[111] w-[calc(100%_-_2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border bg-card p-6 shadow-xl">
        <DialogTitle class="font-semibold">{{ text('unsaved') }}</DialogTitle><DialogDescription class="mt-2 text-sm text-muted-foreground">{{ text('unsavedPrompt') }}</DialogDescription>
        <div class="mt-5 flex flex-wrap justify-end gap-2"><Button variant="outline" @click="resolveChoice('cancel')">{{ t('common.cancel') }}</Button><Button variant="secondary" @click="resolveChoice('discard')">{{ text('discard') }}</Button><Button @click="resolveChoice('save')">{{ text('saveAll') }}</Button></div>
      </DialogContent></DialogPortal>
    </DialogRoot>
  </Card>
</template>
