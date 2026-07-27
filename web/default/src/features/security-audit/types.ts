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
}

export interface PromptAuditLogPage {
  page: number
  page_size: number
  total: number
  items: PromptAuditLog[]
}

export type PromptAuditPolicyResponse = ApiResponse<PromptAuditPolicy>
export type PromptAuditPoliciesResponse = ApiResponse<PromptAuditPolicy[]>
export type PromptAuditLogsResponse = ApiResponse<PromptAuditLogPage>
export type PromptAuditClearLogsResponse = ApiResponse<{ deleted: number }>
