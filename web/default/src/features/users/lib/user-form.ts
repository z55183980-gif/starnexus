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
import { quotaUnitsToDollars } from '@/lib/format'
import { DEFAULT_GROUP } from '../constants'
import { type UserFormData, type User } from '../types'

const CONTEXT_AUTO_COMPACT_FORM_ENABLED = false

// ============================================================================
// Form Schema
// ============================================================================

export const userFormSchema = z
  .object({
    username: z.string().min(1, 'Username is required'),
    display_name: z.string().optional(),
    password: z.string().optional(),
    role: z.number().optional(),
    quota_dollars: z.number().min(0).optional(),
    concurrency: z.number().min(0).optional(),
    group: z.string().optional(),
    inviter_id: z.number().min(0).optional(),
    remark: z.string().optional(),
    context_auto_compact_enabled: z.boolean().optional(),
    context_auto_compact_trigger_k: z.number().min(64).max(250).optional(),
    context_auto_compact_target_k: z.number().min(32).max(249).optional(),
  })
  .refine(
    (data) =>
      data.context_auto_compact_enabled !== true ||
      (data.context_auto_compact_target_k ?? 140) <
        (data.context_auto_compact_trigger_k ?? 250),
    {
      path: ['context_auto_compact_target_k'],
      message: 'Target must be lower than trigger',
    }
  )

export type UserFormValues = z.infer<typeof userFormSchema>

// ============================================================================
// Form Defaults
// ============================================================================

export const USER_FORM_DEFAULT_VALUES: UserFormValues = {
  username: '',
  display_name: '',
  password: '',
  role: 1, // Default to common user
  quota_dollars: 0,
  concurrency: 5,
  group: DEFAULT_GROUP,
  inviter_id: 0,
  remark: '',
  context_auto_compact_enabled: false,
  context_auto_compact_trigger_k: 250,
  context_auto_compact_target_k: 140,
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
export function transformFormDataToPayload(
  data: UserFormValues,
  userId?: number
): UserFormData & { id?: number } {
  const payload: UserFormData & { id?: number } = {
    username: data.username,
    display_name: data.display_name || data.username,
    password: data.password || undefined,
    concurrency: data.concurrency ?? 5,
  }

  // For create: only send required fields
  if (userId === undefined) {
    payload.role = data.role || 1 // Default to common user
  } else {
    // For update: quota is adjusted atomically via /api/user/manage, not sent here
    payload.role = data.role
    payload.group = data.group
    if (data.inviter_id !== undefined) {
      payload.inviter_id = data.inviter_id
    }
    payload.remark = data.remark || undefined
    // Context auto compaction payload is parked for now. Keep this block so it
    // can be re-enabled together with the backend relay trigger.
    if (CONTEXT_AUTO_COMPACT_FORM_ENABLED) {
      payload.setting = {
        context_auto_compact_enabled:
          data.context_auto_compact_enabled === true,
        context_auto_compact_mode: 'same_model_summary',
        context_auto_compact_trigger_k:
          data.context_auto_compact_trigger_k ?? 250,
        context_auto_compact_target_k:
          data.context_auto_compact_target_k ?? 140,
      }
    }
    payload.id = userId
  }

  return payload
}

/**
 * Transform user data to form defaults
 */
export function transformUserToFormDefaults(user: User): UserFormValues {
  let setting: Record<string, unknown> = {}
  if (user.setting) {
    try {
      setting = JSON.parse(user.setting) as Record<string, unknown>
    } catch (_error) {
      setting = {}
    }
  }
  return {
    username: user.username,
    display_name: user.display_name,
    password: '',
    role: user.role,
    quota_dollars: quotaUnitsToDollars(user.quota),
    concurrency: user.concurrency ?? 5,
    group: user.group || DEFAULT_GROUP,
    inviter_id: user.inviter_id ?? 0,
    remark: user.remark || '',
    context_auto_compact_enabled:
      setting.context_auto_compact_enabled === true,
    context_auto_compact_trigger_k:
      typeof setting.context_auto_compact_trigger_k === 'number'
        ? setting.context_auto_compact_trigger_k
        : 250,
    context_auto_compact_target_k:
      typeof setting.context_auto_compact_target_k === 'number'
        ? setting.context_auto_compact_target_k
        : 140,
  }
}
