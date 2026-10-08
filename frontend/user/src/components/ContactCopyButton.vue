<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Copy, Mail } from 'lucide-vue-next'
import { copyText } from '../utils/clipboard'

const props = defineProps<{
  kind: 'wechat' | 'qq' | 'email'
  value: string
}>()

const { t } = useI18n()
const label = computed(() => props.kind === 'qq' ? 'QQ' : t(`footer.${props.kind}`))
const copied = ref(false)
const copyFailed = ref(false)
let resetTimer: ReturnType<typeof setTimeout> | undefined

const handleCopy = async () => {
  if (resetTimer) clearTimeout(resetTimer)
  copied.value = false
  copyFailed.value = false
  try {
    await copyText(props.value)
    copied.value = true
    resetTimer = setTimeout(() => {
      copied.value = false
      resetTimer = undefined
    }, 1800)
  } catch {
    copyFailed.value = true
  }
}

onUnmounted(() => {
  if (resetTimer) clearTimeout(resetTimer)
})
</script>

<template>
  <div class="min-w-0">
    <button
      type="button"
      :aria-label="`${label}: ${value}`"
      :title="copied ? t('footer.copySuccess') : t('footer.copyContact', { name: label })"
      class="group grid w-full min-w-0 grid-cols-[20px_minmax(0,1fr)_16px] items-center gap-x-2.5 rounded-sm py-1.5 text-left text-sm text-muted-foreground transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 motion-reduce:transition-none"
      @click="handleCopy"
    >
      <svg v-if="kind === 'wechat'" viewBox="0 0 24 24" fill="currentColor" class="h-5 w-5 text-[#07c160]" aria-hidden="true">
        <path d="M8.691 2.188C3.891 2.188 0 5.476 0 9.53c0 2.212 1.17 4.203 3.002 5.55a.59.59 0 0 1 .213.665l-.39 1.48c-.019.07-.048.141-.048.213 0 .163.13.295.29.295a.326.326 0 0 0 .167-.054l1.903-1.114a.864.864 0 0 1 .717-.098 10.16 10.16 0 0 0 2.837.403c.276 0 .543-.027.811-.05-.857-2.578.157-4.972 1.932-6.446 1.703-1.415 3.882-1.98 5.853-1.838-.576-3.583-4.196-6.348-8.596-6.348zM5.785 5.991c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178A1.17 1.17 0 0 1 4.623 7.17c0-.651.52-1.18 1.162-1.18zm5.813 0c.642 0 1.162.529 1.162 1.18a1.17 1.17 0 0 1-1.162 1.178 1.17 1.17 0 0 1-1.162-1.178c0-.651.52-1.18 1.162-1.18zm5.34 2.867c-1.797-.052-3.746.512-5.28 1.786-1.72 1.428-2.687 3.72-1.78 6.22.942 2.453 3.666 4.229 6.884 4.229.826 0 1.622-.12 2.361-.336a.722.722 0 0 1 .598.082l1.584.926a.272.272 0 0 0 .14.047c.134 0 .24-.111.24-.247 0-.06-.023-.12-.038-.177l-.327-1.233a.582.582 0 0 1-.023-.156.49.49 0 0 1 .201-.398C23.024 18.48 24 16.82 24 14.98c0-3.21-2.931-5.837-6.656-6.088V8.89c-.135-.01-.27-.027-.407-.03zm-2.53 3.274c.535 0 .969.44.969.982a.976.976 0 0 1-.969.983.976.976 0 0 1-.969-.983c0-.542.434-.982.97-.982zm4.844 0c.535 0 .969.44.969.982a.976.976 0 0 1-.969.983.976.976 0 0 1-.969-.983c0-.542.434-.982.969-.982z" />
      </svg>
      <svg v-else-if="kind === 'qq'" viewBox="0 0 24 24" fill="currentColor" class="h-5 w-5 text-[#12b7f5]" aria-hidden="true">
        <path d="M21.395 15.035a40 40 0 0 0-.803-2.264l-1.079-2.695c.001-.032.014-.562.014-.836C19.526 4.632 17.351 0 12 0S4.474 4.632 4.474 9.241c0 .274.013.804.014.836l-1.08 2.695a39 39 0 0 0-.802 2.264c-1.021 3.283-.69 4.643-.438 4.673.54.065 2.103-2.472 2.103-2.472 0 1.469.756 3.387 2.394 4.771-.612.188-1.363.479-1.845.835-.434.32-.379.646-.301.778.343.578 5.883.369 7.482.189 1.6.18 7.14.389 7.483-.189.078-.132.132-.458-.301-.778-.483-.356-1.233-.646-1.846-.836 1.637-1.384 2.393-3.302 2.393-4.771 0 0 1.563 2.537 2.103 2.472.251-.03.581-1.39-.438-4.673" />
      </svg>
      <span v-else class="flex h-5 w-5 items-center justify-center rounded-full bg-[#4da6ff]">
        <Mail class="h-3 w-3 text-white" aria-hidden="true" />
      </span>
      <span class="min-w-0 break-all leading-6">
        <span>{{ label }}: </span><span class="font-semibold">{{ value }}</span>
      </span>
      <Check v-if="copied" class="h-4 w-4 text-emerald-500" aria-hidden="true" />
      <Copy v-else class="h-4 w-4 opacity-60 group-hover:opacity-100" aria-hidden="true" />
    </button>
    <span v-if="copied" role="status" class="sr-only">{{ t('footer.copySuccess') }}</span>
    <p v-if="copyFailed" role="alert" class="mt-1 text-xs text-destructive">{{ t('footer.copyFailed') }}</p>
  </div>
</template>
