<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import QRCode from 'qrcode'
import { userAuthAPI } from '../../api/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'

const props = defineProps<{ challengeToken: string; remainingSeconds: number }>()
const emit = defineEmits<{ cancel: []; bound: []; complete: [data: any] }>()
const { t } = useI18n()
const busy = ref(false)
const error = ref('')
const secret = ref('')
const qr = ref('')
const code = ref('')
const result = ref<any>(null)
const copied = ref(false)
const saved = ref(false)

const setup = async () => {
  busy.value = true
  error.value = ''
  try {
    const response = await userAuthAPI.setupLogin2FA({ challenge_token: props.challengeToken })
    secret.value = response.data.data.secret
    qr.value = await QRCode.toDataURL(response.data.data.otpauth_url, { width: 240, margin: 1 })
  } catch (err: any) {
    error.value = err?.message || t('personalCenter.security.twofa.setupFailed')
  } finally { busy.value = false }
}

const submit = async () => {
  if (!/^\d{6}$/.test(code.value.trim()) || busy.value) return
  busy.value = true
  error.value = ''
  try {
    const response = await userAuthAPI.enableLogin2FA({ challenge_token: props.challengeToken, code: code.value.trim() })
    result.value = response.data.data
    secret.value = ''
    qr.value = ''
    code.value = ''
    emit('bound')
  } catch (err: any) {
    error.value = err?.message || t('personalCenter.security.twofa.enableFailed')
    code.value = ''
  } finally { busy.value = false }
}

const copyCodes = async () => {
  try { await navigator.clipboard.writeText(result.value.recovery_codes.join('\n')); copied.value = true }
  catch { error.value = t('auth.login.enrollment.copyFailed') }
}
const downloadCodes = () => {
  const url = URL.createObjectURL(new Blob([result.value.recovery_codes.join('\n')], { type: 'text/plain;charset=utf-8' }))
  const link = document.createElement('a'); link.href = url; link.download = 'recovery-codes.txt'; link.click(); URL.revokeObjectURL(url)
}
onMounted(setup)
</script>

<template>
  <div class="space-y-5">
    <p v-if="error" role="alert" class="rounded-lg border border-destructive/30 p-3 text-sm text-destructive">{{ error }}</p>
    <template v-if="!result">
      <p class="text-sm text-muted-foreground">{{ t('auth.login.enrollment.hint') }}</p>
      <p role="status" class="text-center text-xs text-muted-foreground">{{ t('auth.login.totp.countdown', { seconds: remainingSeconds }) }}</p>
      <p v-if="busy && !secret" role="status" class="text-sm">{{ t('personalCenter.security.twofa.startingSetup') }}</p>
      <template v-if="secret">
        <img :src="qr" :alt="t('auth.login.enrollment.qrAlt')" class="mx-auto h-48 w-48 rounded-xl bg-white p-2" />
        <div class="rounded-xl border p-3">
          <p class="text-xs text-muted-foreground">{{ t('personalCenter.security.twofa.secret') }}</p>
          <code class="mt-2 block select-all break-all text-center text-sm">{{ secret }}</code>
        </div>
        <form class="space-y-4" @submit.prevent="submit">
          <label class="block text-sm" for="enrollment-code">{{ t('personalCenter.security.twofa.setupStep2') }}</label>
          <Input id="enrollment-code" v-model="code" autocomplete="one-time-code" inputmode="numeric" maxlength="6" class="h-12 text-center tracking-[0.4em]" :placeholder="t('personalCenter.security.twofa.codePlaceholder')" />
          <Button type="submit" class="w-full" :disabled="busy || !/^\d{6}$/.test(code.trim())">{{ t('auth.login.enrollment.confirmBind') }}</Button>
        </form>
      </template>
      <Button type="button" variant="ghost" class="w-full" :disabled="busy" @click="emit('cancel')">{{ t('auth.login.totp.cancel') }}</Button>
    </template>
    <template v-else>
      <h2 class="font-bold">{{ t('personalCenter.security.twofa.recoveryTitle') }}</h2>
      <p class="text-sm text-muted-foreground">{{ t('auth.login.enrollment.recoveryHint') }}</p>
      <div class="grid grid-cols-2 gap-2"><code v-for="value in result.recovery_codes" :key="value" class="rounded-lg border p-2 text-center text-sm">{{ value }}</code></div>
      <div class="flex gap-2">
        <Button type="button" variant="outline" class="flex-1" @click="copyCodes">{{ t(copied ? 'personalCenter.security.twofa.copied' : 'personalCenter.security.twofa.copy') }}</Button>
        <Button type="button" variant="outline" class="flex-1" @click="downloadCodes">{{ t('auth.login.enrollment.download') }}</Button>
      </div>
      <label class="flex items-start gap-2 text-sm"><input v-model="saved" type="checkbox" class="mt-1" />{{ t('auth.login.enrollment.saved') }}</label>
      <Button type="button" class="w-full" :disabled="!saved" @click="emit('complete', result)">{{ t('auth.login.enrollment.continue') }}</Button>
    </template>
  </div>
</template>
