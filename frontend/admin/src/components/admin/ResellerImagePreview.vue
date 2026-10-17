<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Dialog, DialogScrollContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { getImageUrl } from '@/utils/image'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()
const { t } = useI18n()
</script>

<template>
  <Dialog :open="!!modelValue" @update:open="value => { if (!value) emit('update:modelValue', '') }">
    <DialogScrollContent class="max-w-5xl">
      <DialogHeader><DialogTitle>{{ t('admin.resellerProductImages.preview') }}</DialogTitle></DialogHeader>
      <img v-if="modelValue" :src="getImageUrl(modelValue)" :alt="t('admin.resellerProductImages.preview')" class="mx-auto h-auto max-w-full" />
    </DialogScrollContent>
  </Dialog>
</template>
