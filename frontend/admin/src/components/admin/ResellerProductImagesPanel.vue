<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import DOMPurify from 'dompurify'
import { adminAPI } from '@/api/admin'
import type { AdminResellerProductImages } from '@/api/types'
import { useAdminAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Dialog, DialogScrollContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import ListPagination from '@/components/ListPagination.vue'
import ResellerImagePreview from './ResellerImagePreview.vue'
import { formatDate, getLocalizedText } from '@/utils/format'
import { getImageUrl } from '@/utils/image'
import { processHtmlForDisplay } from '@/utils/content'
import { confirmAction } from '@/utils/confirm'
import { notifyError, notifySuccess } from '@/utils/notify'

const props = defineProps<{ resellerId?: string }>()
const { t } = useI18n()
const auth = useAdminAuthStore()
const tr = (key: string) => t(`admin.resellerProductImages.${key}`)
const rows = ref<AdminResellerProductImages[]>([])
const loading = ref(false)
const loadError = ref('')
const busy = ref(false)
const showEditor = ref(false)
const detail = ref<AdminResellerProductImages | null>(null)
const needsRefresh = ref(false)
const editorError = ref('')
const preview = ref('')
const galleryInput = ref<HTMLInputElement | null>(null)
const detailInput = ref<HTMLInputElement | null>(null)
const pagination = ref({ page: 1, page_size: 20, total: 0, total_page: 1 })
const filters = reactive({ resellerId: props.resellerId || '', keyword: '', scope: 'customized', imageSource: '__all__', detailSource: '__all__' })
const canEdit = computed(() => !!detail.value?.can_edit && auth.hasPermission('PUT:/admin/resellers/product-images/:reseller_id/:product_id') && auth.hasPermission('POST:/admin/resellers/product-images/:reseller_id/:product_id/upload'))
const disabled = computed(() => busy.value || needsRefresh.value || !canEdit.value)
const platformContent = computed(() => DOMPurify.sanitize(processHtmlForDisplay(getLocalizedText(detail.value?.platform_detail_content))))
let loadSequence = 0

async function load(page = 1) {
  if (busy.value) return
  const sequence = ++loadSequence
  if (filters.scope === 'all' && !filters.resellerId.trim()) {
    loading.value = false
    loadError.value = tr('chooseReseller'); rows.value = []; return
  }
  loading.value = true
  loadError.value = ''
  try {
    const response = await adminAPI.getResellerProductImages({ page, page_size: pagination.value.page_size, reseller_id: filters.resellerId.trim() || undefined, keyword: filters.keyword.trim() || undefined, scope: filters.scope, image_source: filters.imageSource === '__all__' ? undefined : filters.imageSource, detail_source: filters.detailSource === '__all__' ? undefined : filters.detailSource })
    if (sequence !== loadSequence) return
    rows.value = response.data.data || []
    pagination.value = response.data.pagination || pagination.value
  } catch (err) {
    if (sequence !== loadSequence) return
    rows.value = []
    loadError.value = err instanceof Error ? err.message : tr('loadFailed')
  } finally { if (sequence === loadSequence) loading.value = false }
}

async function openEditor(row: AdminResellerProductImages) {
  if (busy.value) return
  busy.value = true
  try {
    const response = await adminAPI.getResellerProductImagesDetail(row.reseller_id, row.product.id)
    detail.value = response.data.data
    needsRefresh.value = false
    editorError.value = ''
    showEditor.value = true
  } catch (err) { notifyError(err instanceof Error ? err.message : tr('loadFailed')) }
  finally { busy.value = false }
}

async function refreshDetail() {
  if (!detail.value || busy.value) return
  busy.value = true
  try {
    const response = await adminAPI.getResellerProductImagesDetail(detail.value.reseller_id, detail.value.product.id)
    detail.value = response.data.data
    needsRefresh.value = false
    editorError.value = ''
  } catch (err) { editorError.value = err instanceof Error ? err.message : tr('loadFailed') }
  finally { busy.value = false }
}

async function mutate(action: (row: AdminResellerProductImages, operationId: string) => Promise<{ data: { data: AdminResellerProductImages } }>) {
  if (!detail.value || disabled.value) return
  busy.value = true
  editorError.value = ''
  try {
    const operationId = crypto.randomUUID()
    const response = await action(detail.value, operationId)
    if (response.data.data.operation_id !== operationId) throw new Error(tr('saveFailed'))
    detail.value = response.data.data
    notifySuccess(tr('saved'))
  } catch (err) {
    // 网络超时也可能已经写入；先刷新确认结果，不自动重试旧版本。
    needsRefresh.value = true
    editorError.value = err instanceof Error ? err.message : tr('saveFailed')
  } finally {
    busy.value = false
    await load(pagination.value.page)
  }
}

async function updateGallery(images: string[], restore = false) {
  if (disabled.value) return
  if (restore && !await confirmAction({ description: tr('restoreConfirm'), variant: 'destructive' })) return
  await mutate((row, operationId) => adminAPI.updateResellerProductImages(row.reseller_id, row.product.id, { images, revision: row.revision, operation_id: operationId }))
}

function moveImage(index: number, offset: number) {
  if (!detail.value || disabled.value) return
  const images = [...detail.value.custom_images]
  const next = index + offset
  if (next < 0 || next >= images.length) return
  ;[images[index], images[next]] = [images[next]!, images[index]!]
  void updateGallery(images)
}

async function restoreDetail() {
  if (disabled.value || !await confirmAction({ description: tr('restoreConfirm'), variant: 'destructive' })) return
  await mutate((row, operationId) => adminAPI.resetResellerProductDetailImage(row.reseller_id, row.product.id, { revision: row.revision, operation_id: operationId }))
}

async function upload(event: Event, isDetail = false) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || disabled.value) return
  if (!/\.(png|jpe?g|gif|webp)$/i.test(file.name) || !['image/png', 'image/jpeg', 'image/gif', 'image/webp'].includes(file.type) || file.size <= 0 || file.size > 500 * 1024) {
    notifyError(tr('invalidFile')); return
  }
  await mutate((row, operationId) => adminAPI.uploadResellerProductImage(row.reseller_id, row.product.id, file, row.revision, operationId, isDetail))
}

function beforeUnload(event: BeforeUnloadEvent) {
  if (busy.value) { event.preventDefault(); event.returnValue = '' }
}
onMounted(() => { void load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { loadSequence++; window.removeEventListener('beforeunload', beforeUnload) })
</script>

<template>
  <div class="space-y-4" data-testid="admin-product-images">
    <form class="flex flex-wrap items-end gap-3 rounded-xl border bg-card p-4" @submit.prevent="load(1)">
      <div class="space-y-1"><Label>{{ tr('reseller') }}</Label><Input v-model="filters.resellerId" :disabled="busy" :placeholder="tr('reseller')" class="w-40" data-testid="image-reseller-filter" /></div>
      <div class="space-y-1"><Label>{{ tr('product') }}</Label><Input v-model="filters.keyword" :disabled="busy" :placeholder="tr('keyword')" class="w-60" /></div>
      <Select v-model="filters.scope" :disabled="busy"><SelectTrigger class="w-64"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="customized">{{ tr('customized') }}</SelectItem><SelectItem value="all">{{ tr('allProducts') }}</SelectItem></SelectContent></Select>
      <div v-for="source in ['imageSource', 'detailSource'] as const" :key="source" class="space-y-1">
        <Label>{{ tr(source === 'imageSource' ? 'gallery' : 'detail') }}</Label>
        <Select v-model="filters[source]" :disabled="busy"><SelectTrigger class="w-44"><SelectValue /></SelectTrigger><SelectContent><SelectItem value="__all__">{{ tr('sourceAll') }}</SelectItem><SelectItem value="custom">{{ tr('custom') }}</SelectItem><SelectItem value="platform">{{ tr('platform') }}</SelectItem></SelectContent></Select>
      </div>
      <Button type="submit" :disabled="busy || loading">{{ t('admin.common.search') }}</Button>
      <Button type="button" variant="outline" :disabled="busy || loading" @click="load(pagination.page)">{{ t('admin.common.refresh') }}</Button>
    </form>
    <p v-if="loadError" class="text-sm text-destructive" role="alert">{{ loadError }}</p>
    <div class="overflow-x-auto rounded-xl border bg-card">
      <Table class="min-w-[1000px]">
        <TableHeader><TableRow><TableHead>{{ tr('owner') }}</TableHead><TableHead>{{ tr('product') }}</TableHead><TableHead>{{ tr('gallery') }}</TableHead><TableHead>{{ tr('detail') }}</TableHead><TableHead>{{ tr('updated') }}</TableHead><TableHead>{{ t('admin.resellerProductSettings.columns.actions') }}</TableHead></TableRow></TableHeader>
        <TableBody>
          <TableRow v-for="row in rows" :key="`${row.reseller_id}:${row.product.id}`">
            <TableCell><div>#{{ row.reseller_id }}</div><div class="text-xs text-muted-foreground">{{ row.owner_label || `#${row.user_id}` }}</div></TableCell>
            <TableCell class="max-w-80"><div class="break-words font-medium">{{ getLocalizedText(row.product.title) }}</div><div class="text-xs text-muted-foreground">#{{ row.product.id }}</div></TableCell>
            <TableCell><div class="flex max-w-56 flex-wrap gap-1"><button v-for="image in row.effective_images" :key="image" type="button" :aria-label="tr('preview')" @click="preview = image"><img :src="getImageUrl(image)" :alt="tr('gallery')" class="h-12 w-12 rounded border object-contain" /></button></div><p class="mt-1 text-xs text-muted-foreground">{{ tr(row.image_source) }}</p></TableCell>
            <TableCell><button v-if="row.custom_detail_image" type="button" :aria-label="tr('preview')" @click="preview = row.custom_detail_image"><img :src="getImageUrl(row.custom_detail_image)" :alt="tr('detail')" class="h-12 w-12 rounded border object-contain" /></button><p class="text-xs text-muted-foreground">{{ tr(row.detail_image_source) }}</p></TableCell>
            <TableCell>{{ row.updated_at ? formatDate(row.updated_at) : '-' }}</TableCell>
            <TableCell><Button variant="outline" size="sm" :disabled="busy" @click="openEditor(row)">{{ tr('manage') }}</Button></TableCell>
          </TableRow>
          <TableRow v-if="!rows.length"><TableCell :colspan="6" class="py-10 text-center">{{ loading ? t('admin.common.loading') : tr('empty') }}</TableCell></TableRow>
        </TableBody>
      </Table>
    </div>
    <ListPagination :page="pagination.page" :total-page="pagination.total_page" :total="pagination.total" :page-size="pagination.page_size" @change-page="load" />
    <Dialog :open="showEditor" @update:open="value => { if (!busy) showEditor = value }">
      <DialogScrollContent class="max-w-6xl" @interact-outside="event => event.preventDefault()" @escape-key-down="event => { if (busy) event.preventDefault() }">
        <DialogHeader><DialogTitle>{{ tr('manage') }} · #{{ detail?.reseller_id }} · {{ getLocalizedText(detail?.product.title) }}</DialogTitle></DialogHeader>
        <template v-if="detail">
          <p class="text-xs text-muted-foreground">{{ tr('limits') }}</p>
          <p v-if="!canEdit" class="text-sm text-muted-foreground">{{ tr('readonly') }}</p>
          <p v-if="editorError" class="text-sm text-destructive" role="alert">{{ editorError }}</p>
          <Button variant="outline" size="sm" class="justify-self-start" :disabled="busy" @click="refreshDetail">{{ tr('refresh') }}</Button>
          <div class="grid gap-4 md:grid-cols-2">
            <section class="space-y-3 rounded-lg border p-4"><h3 class="font-medium">{{ tr('platformGallery') }}</h3><div class="flex flex-wrap gap-2"><button v-for="image in detail.platform_images" :key="image" type="button" :aria-label="tr('preview')" @click="preview = image"><img :src="getImageUrl(image)" :alt="tr('gallery')" class="h-24 w-24 rounded border object-contain" /></button></div></section>
            <section class="space-y-3 rounded-lg border p-4"><h3 class="font-medium">{{ tr('effectiveGallery') }} · {{ tr(detail.image_source) }}</h3>
              <div class="flex flex-wrap gap-2"><Button size="sm" :disabled="disabled || detail.custom_images.length >= 5" @click="galleryInput?.click()">{{ tr('upload') }}</Button><Button size="sm" variant="outline" :disabled="disabled || !detail.custom_images.length" @click="updateGallery([], true)">{{ tr('restoreGallery') }}</Button></div>
              <input ref="galleryInput" type="file" class="hidden" accept="image/png,image/jpeg,image/gif,image/webp" :disabled="disabled" :aria-label="tr('upload')" @change="upload($event)" />
              <div class="flex flex-wrap gap-3"><div v-for="(image, index) in detail.effective_images" :key="image" class="w-32"><button type="button" :aria-label="tr('preview')" @click="preview = image"><img :src="getImageUrl(image)" :alt="tr('gallery')" class="h-28 w-32 rounded border object-contain" /></button><div v-if="detail.image_source === 'custom'" class="mt-1 flex flex-wrap gap-1"><Button size="sm" variant="outline" :disabled="disabled || index === 0" :aria-label="tr('previous')" @click="moveImage(index, -1)">←</Button><Button size="sm" variant="outline" :disabled="disabled || index === detail.custom_images.length - 1" :aria-label="tr('next')" @click="moveImage(index, 1)">→</Button><Button size="sm" variant="outline" :disabled="disabled" @click="updateGallery(detail.custom_images.filter((_, i) => i !== index))">{{ tr('remove') }}</Button></div></div></div>
            </section>
            <section class="space-y-3 rounded-lg border p-4"><h3 class="font-medium">{{ tr('platformDetail') }}</h3><div class="prose max-h-96 max-w-none overflow-auto dark:prose-invert [&_img]:max-w-full" v-html="platformContent" /></section>
            <section class="space-y-3 rounded-lg border p-4"><h3 class="font-medium">{{ tr('effectiveDetail') }} · {{ tr(detail.detail_image_source) }}</h3><div class="flex flex-wrap gap-2"><Button size="sm" :disabled="disabled" @click="detailInput?.click()">{{ tr('replaceDetail') }}</Button><Button size="sm" variant="outline" :disabled="disabled || !detail.custom_detail_image" @click="restoreDetail">{{ tr('restoreDetail') }}</Button></div>
              <input ref="detailInput" type="file" class="hidden" accept="image/png,image/jpeg,image/gif,image/webp" :disabled="disabled" :aria-label="tr('replaceDetail')" @change="upload($event, true)" />
              <button v-if="detail.custom_detail_image" type="button" :aria-label="tr('preview')" @click="preview = detail.custom_detail_image"><img :src="getImageUrl(detail.custom_detail_image)" :alt="tr('detail')" class="max-h-96 max-w-full rounded border object-contain" /></button><div v-else class="prose max-h-96 max-w-none overflow-auto dark:prose-invert [&_img]:max-w-full" v-html="platformContent" />
            </section>
          </div>
        </template>
      </DialogScrollContent>
    </Dialog>
    <ResellerImagePreview v-model="preview" />
  </div>
</template>
