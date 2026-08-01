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
import { z } from 'zod'

// ============================================================================
// User Schema & Types
// ============================================================================

/** User status: 1 = enabled, 2 = disabled, 3+ = other states */
export const userStatusSchema = z.number()
export type UserStatus = z.infer<typeof userStatusSchema>

/** User role: 1 = common user, 5 = agent, 10 = admin, 100 = root */
export const userRoleSchema = z.number()
export type UserRole = z.infer<typeof userRoleSchema>

export const userSchema = z.object({
  id: z.number(),
  username: z.string(),
  display_name: z.string(),
  password: z.string().optional(),
  github_id: z.string().optional(),
  oidc_id: z.string().optional(),
  wechat_id: z.string().optional(),
  telegram_id: z.string().optional(),
  email: z.string().optional(),
  quota: z.number(),
  used_quota: z.number(),
  request_count: z.number(),
  concurrency: z.number().optional(),
  group: z.string(),
  aff_code: z.string().optional(),
  aff_count: z.number().optional(),
  aff_quota: z.number().optional(),
  aff_history_quota: z.number().optional(),
  inviter_id: z.number().optional(),
  linux_do_id: z.string().optional(),
  status: userStatusSchema,
  role: userRoleSchema,
  created_at: z.number().optional(),
  updated_at: z.number().optional(),
  last_login_at: z.number().optional(),
  DeletedAt: z.any().nullable().optional(),
  remark: z.string().optional(),
  setting: z.string().optional(),
})
export type User = z.infer<typeof userSchema>

export const userListSchema = z.array(userSchema)

// ============================================================================
// API Request/Response Types
// ============================================================================

/** Generic API response */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetUsersParams {
  p?: number
  page_size?: number
}

export interface GetUsersResponse {
  success: boolean
  message?: string
  data?: {
    items: User[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchUsersParams {
  keyword?: string
  group?: string
  status?: string | number
  role?: string | number
  p?: number
  page_size?: number
}

export interface UserStatisticsSummary {
  total_users: number
  enabled_users: number
  disabled_users: number
  active_users: number
  new_users: number
  new_previous_period: number
  total_quota: number
  total_used_quota: number
  total_request_count: number
}

export interface UserStatisticsParams {
  start_date: string
  end_date: string
}

export interface UserStatisticsTrendPoint {
  date: string
  count: number
}

export interface UserStatisticsDistribution {
  name: string
  value?: number
  count: number
}

export interface UserStatisticsRecentUser {
  id: number
  username: string
  display_name: string
  role: number
  status: number
  group: string
  created_at: number
  last_login_at: number
}

export interface UserStatistics {
  summary: UserStatisticsSummary
  registration_trend: UserStatisticsTrendPoint[]
  group_distribution: UserStatisticsDistribution[]
  recent_users: UserStatisticsRecentUser[]
}

export interface UserFormData {
  username: string
  display_name: string
  password?: string
  role?: number // Only used when creating user
  quota?: number // Only used when updating user
  concurrency?: number
  group?: string // Only used when updating user
  inviter_id?: number // Only Root can update user inviter
  remark?: string // Only used when updating user
  setting?: {
    context_auto_compact_enabled?: boolean
    context_auto_compact_mode?: string
    context_auto_compact_trigger_k?: number
    context_auto_compact_target_k?: number
  }
}

export type ManageUserAction =
  | 'promote'
  | 'promote_agent'
  | 'demote'
  | 'enable'
  | 'disable'
  | 'delete'
  | 'add_quota'

export type QuotaAdjustMode = 'add' | 'subtract' | 'override'

export interface ManageUserQuotaPayload {
  id: number
  action: 'add_quota'
  mode: QuotaAdjustMode
  value: number
}

export interface BatchUpdateUserGroupPayload {
  ids: number[]
  group: string
}

export interface BatchUpdateUsersResult {
  updated_count: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type UsersDialogType = 'create' | 'update' | 'delete'
