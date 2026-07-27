/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { api } from '@/lib/api'
import type {
  PromptAuditClearLogsResponse,
  PromptAuditLogCursorResponse,
  PromptAuditLogsResponse,
  PromptAuditPoliciesResponse,
  PromptAuditPolicy,
  PromptAuditPolicyResponse,
} from './types'

export async function listPromptAuditPolicies(): Promise<PromptAuditPoliciesResponse> {
  const response = await api.get('/api/security-audit/policies')
  const result = response.data as PromptAuditPoliciesResponse
  if (!result.success) throw new Error(result.message || 'Failed to load')
  return result
}

export async function createPromptAuditPolicy(
  userId: number
): Promise<PromptAuditPolicyResponse> {
  const response = await api.post('/api/security-audit/policies', {
    user_id: userId,
  })
  return response.data
}

export async function updatePromptAuditPolicy(
  policyId: number,
  changes: Partial<
    Pick<
      PromptAuditPolicy,
      'monitor_enabled' | 'delay_on_hit' | 'delay_seconds' | 'block_on_hit'
    >
  >
): Promise<PromptAuditPolicyResponse> {
  const response = await api.patch(
    `/api/security-audit/policies/${policyId}`,
    changes
  )
  return response.data
}

export async function deletePromptAuditPolicy(
  policyId: number
): Promise<PromptAuditPolicyResponse> {
  const response = await api.delete(`/api/security-audit/policies/${policyId}`)
  return response.data
}

export async function listPromptAuditLogs(
  userId: number,
  page = 1,
  pageSize = 50
): Promise<PromptAuditLogsResponse> {
  const response = await api.get(
    `/api/security-audit/users/${userId}/prompts?p=${page}&page_size=${pageSize}`
  )
  const result = response.data as PromptAuditLogsResponse
  if (!result.success) throw new Error(result.message || 'Failed to load')
  return result
}

async function listPromptAuditLogsByCursor(
  userId: number,
  cursorName: 'before_id' | 'after_id',
  cursor: number,
  pageSize: number
): Promise<PromptAuditLogCursorResponse> {
  const response = await api.get(
    `/api/security-audit/users/${userId}/prompts`,
    {
      params: {
        [cursorName]: cursor,
        page_size: pageSize,
      },
    }
  )
  const result = response.data as PromptAuditLogCursorResponse
  if (!result.success) throw new Error(result.message || 'Failed to load')
  return result
}

export function listPromptAuditLogsBefore(
  userId: number,
  beforeId = 0,
  pageSize = 50
): Promise<PromptAuditLogCursorResponse> {
  return listPromptAuditLogsByCursor(userId, 'before_id', beforeId, pageSize)
}

export function listPromptAuditLogsAfter(
  userId: number,
  afterId: number,
  pageSize = 100
): Promise<PromptAuditLogCursorResponse> {
  return listPromptAuditLogsByCursor(userId, 'after_id', afterId, pageSize)
}

export async function clearPromptAuditLogs(
  userId: number
): Promise<PromptAuditClearLogsResponse> {
  const response = await api.delete(
    `/api/security-audit/users/${userId}/prompts`
  )
  return response.data
}
