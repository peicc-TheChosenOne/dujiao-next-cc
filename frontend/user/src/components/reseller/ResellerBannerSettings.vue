<template>
  <div class="overflow-hidden rounded-2xl border bg-card">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b bg-muted/20 p-4 sm:p-5">
      <div>
        <h3 class="font-semibold">{{ t(`${key}.title`) }}</h3>
        <p class="mt-1 text-xs text-muted-foreground">{{ t(`${key}.description`) }}</p>
      </div>
      <Button type="button" variant="outline" size="sm" @click="update({ mode: 'inherit' })">
        <RotateCcw class="mr-1.5 h-3.5 w-3.5" />
        {{ t(`${key}.reset`) }}
      </Button>
    </div>

    <div class="space-y-5 p-4 sm:p-5">
      <div class="grid gap-3 sm:grid-cols-3">
        <button
          v-for="mode in modes"
          :key="mode"
          type="button"
          class="rounded-xl border p-4 text-left transition-colors"
          :class="modelValue.mode === mode ? 'border-primary bg-primary/5 ring-1 ring-primary' : 'bg-muted/30 hover:bg-muted/60'"
          :aria-pressed="modelValue.mode === mode"
          @click="update({ mode })"
        >
          <span class="flex items-center justify-between gap-2 text-sm font-semibold">
            {{ t(`${key}.modes.${mode}`) }}
            <span class="h-2 w-2 shrink-0 rounded-full" :class="modelValue.mode === mode ? 'bg-primary' : 'bg-muted-foreground/30'"></span>
          </span>
          <span class="mt-2 block text-xs leading-relaxed text-muted-foreground">{{ t(`${key}.modeHints.${mode}`) }}</span>
        </button>
      </div>

      <template v-if="modelValue.mode === 'custom'">
        <div class="grid gap-5 sm:grid-cols-2">
          <ResellerImageField :model-value="modelValue.image" :label="t(`${key}.desktopImage`)" :hint="t(`${key}.desktopHint`)" :wide="true" @update:model-value="update({ image: $event })" />
          <ResellerImageField :model-value="modelValue.mobile_image" :label="t(`${key}.mobileImage`)" :hint="t(`${key}.mobileHint`)" :wide="true" @update:model-value="update({ mobile_image: $event })" />
        </div>

        <div class="space-y-4 rounded-xl border bg-muted/10 p-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h4 class="text-sm font-semibold">{{ t(`${key}.text`) }}</h4>
              <p class="mt-1 text-xs text-muted-foreground">{{ t(`${key}.textHint`) }}</p>
            </div>
            <ResellerLocaleTabs v-model="activeLocale" :labels="localeLabels" />
          </div>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="space-y-2 text-sm">
              <span>{{ t(`${key}.mainTitle`) }}</span>
              <Input :model-value="modelValue.title[activeLocale]" :maxlength="120" @update:model-value="update({ title: { ...modelValue.title, [activeLocale]: String($event) } })" />
            </label>
            <label class="space-y-2 text-sm">
              <span>{{ t(`${key}.subtitle`) }}</span>
              <Input :model-value="modelValue.subtitle[activeLocale]" :maxlength="300" @update:model-value="update({ subtitle: { ...modelValue.subtitle, [activeLocale]: String($event) } })" />
            </label>
          </div>
        </div>

        <div class="grid gap-4 sm:grid-cols-2">
          <div class="space-y-2">
            <label class="text-sm">{{ t(`${key}.linkType`) }}</label>
            <Select :model-value="modelValue.link_type" @update:model-value="setLinkType">
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem v-for="type in linkTypes" :key="type" :value="type">{{ t(`${key}.linkTypes.${type}`) }}</SelectItem>
              </SelectContent>
            </Select>
          </div>
          <label v-if="modelValue.link_type !== 'none'" class="space-y-2 text-sm">
            <span>{{ t(`${key}.linkValue`) }}</span>
            <Input :model-value="modelValue.link_value" :placeholder="modelValue.link_type === 'internal' ? '/products' : 'https://example.com'" @update:model-value="update({ link_value: String($event) })" />
          </label>
        </div>
        <label v-if="modelValue.link_type !== 'none'" class="flex items-center gap-2 text-sm">
          <Switch :model-value="modelValue.open_in_new_tab" @update:model-value="update({ open_in_new_tab: $event })" />
          {{ t(`${key}.openInNewTab`) }}
        </label>

        <div class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <span class="text-sm font-medium">{{ t(`${key}.preview`) }}</span>
            <div class="flex gap-1">
              <Button v-for="device in devices" :key="device" type="button" size="sm" :variant="previewDevice === device ? 'secondary' : 'ghost'" @click="previewDevice = device">{{ t(`${key}.devices.${device}`) }}</Button>
            </div>
          </div>
          <div class="relative mx-auto flex min-h-[220px] w-full items-end overflow-hidden rounded-xl bg-gradient-to-br from-slate-950 via-slate-900 to-emerald-950" :class="previewDevice === 'mobile' ? 'max-w-[360px] min-h-[280px]' : ''">
            <img v-if="previewImage" :src="previewImage" :alt="previewTitle" class="absolute inset-0 h-full w-full object-cover" />
            <div v-if="previewImage && (previewTitle || previewSubtitle)" class="absolute inset-0 bg-black/45"></div>
            <div class="relative space-y-2 p-6 text-white">
              <h4 v-if="previewTitle" class="text-2xl font-bold">{{ previewTitle }}</h4>
              <p v-if="previewSubtitle" class="text-sm text-white/85">{{ previewSubtitle }}</p>
              <span v-if="!previewImage && !previewTitle && !previewSubtitle" class="text-sm text-white/60">{{ t(`${key}.emptyPreview`) }}</span>
            </div>
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RotateCcw } from 'lucide-vue-next'
import type { ResellerBannerConfig } from '../../api/types'
import { getImageUrl } from '../../utils/image'
import { getLocalizedText, type ResellerLocale } from '../../utils/resellerSiteConfig'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import ResellerImageField from './ResellerImageField.vue'
import ResellerLocaleTabs from './ResellerLocaleTabs.vue'

const props = defineProps<{ modelValue: ResellerBannerConfig }>()
const emit = defineEmits<{ 'update:modelValue': [value: ResellerBannerConfig] }>()
const { t } = useI18n()
const key = 'personalCenter.reseller.siteConfig.banner'
const modes = ['inherit', 'custom', 'hidden'] as const
const linkTypes = ['none', 'internal', 'external'] as const
const devices = ['desktop', 'mobile'] as const
const activeLocale = ref<ResellerLocale>('zh-CN')
const previewDevice = ref<'desktop' | 'mobile'>('desktop')
const localeLabels = computed(() => ({ 'zh-CN': t(`${key}.locales.simplified`), 'zh-TW': t(`${key}.locales.traditional`), 'en-US': t(`${key}.locales.english`) }))
const previewImage = computed(() => getImageUrl(previewDevice.value === 'mobile' ? props.modelValue.mobile_image || props.modelValue.image : props.modelValue.image))
const previewTitle = computed(() => getLocalizedText(props.modelValue.title, activeLocale.value))
const previewSubtitle = computed(() => getLocalizedText(props.modelValue.subtitle, activeLocale.value))
const update = (value: Partial<ResellerBannerConfig>) => emit('update:modelValue', { ...props.modelValue, ...value })
const setLinkType = (value: unknown) => {
  const link_type = value === 'internal' || value === 'external' ? value : 'none'
  update({ link_type, ...(link_type === 'none' ? { link_value: '', open_in_new_tab: false } : {}) })
}
</script>
