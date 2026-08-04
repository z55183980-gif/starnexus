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
import type { ApiResponse } from '@/features/users/types'

export interface PromptAuditPolicy {
  id: number
  user_id: number
  username: string
  display_name: string
  email: string
  monitor_enabled: boolean
  delay_on_hit: boolean
  delay_seconds: number
  block_on_hit: boolean
  created_by: number
  created_at: number
  updated_at: number
}

export interface PromptAuditLog {
  id: number
  user_id: number
  username: string
  token_id: number
  token_name: string
  request_id: string
  model_name: string
  protocol: string
  endpoint: string
  prompt: string
  prompt_hash: string
  hit: boolean
  matched_words: string
  action: 'recorded' | 'hit' | 'delayed' | 'blocked'
  delay_ms: number
  truncated: boolean
  created_at: number
  score: number
}

export interface ContentModerationLogQuery {
  p?: number
  page_size?: number
  action?: string
  category?: string
  keyword?: string
  start_timestamp?: number
  end_timestamp?: number
}

export interface ContentModerationKeyBalance {
  currency: string
  total_balance: string
  granted_balance: string
  topped_up_balance: string
}

export interface ContentModerationKeyUsageItem {
  index: number
  key_mask: string
  provider: string
  model_name: string
  request_count: number
  prompt_tokens: number
  completion_tokens: number
  cache_hit_tokens: number
  cache_miss_tokens: number
  total_tokens: number
  token_usage_available: boolean
  billing_usd: number
  billing_available: boolean
  balance_available: boolean
  balances: ContentModerationKeyBalance[]
  balance_error?: string
}

export interface ContentModerationKeyUsageResult {
  start_time: number
  end_time: number
  items: ContentModerationKeyUsageItem[]
}

export interface PromptAuditLogPage {
  page: number
  page_size: number
  total: number
  items: PromptAuditLog[]
}

export interface PromptAuditLogCursorPage {
  items: PromptAuditLog[]
  has_more: boolean
  next_cursor: number
}

export type PromptAuditPolicyResponse = ApiResponse<PromptAuditPolicy>
export type PromptAuditPoliciesResponse = ApiResponse<PromptAuditPolicy[]>
export type PromptAuditLogsResponse = ApiResponse<PromptAuditLogPage>
export type PromptAuditLogCursorResponse = ApiResponse<PromptAuditLogCursorPage>
export type PromptAuditClearLogsResponse = ApiResponse<{ deleted: number }>
export type ContentModerationKeyUsageResponse =
  ApiResponse<ContentModerationKeyUsageResult>
