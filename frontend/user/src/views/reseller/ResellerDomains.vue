<template>
  <div class="space-y-5">
    <ResellerSectionHeader :title="t('resellerConsole.domains.title')" :description="t('resellerConsole.domains.description')">
      <template #actions>
        <Button type="button" variant="outline" size="sm" @click="load()">
          <RotateCw class="h-4 w-4" />{{ t('orders.filters.refresh') }}
        </Button>
      </template>
    </ResellerSectionHeader>

    <Alert v-if="alert" :variant="alert.level === 'error' ? 'destructive' : 'default'" :class="alert.level === 'success' ? 'border-success/40 text-success' : ''">
      <AlertDescription>{{ alert.message }}</AlertDescription>
    </Alert>
    <ResellerPageState v-if="loading" loading :title="t('resellerConsole.common.loading')" />

    <template v-else>
      <section class="rounded-lg border border-border bg-card p-5 shadow-sm sm:p-6" data-testid="domain-overview">
        <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
          <div class="flex min-w-0 items-start gap-3">
            <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary"><Globe2 class="h-5 w-5" /></span>
            <div class="min-w-0">
              <h2 class="text-sm font-semibold text-muted-foreground">{{ t('resellerConsole.domains.primaryTitle') }}</h2>
              <p class="mt-1 break-all font-mono text-lg font-bold text-foreground">{{ primaryDomain?.domain || t('resellerConsole.domains.noPrimaryDomain') }}</p>
              <p class="mt-2 text-sm text-muted-foreground">{{ t('resellerConsole.domains.groupSummary', { count: domainGroups.length, active: customDomains.filter(isActiveVerifiedDomain).length }) }}</p>
            </div>
          </div>
          <div v-if="primaryDomain" class="flex shrink-0 flex-wrap items-center gap-2">
            <ResellerCopyButton :value="`https://${primaryDomain.domain}`" :label="t('resellerConsole.domains.copyAddress')" />
            <Button as-child size="sm"><a :href="`https://${primaryDomain.domain}`" target="_blank" rel="noopener noreferrer"><ExternalLink class="h-4 w-4" />{{ t('resellerConsole.domains.visitStore') }}</a></Button>
          </div>
        </div>
      </section>

      <details class="group rounded-lg border border-border bg-card shadow-sm" :open="customDomains.length === 0" data-testid="add-domain">
        <summary class="flex cursor-pointer list-none items-center justify-between gap-3 rounded-lg px-5 py-4 focus-visible:outline-2 focus-visible:outline-primary sm:px-6 [&::-webkit-details-marker]:hidden">
          <span class="flex min-w-0 items-center gap-3"><Plus class="h-5 w-5 shrink-0 text-primary" /><span class="text-sm font-bold">{{ t('resellerConsole.domains.addDomain') }}</span></span>
          <ChevronDown class="h-4 w-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-180" />
        </summary>
        <div class="space-y-4 border-t border-border px-5 py-5 sm:px-6">
          <p class="text-sm text-muted-foreground">{{ t(snapshot?.domain_binding_enabled ? 'resellerConsole.domains.bindingSummary' : 'resellerConsole.domains.verifyDesc') }}</p>
          <form v-if="canSubmitDomain" class="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto]" @submit.prevent="submitDomain">
            <Input v-model.trim="domainForm.domain" type="text" :disabled="submitting" :aria-label="t('resellerConsole.domains.tableDomain')" :placeholder="t('personalCenter.reseller.customDomainPlaceholder')" />
            <Button type="submit" class="sm:min-w-32" :disabled="submitting || !domainForm.domain.trim()">{{ submitting ? t('personalCenter.reseller.submittingDomain') : t('personalCenter.reseller.submitDomain') }}</Button>
          </form>
          <p v-else class="rounded-lg border border-dashed border-border p-4 text-sm text-muted-foreground">{{ t('resellerConsole.domains.submitDisabledDesc') }}</p>
          <div v-if="snapshot?.domain_binding_enabled" class="space-y-3">
            <h3 class="text-sm font-semibold">{{ t('resellerConsole.domains.dnsRecordsTitle') }}</h3>
            <div class="overflow-x-auto rounded-lg border border-border">
              <table class="w-full text-left text-sm">
                <thead class="bg-muted/30 text-xs text-muted-foreground"><tr><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordName') }}</th><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordType') }}</th><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordTarget') }}</th></tr></thead>
                <tbody class="divide-y divide-border">
                  <tr v-for="name in ['@', 'www']" :key="name">
                    <td class="px-3 py-2 font-mono">{{ name }}</td><td class="px-3 py-2 font-mono">CNAME</td>
                    <td class="px-3 py-2"><div class="flex flex-wrap items-center gap-2"><span class="break-all font-mono">{{ snapshot.domain_cname_target }}</span><ResellerCopyButton v-if="snapshot.domain_cname_target" :value="snapshot.domain_cname_target" :show-label="false" :label="t('resellerConsole.domains.copyTarget')" /></div></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <details class="text-sm text-muted-foreground">
              <summary class="cursor-pointer text-primary">{{ t('resellerConsole.domains.dnsHelp') }}</summary>
              <div class="mt-3 space-y-2 leading-relaxed"><p>{{ t('resellerConsole.domains.apexHelp') }}</p><p>{{ t('resellerConsole.domains.cloudflareHelp') }}</p></div>
            </details>
          </div>
        </div>
      </details>

      <section class="space-y-3" data-testid="custom-domain-groups">
        <div class="flex flex-wrap items-center justify-between gap-2 px-1">
          <h2 class="text-sm font-bold">{{ t('resellerConsole.domains.customDomains') }}</h2>
          <span class="text-xs text-muted-foreground">{{ t('resellerConsole.domains.groupCount', { count: domainGroups.length }) }}</span>
        </div>
        <article v-for="group in domainGroups" :key="group.rootDomain" class="min-w-0 overflow-hidden rounded-lg border border-border bg-card shadow-sm" :data-domain-group="group.rootDomain">
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-muted/20 px-5 py-4 sm:px-6">
            <div class="flex min-w-0 flex-wrap items-center gap-2">
              <h3 class="break-all font-mono text-base font-bold">{{ group.rootDomain }}</h3>
              <ResellerStatusBadge v-if="group.domains.some(item => item.is_primary && isActiveVerifiedDomain(item))" :label="t('personalCenter.reseller.primaryDomain')" tone="accent" />
            </div>
            <span class="text-xs text-muted-foreground">{{ t('resellerConsole.domains.groupAvailability', { active: group.domains.filter(isActiveVerifiedDomain).length, total: group.domains.length }) }}</span>
          </div>
          <div class="divide-y divide-border">
            <div v-for="item in group.domains" :key="item.id" class="flex flex-col gap-3 px-5 py-4 sm:px-6 lg:flex-row lg:items-center lg:justify-between" :data-domain-id="item.id">
              <div class="min-w-0 space-y-2">
                <div class="flex min-w-0 flex-wrap items-center gap-2">
                  <span class="break-all font-mono text-sm font-semibold">{{ item.domain }}</span>
                  <ResellerStatusBadge :label="progressLabel(item)" :tone="progressTone(item)" dot />
                </div>
                <p v-if="item.provisioning_error" class="text-sm text-warning" role="status">{{ t(`resellerConsole.domains.errors.${item.provisioning_error}`) }}</p>
                <p class="text-xs text-muted-foreground">{{ t('personalCenter.reseller.updatedAt') }} {{ formatResellerConsoleDate(item.updated_at) }}</p>
              </div>
              <div class="flex shrink-0 flex-wrap items-center gap-2">
                <Button v-if="snapshot?.domain_binding_enabled && item.type === 'custom' && item.status !== RESELLER_DOMAIN_STATUS_DISABLED" variant="outline" size="sm" :disabled="refreshingId === item.id" @click="refreshDomain(item.id)"><RotateCw class="h-3.5 w-3.5" :class="refreshingId === item.id ? 'animate-spin' : ''" />{{ t('resellerConsole.domains.refreshValidation') }}</Button>
                <ResellerCopyButton :value="item.domain" :show-label="false" :label="t('resellerConsole.domains.copyAddress')" />
                <Button v-if="isActiveVerifiedDomain(item)" as-child variant="ghost" size="sm"><a :href="`https://${item.domain}`" target="_blank" rel="noopener noreferrer"><ExternalLink class="h-4 w-4" />{{ t('resellerConsole.domains.visit') }}</a></Button>
              </div>
            </div>
          </div>
          <details class="border-t border-border" :open="group.domains.some(item => !isActiveVerifiedDomain(item) || item.provisioning_error)">
            <summary class="cursor-pointer px-5 py-3 text-sm text-muted-foreground hover:text-foreground sm:px-6">{{ t('resellerConsole.domains.connectionDetails') }}</summary>
            <div class="space-y-4 px-5 pb-5 sm:px-6">
              <div v-if="groupRecords(group.domains).length" class="overflow-x-auto rounded-lg border border-border">
                <table class="w-full text-left text-sm">
                  <thead class="bg-muted/30 text-xs text-muted-foreground"><tr><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordName') }}</th><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordType') }}</th><th class="px-3 py-2 font-medium">{{ t('resellerConsole.domains.recordTarget') }}</th></tr></thead>
                  <tbody class="divide-y divide-border"><tr v-for="record in groupRecords(group.domains)" :key="record.name"><td class="break-all px-3 py-2 font-mono">{{ record.name }}</td><td class="px-3 py-2 font-mono">{{ record.type }}</td><td class="px-3 py-2"><div class="flex flex-wrap items-center gap-2"><span class="break-all font-mono">{{ record.value }}</span><ResellerCopyButton :value="record.value" :show-label="false" :label="t('resellerConsole.domains.copyTarget')" /></div></td></tr></tbody>
                </table>
              </div>
              <dl class="space-y-3 text-xs text-muted-foreground"><div v-for="item in group.domains" :key="item.id" class="flex flex-wrap items-center gap-x-4 gap-y-2"><dt class="break-all font-mono">{{ item.domain }}</dt><dd>{{ t('resellerConsole.domains.tableStatus') }}：{{ domainStatusLabel(item.status) }}</dd><dd>{{ t('resellerConsole.domains.tableVerification') }}：{{ verificationLabel(item.verification_status) }}</dd><dd v-if="item.provisioning_status">{{ t(`resellerConsole.domains.provisioning.${item.provisioning_status}`) }}</dd></div></dl>
            </div>
          </details>
        </article>
        <div v-if="!domainGroups.length" class="rounded-lg border border-dashed border-border bg-card px-5 py-8 text-center sm:px-6">
          <Globe2 class="mx-auto h-8 w-8 text-muted-foreground" /><h3 class="mt-3 text-sm font-bold">{{ t('personalCenter.reseller.domainEmpty') }}</h3><p class="mt-2 text-sm text-muted-foreground">{{ t('resellerConsole.domains.customEmptyDescription') }}</p>
        </div>
      </section>

      <section class="rounded-lg border border-border bg-card px-5 py-4 sm:px-6" data-testid="system-domain">
        <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div class="flex min-w-0 items-start gap-3">
            <Link2 class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
            <div class="min-w-0">
              <h2 class="text-sm font-semibold">{{ t('resellerConsole.domains.systemDomainTitle') }}</h2>
              <template v-if="systemDomain">
                <p class="mt-2 break-all font-mono text-sm font-semibold">{{ systemDomain.domain }}</p>
                <div class="mt-2 flex flex-wrap gap-2"><ResellerStatusBadge :label="domainStatusLabel(systemDomain.status)" :tone="domainTone(systemDomain.status)" dot /><ResellerStatusBadge :label="verificationLabel(systemDomain.verification_status)" :tone="verificationTone(systemDomain.verification_status)" /><ResellerStatusBadge v-if="systemDomain.is_primary && isActiveVerifiedDomain(systemDomain)" :label="t('personalCenter.reseller.primaryDomain')" tone="accent" /></div>
                <p class="mt-2 text-xs text-muted-foreground">{{ t('personalCenter.reseller.updatedAt') }} {{ formatResellerConsoleDate(systemDomain.updated_at) }}</p>
              </template>
              <p v-else class="mt-1 text-sm text-muted-foreground">{{ t(activeDomains.length ? 'resellerConsole.domains.systemOptional' : 'resellerConsole.domains.systemWaitingDesc') }}</p>
            </div>
          </div>
          <div v-if="systemDomain" class="flex shrink-0 flex-wrap items-center gap-2"><ResellerCopyButton :value="systemDomain.domain" :label="t('resellerConsole.common.copy')" /><Button as-child variant="ghost" size="sm"><a :href="`https://${systemDomain.domain}`" target="_blank" rel="noopener noreferrer"><ExternalLink class="h-4 w-4" />{{ t('resellerConsole.domains.visit') }}</a></Button></div>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronDown, ExternalLink, Globe2, Link2, Plus, RotateCw } from 'lucide-vue-next'
import { resellerAPI, type ResellerDomainData, type ResellerManagementSnapshotData } from '../../api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import ResellerCopyButton from '../../components/reseller-console/ResellerCopyButton.vue'
import ResellerPageState from '../../components/reseller-console/ResellerPageState.vue'
import ResellerSectionHeader from '../../components/reseller-console/ResellerSectionHeader.vue'
import ResellerStatusBadge, { type ResellerBadgeTone } from '../../components/reseller-console/ResellerStatusBadge.vue'
import {
  RESELLER_DOMAIN_STATUS_ACTIVE,
  RESELLER_DOMAIN_STATUS_DISABLED,
  RESELLER_DOMAIN_STATUS_PENDING_REVIEW,
  RESELLER_DOMAIN_TYPE_SUBDOMAIN,
  RESELLER_DOMAIN_VERIFICATION_FAILED,
  RESELLER_DOMAIN_VERIFICATION_PENDING,
  RESELLER_DOMAIN_VERIFICATION_VERIFIED,
  RESELLER_PROFILE_STATUS_ACTIVE,
} from '../../constants/reseller'
import { type PageAlert } from '../../utils/alerts'
import { formatResellerConsoleDate } from '../../utils/resellerConsole'
import { getResellerDomainStatusKey, groupResellerDomains } from '../../utils/resellerManagement'

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const refreshingId = ref<number | null>(null)
let pollTimer: ReturnType<typeof setInterval> | undefined
let fetching = false
const snapshot = ref<ResellerManagementSnapshotData | null>(null)
const alert = ref<PageAlert | null>(null)
const domainForm = reactive({ domain: '' })

const domains = computed(() => snapshot.value?.domains || [])
const canSubmitDomain = computed(() => snapshot.value?.profile?.status === RESELLER_PROFILE_STATUS_ACTIVE)
const isActiveVerifiedDomain = (domain: ResellerDomainData) =>
  domain.status === RESELLER_DOMAIN_STATUS_ACTIVE &&
  domain.verification_status === RESELLER_DOMAIN_VERIFICATION_VERIFIED
const systemDomain = computed(() => domains.value.find((d) => d.type === RESELLER_DOMAIN_TYPE_SUBDOMAIN) || null)
const customDomains = computed(() => domains.value.filter((d) => d.type !== RESELLER_DOMAIN_TYPE_SUBDOMAIN))
const activeDomains = computed(() => domains.value.filter(isActiveVerifiedDomain))
const primaryDomain = computed(() => activeDomains.value.find((d) => d.is_primary) || null)
const domainGroups = computed(() => groupResellerDomains(customDomains.value))
const groupRecords = (rows: ResellerDomainData[]) => [...new Map(rows.flatMap(item => item.dns_records || []).map(record => [record.name, record])).values()]
const progressLabel = (item: ResellerDomainData) => {
  if (item.status === RESELLER_DOMAIN_STATUS_DISABLED) return domainStatusLabel(item.status)
  if (item.provisioning_error) return t('resellerConsole.domains.connectionIssue')
  if (isActiveVerifiedDomain(item) || item.status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW) return domainStatusLabel(item.status)
  if (item.provisioning_status && item.provisioning_status !== 'ready') return t(`resellerConsole.domains.provisioning.${item.provisioning_status}`)
  if (item.verification_status !== RESELLER_DOMAIN_VERIFICATION_VERIFIED) return verificationLabel(item.verification_status)
  return domainStatusLabel(item.status)
}
const progressTone = (item: ResellerDomainData): ResellerBadgeTone => {
  if (item.status === RESELLER_DOMAIN_STATUS_DISABLED) return 'neutral'
  if (item.provisioning_error || !isActiveVerifiedDomain(item)) return 'warning'
  return 'success'
}

const load = async (silent = false) => {
  if (fetching) return
  fetching = true
  if (!silent) { loading.value = true; alert.value = null }
  try {
    const response = await resellerAPI.managementProfile()
    snapshot.value = response.data.data || null
  } finally {
    fetching = false
    if (!silent) loading.value = false
  }
}

const refreshDomain = async (id: number) => {
  refreshingId.value = id
  try {
    await resellerAPI.refreshDomain(id)
    await load(true)
  } catch (err) {
    alert.value = { level: 'error', message: err instanceof Error ? err.message : t('personalCenter.common.saveFailed') }
  } finally { refreshingId.value = null }
}

const submitDomain = async () => {
  if (!domainForm.domain.trim()) return
  submitting.value = true
  alert.value = null
  try {
    await resellerAPI.submitDomain({ domain: domainForm.domain.trim() })
    domainForm.domain = ''
    await load()
    alert.value = { level: 'success', message: t(snapshot.value?.domain_binding_enabled ? 'resellerConsole.domains.submittedForValidation' : 'personalCenter.common.saveSuccess') }
  } catch (err) {
    // 透传后端细分错误（域名格式无效 / 不能用主站域名 / 已被占用等）。
    const message = err instanceof Error && err.message ? err.message : t('personalCenter.common.saveFailed')
    alert.value = { level: 'error', message }
  } finally {
    submitting.value = false
  }
}

const domainStatusLabel = (status?: string) => t(`personalCenter.reseller.domainStatus.${getResellerDomainStatusKey(status)}`)

const domainTone = (status?: string): ResellerBadgeTone => {
  if (status === RESELLER_DOMAIN_STATUS_ACTIVE) return 'success'
  if (status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW) return 'warning'
  if (status === RESELLER_DOMAIN_STATUS_DISABLED) return 'neutral'
  return 'neutral'
}

const verificationLabel = (status?: string) => {
  if (status === RESELLER_DOMAIN_VERIFICATION_VERIFIED) return t('personalCenter.reseller.domainVerification.verified')
  if (status === RESELLER_DOMAIN_VERIFICATION_PENDING) return t('personalCenter.reseller.domainVerification.pending')
  if (status === RESELLER_DOMAIN_VERIFICATION_FAILED) return t('personalCenter.reseller.domainVerification.failed')
  return t('personalCenter.reseller.domainVerification.unknown')
}

const verificationTone = (status?: string): ResellerBadgeTone => {
  if (status === RESELLER_DOMAIN_VERIFICATION_VERIFIED) return 'success'
  if (status === RESELLER_DOMAIN_VERIFICATION_PENDING) return 'warning'
  if (status === RESELLER_DOMAIN_VERIFICATION_FAILED) return 'warning'
  return 'neutral'
}

onMounted(() => {
  void load()
  pollTimer = setInterval(() => {
    if (snapshot.value?.domain_binding_enabled && domains.value.some(item => item.status === 'provisioning' || item.status === RESELLER_DOMAIN_STATUS_PENDING_REVIEW)) {
      void load(true).catch(() => {})
    }
  }, 15000)
})
onUnmounted(() => { if (pollTimer) clearInterval(pollTimer) })
</script>
