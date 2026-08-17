/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import type { AxiosRequestConfig } from 'axios'
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
  UpstreamAccountPoolCapabilities,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamAccountQuotaResetResult,
  UpstreamAccountPoolPublishResult,
  UpstreamAccountQuotaUsage,
  UpstreamAccountUsage,
  UpstreamAccountScheduledTestPlan,
  UpstreamAccountScheduledTestPlanPayload,
  UpstreamAccountScheduledTestResult,
  UpstreamAccountStats,
  UpstreamAccountWindowStats,
  UpstreamBatchResult,
  UpstreamPoolPayload,
  UpstreamProxy,
  UpstreamProxyPayload,
  UpstreamDataImportResult,
} from './types'

type LocalFeedbackApiConfig = AxiosRequestConfig & {
  skipBusinessError?: boolean
  skipErrorHandler?: boolean
}

const localFeedbackApiConfig: LocalFeedbackApiConfig = {
  skipBusinessError: true,
  skipErrorHandler: true,
}

export async function listUpstreamPools(): Promise<
  ApiResponse<UpstreamAccountPool[]>
> {
  const response = await api.get('/api/upstream/account-pools')
  return response.data
}

export async function getUpstreamPoolCapabilities(
  id: number
): Promise<ApiResponse<UpstreamAccountPoolCapabilities>> {
  const response = await api.get(
    `/api/upstream/account-pools/${id}/capabilities`,
    localFeedbackApiConfig
  )
  return response.data
}

export async function listAvailableChannelGroups(): Promise<
  ApiResponse<string[]>
> {
  const response = await api.get('/api/group/', localFeedbackApiConfig)
  return response.data
}

export async function publishUpstreamPoolChannel(
  id: number,
  groups: string[],
  models: string[]
): Promise<ApiResponse<UpstreamAccountPoolPublishResult>> {
  const response = await api.post(
    `/api/upstream/account-pools/${id}/publish`,
    { groups, models },
    localFeedbackApiConfig
  )
  return response.data
}

export async function unpublishUpstreamPoolChannel(
  id: number
): Promise<ApiResponse<null>> {
  const response = await api.delete(`/api/upstream/account-pools/${id}/publish`)
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
  sort_by?: 'priority' | 'schedulable'
  sort_order?: 'asc' | 'desc'
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
  id: number,
  model?: string,
  mode: 'default' | 'compact' = 'default'
): Promise<ApiResponse<UpstreamAccountTestResult>> {
  const response = await api.post(`/api/upstream/accounts/${id}/test`, {
    model_id: model,
    mode,
  })
  return response.data
}

export async function getUpstreamAccountQuota(
  id: number,
  options?: { force?: boolean; includeCredits?: boolean }
): Promise<ApiResponse<UpstreamAccountQuotaUsage>> {
  const config: LocalFeedbackApiConfig = {
    ...localFeedbackApiConfig,
    params: {
      force: options?.force || undefined,
      include_credits:
        options?.includeCredits === undefined
          ? undefined
          : options.includeCredits,
    },
  }
  const response = await api.get(`/api/upstream/accounts/${id}/quota`, config)
  return response.data
}

export async function resetUpstreamAccountQuota(
  id: number
): Promise<ApiResponse<UpstreamAccountQuotaResetResult>> {
  const response = await api.post(
    `/api/upstream/accounts/${id}/reset-quota`,
    undefined,
    localFeedbackApiConfig
  )
  return response.data
}

export async function getUpstreamAccountUsage(
  id: number,
  options?: { force?: boolean }
): Promise<ApiResponse<UpstreamAccountUsage>> {
  const response = await api.get(`/api/upstream/accounts/${id}/usage`, {
    ...localFeedbackApiConfig,
    params: { force: options?.force || undefined },
  })
  return response.data
}

export async function getUpstreamAccountStats(
  id: number,
  days = 30
): Promise<ApiResponse<UpstreamAccountStats>> {
  const response = await api.get(`/api/upstream/accounts/${id}/stats`, {
    params: { days },
  })
  return response.data
}

export async function getUpstreamAccountTodayStats(
  id: number
): Promise<ApiResponse<UpstreamAccountWindowStats>> {
  const response = await api.get(`/api/upstream/accounts/${id}/today-stats`, {
    ...localFeedbackApiConfig,
  })
  return response.data
}

export async function getUpstreamAccountTodayStatsBatch(
  accountIds: number[]
): Promise<ApiResponse<{ stats: Record<string, UpstreamAccountWindowStats> }>> {
  const response = await api.post(
    '/api/upstream/accounts/today-stats/batch',
    { account_ids: accountIds },
    localFeedbackApiConfig
  )
  return response.data
}

export async function listUpstreamAccountScheduledTests(
  accountId: number
): Promise<ApiResponse<UpstreamAccountScheduledTestPlan[]>> {
  const response = await api.get(
    `/api/upstream/accounts/${accountId}/scheduled-tests`
  )
  return response.data
}

export async function createUpstreamAccountScheduledTest(
  accountId: number,
  payload: UpstreamAccountScheduledTestPlanPayload
): Promise<ApiResponse<UpstreamAccountScheduledTestPlan>> {
  const response = await api.post(
    `/api/upstream/accounts/${accountId}/scheduled-tests`,
    payload
  )
  return response.data
}

export async function updateUpstreamAccountScheduledTest(
  accountId: number,
  planId: number,
  payload: Partial<UpstreamAccountScheduledTestPlanPayload>
): Promise<ApiResponse<UpstreamAccountScheduledTestPlan>> {
  const response = await api.put(
    `/api/upstream/accounts/${accountId}/scheduled-tests/${planId}`,
    payload
  )
  return response.data
}

export async function deleteUpstreamAccountScheduledTest(
  accountId: number,
  planId: number
): Promise<ApiResponse<null>> {
  const response = await api.delete(
    `/api/upstream/accounts/${accountId}/scheduled-tests/${planId}`
  )
  return response.data
}

export async function listUpstreamAccountScheduledTestResults(
  accountId: number,
  planId: number
): Promise<ApiResponse<UpstreamAccountScheduledTestResult[]>> {
  const response = await api.get(
    `/api/upstream/accounts/${accountId}/scheduled-tests/${planId}/results`
  )
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
