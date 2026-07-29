/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { api } from '@/lib/api'
import type {
  ApiResponse,
  CRSPreviewResult,
  CRSSyncInput,
  CRSSyncResult,
  UpstreamAccount,
  UpstreamAccountExport,
  UpstreamAccountTestResult,
  UpstreamAccountPoolMember,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamAccountQuotaResetResult,
  UpstreamAccountQuotaUsage,
  UpstreamBatchResult,
  UpstreamPoolPayload,
  UpstreamProxy,
  UpstreamProxyPayload,
  UpstreamDataImportResult,
} from './types'

export async function listUpstreamPools(): Promise<
  ApiResponse<UpstreamAccountPool[]>
> {
  const response = await api.get('/api/upstream/account-pools')
  return response.data
}

export async function createUpstreamPool(
  payload: UpstreamPoolPayload
): Promise<ApiResponse<UpstreamAccountPool>> {
  const response = await api.post('/api/upstream/account-pools', payload)
  return response.data
}

export async function updateUpstreamPool(
  id: number,
  payload: UpstreamPoolPayload
): Promise<ApiResponse<null>> {
  const response = await api.put(`/api/upstream/account-pools/${id}`, payload)
  return response.data
}

export async function deleteUpstreamPool(
  id: number
): Promise<ApiResponse<null>> {
  const response = await api.delete(`/api/upstream/account-pools/${id}`)
  return response.data
}

export async function listUpstreamPoolMembers(
  id: number
): Promise<ApiResponse<UpstreamAccountPoolMember[]>> {
  const response = await api.get(`/api/upstream/account-pools/${id}/members`)
  return response.data
}

export async function replaceUpstreamPoolMembers(
  id: number,
  members: Array<{ account_id: number; priority: number; weight: number }>
): Promise<ApiResponse<null>> {
  const response = await api.put(`/api/upstream/account-pools/${id}/members`, {
    members,
  })
  return response.data
}

export async function listUpstreamAccounts(params?: {
  page?: number
  page_size?: number
  search?: string
  pool_id?: number
  proxy_id?: number
  platform?: string
  type?: string
  status?: string
  schedulable?: boolean
}): Promise<
  ApiResponse<{
    items: UpstreamAccount[]
    total: number
    page: number
    page_size: number
  }>
> {
  const response = await api.get('/api/upstream/accounts', { params })
  return response.data
}

export async function createUpstreamAccount(
  payload: UpstreamAccountPayload
): Promise<ApiResponse<UpstreamAccount>> {
  const response = await api.post('/api/upstream/accounts', payload)
  return response.data
}

export async function createUpstreamAccountsBatch(
  items: UpstreamAccountPayload[]
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.post('/api/upstream/accounts/batch', { items })
  return response.data
}

export async function exportUpstreamAccounts(
  ids: number[]
): Promise<ApiResponse<UpstreamAccountExport>> {
  const response = await api.post('/api/upstream/accounts/export', { ids })
  return response.data
}

export async function importUpstreamData(
  data: UpstreamAccountExport
): Promise<ApiResponse<UpstreamDataImportResult>> {
  const response = await api.post('/api/upstream/accounts/import', {
    data,
    skip_default_group_bind: true,
  })
  return response.data
}

export async function previewUpstreamAccountsFromCRS(
  payload: Omit<CRSSyncInput, 'selected_account_ids'>
): Promise<ApiResponse<CRSPreviewResult>> {
  const response = await api.post(
    '/api/upstream/accounts/sync/crs/preview',
    payload
  )
  return response.data
}

export async function syncUpstreamAccountsFromCRS(
  payload: CRSSyncInput
): Promise<ApiResponse<CRSSyncResult>> {
  const response = await api.post('/api/upstream/accounts/sync/crs', payload)
  return response.data
}

export async function updateUpstreamAccountsBatch(
  ids: number[],
  patch: Partial<UpstreamAccountPayload>
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.put('/api/upstream/accounts/batch', { ids, patch })
  return response.data
}

export async function deleteUpstreamAccountsBatch(
  ids: number[]
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.delete('/api/upstream/accounts/batch', {
    data: { ids },
  })
  return response.data
}

export async function updateUpstreamAccount(
  id: number,
  payload: Partial<UpstreamAccountPayload>
): Promise<ApiResponse<UpstreamAccount>> {
  const response = await api.put(`/api/upstream/accounts/${id}`, payload)
  return response.data
}

export async function deleteUpstreamAccount(
  id: number
): Promise<ApiResponse<null>> {
  const response = await api.delete(`/api/upstream/accounts/${id}`)
  return response.data
}

export async function testUpstreamAccount(
  id: number
): Promise<ApiResponse<UpstreamAccountTestResult>> {
  const response = await api.post(`/api/upstream/accounts/${id}/test`)
  return response.data
}

export async function getUpstreamAccountQuota(
  id: number
): Promise<ApiResponse<UpstreamAccountQuotaUsage>> {
  const response = await api.get(`/api/upstream/accounts/${id}/quota`)
  return response.data
}

export async function resetUpstreamAccountQuota(
  id: number
): Promise<ApiResponse<UpstreamAccountQuotaResetResult>> {
  const response = await api.post(`/api/upstream/accounts/${id}/reset-quota`)
  return response.data
}

export async function recoverUpstreamAccount(
  id: number,
  scope: 'all' | 'rate_limit' | 'temporary' = 'all'
): Promise<ApiResponse<UpstreamAccount>> {
  const response = await api.post(`/api/upstream/accounts/${id}/recover`, {
    scope,
  })
  return response.data
}

export async function recoverUpstreamAccounts(
  ids: number[],
  scope: 'all' | 'rate_limit' | 'temporary' = 'all'
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.post('/api/upstream/accounts/recover', {
    ids,
    scope,
  })
  return response.data
}

export async function refreshUpstreamOAuth(
  id: number
): Promise<ApiResponse<UpstreamAccount>> {
  const response = await api.post(`/api/upstream/accounts/${id}/oauth/refresh`)
  return response.data
}

export async function startUpstreamOAuth(payload: {
  account_id?: number
  proxy_id?: number | null
  platform?: 'openai' | 'anthropic'
  credential_type?: 'oauth' | 'setup_token'
}): Promise<ApiResponse<{ authorize_url: string }>> {
  const response = await api.post('/api/upstream/accounts/oauth/start', payload)
  return response.data
}

export async function completeUpstreamOAuth(payload: {
  input: string
  name: string
  pool_ids: number[]
  proxy_id?: number
}): Promise<ApiResponse<UpstreamAccount>> {
  const response = await api.post(
    '/api/upstream/accounts/oauth/complete',
    payload
  )
  return response.data
}

export async function listUpstreamProxies(): Promise<
  ApiResponse<UpstreamProxy[]>
> {
  const response = await api.get('/api/upstream/proxies')
  return response.data
}

export async function createUpstreamProxy(
  payload: UpstreamProxyPayload
): Promise<ApiResponse<UpstreamProxy>> {
  const response = await api.post('/api/upstream/proxies', payload)
  return response.data
}

export async function createUpstreamProxiesBatch(
  items: UpstreamProxyPayload[]
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.post('/api/upstream/proxies/batch', { items })
  return response.data
}

export async function updateUpstreamProxiesBatch(
  ids: number[],
  patch: Partial<UpstreamProxyPayload>
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.put('/api/upstream/proxies/batch', { ids, patch })
  return response.data
}

export async function deleteUpstreamProxiesBatch(
  ids: number[]
): Promise<ApiResponse<UpstreamBatchResult>> {
  const response = await api.delete('/api/upstream/proxies/batch', {
    data: { ids },
  })
  return response.data
}

export async function updateUpstreamProxy(
  id: number,
  payload: Partial<UpstreamProxyPayload>
): Promise<ApiResponse<UpstreamProxy>> {
  const response = await api.put(`/api/upstream/proxies/${id}`, payload)
  return response.data
}

export async function deleteUpstreamProxy(
  id: number
): Promise<ApiResponse<null>> {
  const response = await api.delete(`/api/upstream/proxies/${id}`)
  return response.data
}

export async function testUpstreamProxy(
  id: number
): Promise<ApiResponse<{ result: string; latency_ms: number }>> {
  const response = await api.post(`/api/upstream/proxies/${id}/test`)
  return response.data
}
