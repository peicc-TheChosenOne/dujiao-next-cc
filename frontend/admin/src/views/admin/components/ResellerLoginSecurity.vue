<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { AdminResellerProfile } from '@/api/types'
import { useAdminAuthStore } from '@/stores/auth'
import { Button } from '@/components/ui/button'
import { Dialog, DialogHeader, DialogScrollContent, DialogTitle } from '@/components/ui/dialog'
import { Textarea } from '@/components/ui/textarea'
import { formatDate } from '@/utils/format'
import { notifyError, notifySuccess } from '@/utils/notify'

const props = defineProps<{ profile: AdminResellerProfile }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const auth = useAdminAuthStore()
const security = computed(() => props.profile.login_security)
const open = ref(false)
const action = ref<'require' | 'pause' | 'reset'>('require')
const reason = ref('')
const identityVerified = ref(false)
const busy = ref(false)
const canChange = computed(() => auth.hasPermission('PUT:/admin/resellers/profiles/:id/login-security'))
const canReset = computed(() => auth.hasPermission('POST:/admin/resellers/profiles/:id/login-security/reset'))
const managed = computed(() => ['active', 'disabled'].includes(props.profile.status))
const begin = (value: 'require' | 'pause' | 'reset') => {
  action.value = value; reason.value = ''; identityVerified.value = false; open.value = true
}
const submit = async () => {
  if (busy.value || !reason.value.trim() || (action.value === 'reset' && !identityVerified.value)) return
  busy.value = true
  try {
    if (action.value === 'reset') await adminAPI.resetResellerLoginSecurity(props.profile.id, { reason: reason.value.trim(), identity_verified: true })
    else await adminAPI.updateResellerLoginSecurity(props.profile.id, { required: action.value === 'require', reason: reason.value.trim() })
    open.value = false
    notifySuccess(t(action.value === 'reset' ? 'admin.loginSecurity.resetSuccess' : 'admin.loginSecurity.saved'))
    emit('updated')
  } catch (err: any) {
    notifyError(err?.message || t('admin.loginSecurity.failed'))
    emit('updated')
  } finally { busy.value = false }
}
</script>

<template>
  <section class="space-y-5 rounded-xl border border-border bg-card p-5">
    <div><h2 class="font-semibold">{{ t('admin.loginSecurity.title') }}</h2><p class="mt-2 text-sm text-muted-foreground">{{ t('admin.loginSecurity.description') }}</p></div>
    <p v-if="!managed" class="text-sm text-muted-foreground">{{ t('admin.loginSecurity.notApproved') }}</p>
    <dl class="grid gap-4 text-sm sm:grid-cols-2">
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.policy') }}</dt><dd class="mt-1 font-medium">{{ t(security?.required === false ? 'admin.loginSecurity.paused' : 'admin.loginSecurity.required') }}</dd></div>
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.binding') }}</dt><dd class="mt-1 font-medium">{{ t(`admin.loginSecurity.${security?.binding_state || 'unbound'}`) }}</dd></div>
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.boundAt') }}</dt><dd class="mt-1">{{ security?.bound_at ? formatDate(security.bound_at) : '-' }}</dd></div>
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.changedAt') }}</dt><dd class="mt-1">{{ security?.updated_at ? formatDate(security.updated_at) : '-' }}</dd></div>
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.operator') }}</dt><dd class="mt-1">{{ security?.admin_id ? `#${security.admin_id}` : '-' }}</dd></div>
      <div><dt class="text-muted-foreground">{{ t('admin.loginSecurity.lastAction') }}</dt><dd class="mt-1">{{ security?.action ? t(`admin.loginSecurity.action.${security.action}`) : '-' }}</dd></div>
      <div class="sm:col-span-2"><dt class="text-muted-foreground">{{ t('admin.loginSecurity.lastReason') }}</dt><dd class="mt-1 whitespace-pre-wrap break-words">{{ security?.reason || '-' }}</dd></div>
    </dl>
    <div v-if="managed" class="flex flex-wrap gap-3 border-t pt-4">
      <button v-if="canChange" type="button" role="switch" :aria-checked="security?.required !== false" :aria-label="t('admin.loginSecurity.policy')" class="flex items-center gap-3 rounded-lg border px-4 py-2 text-sm" :disabled="busy" @click="begin(security?.required === false ? 'require' : 'pause')">
        <span class="relative inline-flex h-6 w-11 rounded-full transition-colors" :class="security?.required === false ? 'bg-muted' : 'bg-primary'"><span class="absolute top-1 h-4 w-4 rounded-full bg-white transition-transform" :class="security?.required === false ? 'translate-x-1' : 'translate-x-6'"></span></span>{{ t('admin.loginSecurity.policy') }}
      </button>
      <Button v-if="canReset" type="button" variant="destructive" :disabled="busy" @click="begin('reset')">{{ t('admin.loginSecurity.reset') }}</Button>
    </div>
    <Dialog v-model:open="open">
      <DialogScrollContent class="sm:max-w-lg">
        <DialogHeader><DialogTitle>{{ t(`admin.loginSecurity.action.${action}`) }}</DialogTitle></DialogHeader>
        <p class="text-sm text-muted-foreground">{{ t(`admin.loginSecurity.confirm.${action}`) }}</p>
        <form class="space-y-4" @submit.prevent="submit">
          <label class="block space-y-2 text-sm"><span>{{ t('admin.loginSecurity.reason') }}</span><Textarea v-model="reason" required maxlength="500" :disabled="busy" /></label>
          <label v-if="action === 'reset'" class="flex items-start gap-2 text-sm"><input v-model="identityVerified" type="checkbox" :disabled="busy" class="mt-1" />{{ t('admin.loginSecurity.identityVerified') }}</label>
          <Button type="submit" class="w-full" :disabled="busy || !reason.trim() || (action === 'reset' && !identityVerified)">{{ t(busy ? 'admin.common.submitting' : 'admin.common.confirm') }}</Button>
        </form>
      </DialogScrollContent>
    </Dialog>
  </section>
</template>
