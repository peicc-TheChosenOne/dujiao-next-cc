import { userApi } from './client'
import type {
    ResellerApplyPayload,
    ResellerCustomDomainPayload,
    ResellerOrderListParams,
    ResellerOrderStatsParams,
    ResellerProductSettingUpdatePayload,
    ResellerProductImagesUpdatePayload,
    ResellerSiteConfigPayload,
    ResellerWithdrawApplyPayload,
} from './types'

export const resellerAPI = {
    managementProfile: () => userApi.get('/reseller/profile'),
    apply: (data: ResellerApplyPayload) => userApi.post('/reseller/apply', data),
    domains: () => userApi.get('/reseller/domains'),
    submitDomain: (data: ResellerCustomDomainPayload) => userApi.post('/reseller/domains', data),
    siteConfig: () => userApi.get('/reseller/site-config'),
    updateSiteConfig: (data: ResellerSiteConfigPayload) => userApi.put('/reseller/site-config', data),
    uploadImage: (file: File) => {
        const formData = new FormData()
        formData.append('file', file)
        return userApi.post('/reseller/upload', formData)
    },
    productSettings: (params?: any) => userApi.get('/reseller/product-settings', { params }),
    productImages: (params?: { page: number; page_size: number; keyword: string }) => userApi.get('/reseller/product-images', { params }),
    productImageDetail: (productId: number) => userApi.get(`/reseller/product-images/${productId}`),
    uploadProductImage: (productId: number, file: File, revision: number, operationId: string) => {
        const data = new FormData()
        data.append('file', file)
        data.append('revision', String(revision))
        data.append('operation_id', operationId)
        return userApi.post(`/reseller/product-images/${productId}/upload`, data)
    },
    updateProductImages: (productId: number, data: ResellerProductImagesUpdatePayload) => userApi.put(`/reseller/product-images/${productId}`, data),
    uploadProductDetailImage: (productId: number, file: File, revision: number, operationId: string) => {
        const data = new FormData()
        data.append('file', file)
        data.append('revision', String(revision))
        data.append('operation_id', operationId)
        return userApi.post(`/reseller/product-images/${productId}/detail/upload`, data)
    },
    resetProductDetailImage: (productId: number, data: Pick<ResellerProductImagesUpdatePayload, 'revision' | 'operation_id'>) => userApi.put(`/reseller/product-images/${productId}/detail`, data),
    productSettingDetail: (productId: number) => userApi.get(`/reseller/product-settings/${productId}`),
    updateProductSettings: (productId: number, data: ResellerProductSettingUpdatePayload) =>
        userApi.put(`/reseller/product-settings/${productId}`, data),
    previewProductSettings: (productId: number, data: ResellerProductSettingUpdatePayload) =>
        userApi.post(`/reseller/product-settings/${productId}/preview`, data),
    resetProductSetting: (productId: number, skuId = 0) =>
        userApi.delete(`/reseller/product-settings/${productId}`, { params: { sku_id: skuId } }),
    dashboard: () => userApi.get('/reseller/dashboard'),
    orders: (params?: ResellerOrderListParams) => userApi.get('/reseller/orders', { params }),
    orderStats: (params?: ResellerOrderStatsParams) => userApi.get('/reseller/orders/stats', { params }),
    orderDetail: (orderNo: string) => userApi.get(`/reseller/orders/${encodeURIComponent(orderNo)}`),
    balanceAccounts: (params?: any) => userApi.get('/reseller/balance-accounts', { params }),
    ledgerEntries: (params?: any) => userApi.get('/reseller/ledger-entries', { params }),
    withdraws: (params?: any) => userApi.get('/reseller/withdraws', { params }),
    applyWithdraw: (data: ResellerWithdrawApplyPayload) =>
        userApi.post('/reseller/withdraws', data),
}
