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

export const CONTENT_MODERATION_CATEGORY_GROUPS = [
  {
    label: 'Severe safety risks in prompts',
    categories: [
      'child-exploitation',
      'sexual-content',
      'violence-weapons-terrorism',
      'self-harm',
      'hate-harassment',
    ],
  },
  {
    label: 'Illegal or abusive requests',
    categories: [
      'fraud-scams-spam',
      'illegal-activity',
      'cyber-abuse',
      'privacy-abuse',
      'intellectual-property',
    ],
  },
  {
    label: 'Safeguard bypass requests',
    categories: ['safeguards-evasion'],
  },
] as const

export const CONTENT_MODERATION_CATEGORIES =
  CONTENT_MODERATION_CATEGORY_GROUPS.flatMap((group) => group.categories)

export const DEFAULT_CONTENT_MODERATION_THRESHOLDS: Record<string, number> = {
  'child-exploitation': 0.65,
  'sexual-content': 0.65,
  'violence-weapons-terrorism': 0.95,
  'self-harm': 0.65,
  'hate-harassment': 0.65,
  'fraud-scams-spam': 0.9,
  'illegal-activity': 0.95,
  'cyber-abuse': 0.9,
  'privacy-abuse': 0.9,
  'intellectual-property': 0.9,
  'safeguards-evasion': 0.9,
}

export type ContentModerationMode = 'pre_block' | 'observe'

export type ContentModerationObserveHitAction =
  | 'observe'
  | 'pre_block'
  | 'pre_block_monitor'

export type ContentModerationModelType = 'general' | 'dedicated'

export type ContentModerationProvider = 'deepseek'

export type ContentModerationModelFilterType = 'all' | 'include' | 'exclude'

export type ContentModerationModelFilter = {
  type: ContentModerationModelFilterType
  models: string[]
}

export type ContentModerationConfigView = {
  enabled: boolean
  mode: ContentModerationMode
  observe_hit_action: ContentModerationObserveHitAction
  model_type: ContentModerationModelType
  provider: ContentModerationProvider
  base_url: string
  model: string
  api_key_count?: number
  api_key_masks?: string[]
  api_keys: string[]
  timeout_ms: number
  all_groups: boolean
  groups: string[]
  model_filter: ContentModerationModelFilter
  thresholds: Record<string, number>
  keys_configured?: boolean
}

export const DEFAULT_CONTENT_MODERATION_CONFIG: ContentModerationConfigView = {
  enabled: false,
  mode: 'pre_block',
  observe_hit_action: 'observe',
  model_type: 'dedicated',
  provider: 'deepseek',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_keys: [],
  timeout_ms: 3000,
  all_groups: true,
  groups: [],
  model_filter: {
    type: 'all',
    models: [],
  },
  thresholds: { ...DEFAULT_CONTENT_MODERATION_THRESHOLDS },
  keys_configured: false,
}

function normalizeMode(value: unknown): ContentModerationMode {
  return value === 'observe' ? 'observe' : 'pre_block'
}

function normalizeObserveHitAction(
  value: unknown
): ContentModerationObserveHitAction {
  if (value === 'pre_block' || value === 'pre_block_monitor') {
    return value
  }
  return 'observe'
}

function normalizeModelType(value: unknown): ContentModerationModelType {
  return value === 'general' ? 'general' : 'dedicated'
}

function normalizeProvider(value: unknown): ContentModerationProvider {
  switch (value) {
    case 'deepseek':
      return value
    default:
      return 'deepseek'
  }
}

function normalizeModelFilterType(
  value: unknown
): ContentModerationModelFilterType {
  if (value === 'include' || value === 'exclude') {
    return value
  }
  return 'all'
}

function normalizeStringList(value: unknown): string[] {
  if (!Array.isArray(value)) {
    return []
  }
  const seen = new Set<string>()
  const out: string[] = []
  for (const item of value) {
    const trimmed = String(item ?? '').trim()
    if (!trimmed) continue
    const key = trimmed.toLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    out.push(trimmed)
  }
  return out
}

function normalizeModelFilter(value: unknown): ContentModerationModelFilter {
  const raw =
    value && typeof value === 'object'
      ? (value as Partial<ContentModerationModelFilter>)
      : {}
  const type = normalizeModelFilterType(raw.type)
  const models = normalizeStringList(raw.models)
  if (type === 'all' || models.length === 0) {
    return { type: 'all', models: [] }
  }
  return { type, models }
}

export function parseContentModerationConfig(
  value: string | undefined | null
): ContentModerationConfigView {
  if (!value || !value.trim()) {
    return { ...DEFAULT_CONTENT_MODERATION_CONFIG }
  }
  try {
    const parsed = JSON.parse(value) as Partial<ContentModerationConfigView> & {
      all_groups?: boolean
    }
    const thresholds = {
      ...DEFAULT_CONTENT_MODERATION_THRESHOLDS,
      ...(parsed.thresholds ?? {}),
    }
    const apiKeys = Array.isArray(parsed.api_keys)
      ? parsed.api_keys.map((key) => String(key ?? '').trim()).filter(Boolean)
      : []
    const hasAllGroupsField = Object.prototype.hasOwnProperty.call(
      parsed,
      'all_groups'
    )
    const allGroups = hasAllGroupsField ? Boolean(parsed.all_groups) : true
    const groups = allGroups ? [] : normalizeStringList(parsed.groups)
    const modelType = normalizeModelType(parsed.model_type)
    return {
      enabled: Boolean(parsed.enabled),
      mode: normalizeMode(parsed.mode),
      observe_hit_action: normalizeObserveHitAction(parsed.observe_hit_action),
      model_type: modelType,
      provider: normalizeProvider(parsed.provider),
      base_url: String(
        parsed.base_url ||
          (modelType === 'general'
            ? 'https://api.deepseek.com'
            : DEFAULT_CONTENT_MODERATION_CONFIG.base_url)
      ),
      model: String(
        parsed.model ||
          (modelType === 'general'
            ? 'deepseek-v4-flash'
            : DEFAULT_CONTENT_MODERATION_CONFIG.model)
      ),
      api_key_count: parsed.api_key_count,
      api_key_masks: parsed.api_key_masks,
      api_keys: apiKeys,
      timeout_ms:
        typeof parsed.timeout_ms === 'number' && parsed.timeout_ms > 0
          ? parsed.timeout_ms
          : modelType === 'general'
            ? 8000
            : DEFAULT_CONTENT_MODERATION_CONFIG.timeout_ms,
      all_groups: allGroups,
      groups,
      model_filter: normalizeModelFilter(parsed.model_filter),
      thresholds,
      keys_configured: Boolean(parsed.keys_configured ?? apiKeys.length > 0),
    }
  } catch {
    return { ...DEFAULT_CONTENT_MODERATION_CONFIG }
  }
}

export function stringifyContentModerationConfig(
  config: ContentModerationConfigView
) {
  const modelFilter = normalizeModelFilter(config.model_filter)
  return JSON.stringify({
    enabled: config.enabled,
    mode: normalizeMode(config.mode),
    observe_hit_action: normalizeObserveHitAction(config.observe_hit_action),
    model_type: normalizeModelType(config.model_type),
    provider: normalizeProvider(config.provider),
    base_url: config.base_url,
    model: config.model,
    api_keys: config.api_keys,
    timeout_ms: config.timeout_ms,
    all_groups: Boolean(config.all_groups),
    groups: config.all_groups ? [] : normalizeStringList(config.groups),
    model_filter: modelFilter,
    thresholds: config.thresholds,
  })
}
