import type { AdminResellerBannerConfig } from '../api/types'

export type ResellerLocale = 'zh-CN' | 'zh-TW' | 'en-US'
export type ResellerLocalizedText = Record<ResellerLocale, string>

export const resellerLocales: ResellerLocale[] = ['zh-CN', 'zh-TW', 'en-US']

export const blankLocalizedText = (): ResellerLocalizedText => ({
  'zh-CN': '',
  'zh-TW': '',
  'en-US': '',
})

export const getLocalizedText = (
  value: Partial<ResellerLocalizedText> | Record<string, unknown> | string | undefined | null,
  locale: string,
) => {
  if (!value) return ''
  if (typeof value === 'string') return value.trim()

  const candidates = [
    value[locale as keyof typeof value],
    value['zh-CN' as keyof typeof value],
    value['zh-TW' as keyof typeof value],
    value['en-US' as keyof typeof value],
    ...Object.values(value),
  ]

  for (const item of candidates) {
    if (typeof item === 'string' && item.trim()) return item.trim()
  }
  return ''
}

export const normalizeLocalizedTextForForm = (
  value?: Partial<ResellerLocalizedText> | Record<string, unknown> | null,
): ResellerLocalizedText => ({
  'zh-CN': String(value?.['zh-CN'] || ''),
  'zh-TW': String(value?.['zh-TW'] || ''),
  'en-US': String(value?.['en-US'] || ''),
})

export const normalizeResellerBannerForForm = (
  banner?: Partial<AdminResellerBannerConfig> | null,
): AdminResellerBannerConfig => ({
  mode: banner?.mode === 'custom' || banner?.mode === 'hidden' ? banner.mode : 'inherit',
  image: String(banner?.image || ''),
  mobile_image: String(banner?.mobile_image || ''),
  title: normalizeLocalizedTextForForm(banner?.title),
  subtitle: normalizeLocalizedTextForForm(banner?.subtitle),
  link_type: banner?.link_type === 'internal' || banner?.link_type === 'external' ? banner.link_type : 'none',
  link_value: String(banner?.link_value || ''),
  open_in_new_tab: banner?.open_in_new_tab === true,
})

export const normalizeFooterLinksForForm = (
  links?: Array<{ name?: Partial<ResellerLocalizedText> | Record<string, unknown>; url?: string }> | null,
) =>
  Array.isArray(links)
    ? links.map((item) => ({
        name: normalizeLocalizedTextForForm(item.name),
        url: String(item.url || ''),
      }))
    : []

export const canEditResellerSiteConfig = (profile?: { status?: string } | null) =>
  profile?.status === 'active'
