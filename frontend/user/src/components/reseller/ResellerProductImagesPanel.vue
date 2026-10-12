<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ArrowLeft, ArrowRight, ImagePlus, LoaderCircle, RotateCcw, Search, Star, Trash2 } from 'lucide-vue-next'
import { resellerAPI } from '../../api/reseller'
import type { ResellerProductImagesData } from '../../api/types'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { useLocalized } from '../../composables/useProduct'
import { getImageUrl } from '../../utils/image'
import { normalizeResellerProductSettingsPagination } from '../../utils/resellerProductSettings'
import { PRODUCT_IMAGE_MAX_COUNT, moveProductImage, saveProductImagesWithRecovery, validateProductImage } from '../../utils/resellerProductImages'

const { t } = useI18n()
const { getLocalizedText } = useLocalized()
const rows = ref<ResellerProductImagesData[]>([])
const keyword = ref('')
const loading = ref(false)
const error = ref('')
const pagination = reactive({ page: 1, page_size: 20, total: 0, total_page: 1 })
type RowStatus = { busy: boolean; failed: boolean; needsRefresh: boolean; message: string }
const statuses = reactive<Record<number, RowStatus>>({})
const status = (id: number) => statuses[id] ??= { busy: false, failed: false, needsRefresh: false, message: '' }
const saving = computed(() => Object.values(statuses).some(item => item.busy))
const disabled = (row: ResellerProductImagesData) => loading.value || status(row.product.id).busy || status(row.product.id).needsRefresh
const inputs = new Map<number, HTMLInputElement>()
const detailInputs = new Map<number, HTMLInputElement>()
const replaceRow = (row: ResellerProductImagesData) => {
  const index = rows.value.findIndex(item => item.product.id === row.product.id)
  if (index >= 0) rows.value[index] = row
}
const readRow = async (id: number): Promise<ResellerProductImagesData> => (await resellerAPI.productImageDetail(id)).data.data

async function load(page = 1) {
  if (saving.value || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const response = await resellerAPI.productImages({ page, page_size: pagination.page_size, keyword: keyword.value.trim() })
    rows.value = response.data.data
    Object.assign(pagination, normalizeResellerProductSettingsPagination(response.data.pagination, pagination))
    for (const row of rows.value) delete statuses[row.product.id]
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('resellerProductImages.loadFailed')
  } finally { loading.value = false }
}

async function mutate(row: ResellerProductImagesData, request: (operationId: string) => Promise<ResellerProductImagesData>) {
  const id = row.product.id
  const operationId = crypto.randomUUID()
  let refreshed = false
  try {
    const result = await saveProductImagesWithRecovery(operationId, () => request(operationId), () => readRow(id))
    replaceRow(result.state)
    refreshed = true
    if (!result.confirmed) {
      throw new Error((result.error instanceof Error ? result.error.message + ' ' : '') + t('resellerProductImages.refreshed'))
    }
    return result.state
  } catch (err) {
    // 无法读取实际结果时必须先刷新，不能继续拿旧版本上传。
    if (!refreshed) status(id).needsRefresh = true
    throw err
  }
}

async function update(row: ResellerProductImagesData, images: string[]) {
  if (disabled(row)) return
  const state = status(row.product.id)
  state.busy = true
  state.failed = false
  state.message = t('resellerProductImages.saving')
  try {
    await mutate(row, async operationId => (await resellerAPI.updateProductImages(row.product.id, { images, revision: row.revision, operation_id: operationId })).data.data)
    state.message = t('resellerProductImages.saved')
  } catch (err) {
    state.failed = true
    state.message = err instanceof Error ? err.message : t('resellerProductImages.saveFailed')
  } finally { state.busy = false }
}

async function upload(row: ResellerProductImagesData, event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length || disabled(row)) return
  const state = status(row.product.id)
  state.busy = true
  state.failed = false
  let current = row
  try {
    for (let index = 0; index < files.length; index++) {
      if (current.custom_images.length >= PRODUCT_IMAGE_MAX_COUNT) throw new Error(t('resellerProductImages.countError'))
      const file = files[index]!
      const validation = validateProductImage(file)
      if (validation) throw new Error(t(`resellerProductImages.${validation}`))
      state.message = t('resellerProductImages.uploading', { current: index + 1, total: files.length })
      current = await mutate(current, async operationId => (await resellerAPI.uploadProductImage(current.product.id, file, current.revision, operationId)).data.data)
    }
    state.message = t('resellerProductImages.saved')
  } catch (err) {
    state.failed = true
    state.message = err instanceof Error ? err.message : t('resellerProductImages.saveFailed')
  } finally { state.busy = false }
}

async function refreshRow(row: ResellerProductImagesData) {
  const state = status(row.product.id)
  if (state.busy) return
  state.busy = true
  try {
    replaceRow(await readRow(row.product.id))
    state.needsRefresh = false
    state.failed = false
    state.message = t('resellerProductImages.refreshed')
  } catch {
    state.failed = true
    state.message = t('resellerProductImages.refreshFailed')
  } finally { state.busy = false }
}

async function saveDetail(row: ResellerProductImagesData, file?: File) {
  if (disabled(row)) return
  const state = status(row.product.id)
  state.busy = true
  state.failed = false
  state.message = t('resellerProductImages.saving')
  try {
    if (file) {
      const validation = validateProductImage(file)
      if (validation) throw new Error(t(`resellerProductImages.${validation}`))
    }
    await mutate(row, async operationId => {
      const response = file
        ? await resellerAPI.uploadProductDetailImage(row.product.id, file, row.revision, operationId)
        : await resellerAPI.resetProductDetailImage(row.product.id, { revision: row.revision, operation_id: operationId })
      return response.data.data
    })
    state.message = t('resellerProductImages.saved')
  } catch (err) {
    state.failed = true
    state.message = err instanceof Error ? err.message : t('resellerProductImages.saveFailed')
  } finally { state.busy = false }
}

function uploadDetail(row: ResellerProductImagesData, event: Event) {
  const input = event.target as HTMLInputElement
  const files = Array.from(input.files || [])
  input.value = ''
  if (!files.length || disabled(row)) return
  if (files.length !== 1) {
    const state = status(row.product.id)
    state.failed = true
    state.message = t('resellerProductImages.detailCountError')
    return
  }
  void saveDetail(row, files[0]!)
}

onBeforeRouteLeave(() => {
  if (!saving.value) return true
  error.value = t('resellerProductImages.waitSaving')
  return false
})
function beforeUnload(event: BeforeUnloadEvent) {
  if (saving.value) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => { window.addEventListener('beforeunload', beforeUnload); void load() })
onBeforeUnmount(() => { window.removeEventListener('beforeunload', beforeUnload); inputs.clear(); detailInputs.clear() })
</script>

<template>
  <div class="space-y-4" data-testid="product-images-panel">
    <Card class="p-4 text-sm text-muted-foreground">{{ t('resellerProductImages.description') }}</Card>
    <form class="flex gap-2" @submit.prevent="load(1)">
      <div class="relative min-w-0 flex-1">
        <Search class="absolute left-3 top-3 h-4 w-4 text-muted-foreground" />
        <Input v-model="keyword" class="pl-9" :placeholder="t('resellerProductImages.search')" :aria-label="t('resellerProductImages.search')" :disabled="saving || loading" />
      </div>
      <Button type="submit" variant="outline" :disabled="saving || loading">{{ t('resellerProductImages.searchButton') }}</Button>
      <Button type="button" variant="outline" :aria-label="t('resellerProductImages.refresh')" :disabled="saving || loading" @click="load(pagination.page)"><RotateCcw class="h-4 w-4" /></Button>
    </form>
    <p v-if="error" role="alert" class="text-sm text-destructive">{{ error }}</p>
    <p v-if="loading" role="status" class="text-sm text-muted-foreground">{{ t('resellerProductImages.loading') }}</p>
    <Card v-if="rows.length" class="overflow-hidden divide-y">
      <article v-for="row in rows" :key="row.product.id" class="p-4 sm:p-5" :data-product-id="row.product.id">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div class="min-w-0 flex-1">
            <h3 class="break-words text-sm font-semibold">{{ getLocalizedText(row.product.title) }}</h3>
            <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span class="rounded-full bg-muted px-2 py-1">{{ t(`resellerProductImages.${row.image_source}`) }}</span>
              <span v-if="row.image_source === 'custom'">{{ row.custom_images.length }} / {{ PRODUCT_IMAGE_MAX_COUNT }}</span>
              <span v-else>{{ t('resellerProductImages.followPlatform') }}</span>
            </div>
          </div>
          <div class="flex flex-wrap gap-2">
            <Button v-if="row.image_source === 'custom'" size="sm" variant="outline" :disabled="disabled(row)" @click="update(row, [])">{{ t('resellerProductImages.restore') }}</Button>
            <Button size="sm" :disabled="disabled(row) || row.custom_images.length >= PRODUCT_IMAGE_MAX_COUNT" @click="inputs.get(row.product.id)?.click()"><ImagePlus class="mr-1 h-4 w-4" />{{ t('resellerProductImages.upload') }}</Button>
            <input :ref="el => { if (el) inputs.set(row.product.id, el as HTMLInputElement) }" type="file" class="hidden" accept="image/png,image/jpeg,image/gif,image/webp" multiple :aria-label="t('resellerProductImages.upload')" :disabled="disabled(row)" @change="upload(row, $event)" />
          </div>
        </div>
        <div class="mt-4 flex flex-wrap gap-3">
          <div v-for="(image, index) in row.effective_images" :key="image" class="w-28 sm:w-32">
            <div class="relative aspect-square overflow-hidden rounded-lg border bg-muted">
              <img :src="getImageUrl(image)" :alt="t('resellerProductImages.imageAlt', { index: index + 1 })" class="h-full w-full object-cover" loading="lazy" />
              <span v-if="index === 0" class="absolute left-1 top-1 rounded bg-background/90 px-2 py-0.5 text-xs font-medium">{{ t('resellerProductImages.cover') }}</span>
            </div>
            <div v-if="row.image_source === 'custom'" class="mt-2 flex justify-between gap-1">
              <Button size="icon" variant="ghost" class="h-7 w-7" :title="t('resellerProductImages.previous')" :aria-label="t('resellerProductImages.previous')" :disabled="disabled(row) || index === 0" @click="update(row, moveProductImage(row.custom_images, index, index - 1))"><ArrowLeft class="h-3.5 w-3.5" /></Button>
              <Button size="icon" variant="ghost" class="h-7 w-7" :title="t('resellerProductImages.next')" :aria-label="t('resellerProductImages.next')" :disabled="disabled(row) || index === row.custom_images.length - 1" @click="update(row, moveProductImage(row.custom_images, index, index + 1))"><ArrowRight class="h-3.5 w-3.5" /></Button>
              <Button size="icon" variant="ghost" class="h-7 w-7" :title="t('resellerProductImages.setCover')" :aria-label="t('resellerProductImages.setCover')" :disabled="disabled(row) || index === 0" @click="update(row, moveProductImage(row.custom_images, index, 0))"><Star class="h-3.5 w-3.5" /></Button>
              <Button size="icon" variant="ghost" class="h-7 w-7 text-destructive" :title="t('resellerProductImages.delete')" :aria-label="t('resellerProductImages.delete')" :disabled="disabled(row)" @click="update(row, row.custom_images.filter((_, position) => position !== index))"><Trash2 class="h-3.5 w-3.5" /></Button>
            </div>
          </div>
          <div v-if="!row.effective_images.length" class="flex h-28 w-28 items-center justify-center rounded-lg border border-dashed text-xs text-muted-foreground">{{ t('resellerProductImages.noImages') }}</div>
        </div>
        <p class="mt-3 text-xs text-muted-foreground">{{ t('resellerProductImages.limits') }}</p>
        <section class="mt-4 border-t pt-4" data-testid="product-detail-image">
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0 flex-1">
              <h4 class="text-sm font-semibold">{{ t('resellerProductImages.detailTitle') }}</h4>
              <p class="mt-1 text-xs text-muted-foreground">{{ t('resellerProductImages.detailDescription') }}</p>
              <span class="mt-2 inline-block rounded-full bg-muted px-2 py-1 text-xs text-muted-foreground">{{ t(row.detail_image_source === 'custom' ? 'resellerProductImages.customDetail' : 'resellerProductImages.platformDetail') }}</span>
            </div>
            <div class="flex flex-wrap gap-2">
              <Button v-if="row.custom_detail_image" size="sm" variant="outline" :disabled="disabled(row)" @click="saveDetail(row)">{{ t('resellerProductImages.restoreDetail') }}</Button>
              <Button size="sm" :disabled="disabled(row)" @click="detailInputs.get(row.product.id)?.click()"><ImagePlus class="mr-1 h-4 w-4" />{{ t(row.custom_detail_image ? 'resellerProductImages.replaceDetail' : 'resellerProductImages.uploadDetail') }}</Button>
              <input :ref="el => { if (el) detailInputs.set(row.product.id, el as HTMLInputElement) }" type="file" class="hidden" accept="image/png,image/jpeg,image/gif,image/webp" :aria-label="t('resellerProductImages.uploadDetail')" :disabled="disabled(row)" @change="uploadDetail(row, $event)" />
            </div>
          </div>
          <img v-if="row.custom_detail_image" :src="getImageUrl(row.custom_detail_image)" :alt="t('resellerProductImages.detailAlt')" class="mt-3 max-h-64 max-w-full rounded-lg border object-contain object-left" loading="lazy" />
          <p v-else class="mt-3 text-xs text-muted-foreground">{{ t('resellerProductImages.followPlatformDetail') }}</p>
          <p class="mt-3 text-xs text-muted-foreground">{{ t('resellerProductImages.detailLimits') }}</p>
        </section>
        <div class="mt-2 flex items-center gap-2 text-xs" :class="status(row.product.id).failed ? 'text-destructive' : 'text-success'" role="status" aria-live="polite">
          <LoaderCircle v-if="status(row.product.id).busy" class="h-4 w-4 animate-spin" />
          <span>{{ status(row.product.id).message }}</span>
          <Button v-if="status(row.product.id).needsRefresh" size="sm" variant="outline" :disabled="status(row.product.id).busy" @click="refreshRow(row)">{{ t('resellerProductImages.refresh') }}</Button>
        </div>
      </article>
    </Card>
    <p v-else-if="!loading && !error" class="py-8 text-center text-sm text-muted-foreground">{{ t('resellerProductImages.empty') }}</p>
    <div v-if="pagination.total_page > 1" class="flex items-center justify-center gap-3">
      <Button variant="outline" size="sm" :disabled="saving || loading || pagination.page <= 1" @click="load(pagination.page - 1)">{{ t('orders.prevPage') }}</Button>
      <span class="text-sm text-muted-foreground">{{ t('orders.pageInfo', { page: pagination.page, total: pagination.total_page }) }}</span>
      <Button variant="outline" size="sm" :disabled="saving || loading || pagination.page >= pagination.total_page" @click="load(pagination.page + 1)">{{ t('orders.nextPage') }}</Button>
    </div>
  </div>
</template>
