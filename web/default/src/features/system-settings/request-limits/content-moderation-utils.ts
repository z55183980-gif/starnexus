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

export const CONTENT_MODERATION_CATEGORIES = [
  'harassment',
  'harassment/threatening',
  'hate',
  'hate/threatening',
  'illicit',
  'illicit/violent',
  'self-harm',
  'self-harm/intent',
  'self-harm/instructions',
  'sexual',
  'sexual/minors',
  'violence',
  'violence/graphic',
] as const

export const DEFAULT_CONTENT_MODERATION_THRESHOLDS: Record<string, number> = {
  harassment: 0.98,
  'harassment/threatening': 0.9,
  hate: 0.65,
  'hate/threatening': 0.65,
  illicit: 0.95,
  'illicit/violent': 0.95,
  'self-harm': 0.65,
  'self-harm/intent': 0.85,
  'self-harm/instructions': 0.65,
  sexual: 0.65,
  'sexual/minors': 0.65,
  violence: 0.95,
  'violence/graphic': 0.95,
}

export type ContentModerationMode = 'pre_block' | 'observe'

export type ContentModerationConfigView = {
  enabled: boolean
  mode: ContentModerationMode
  base_url: string
  model: string
  api_key_count?: number
  api_key_masks?: string[]
  api_keys: string[]
  timeout_ms: number
  thresholds: Record<string, number>
  keys_configured?: boolean
}

export const DEFAULT_CONTENT_MODERATION_CONFIG: ContentModerationConfigView = {
  enabled: false,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_keys: [],
  timeout_ms: 3000,
  thresholds: { ...DEFAULT_CONTENT_MODERATION_THRESHOLDS },
  keys_configured: false,
}

function normalizeMode(value: unknown): ContentModerationMode {
  return value === 'observe' ? 'observe' : 'pre_block'
}

export function parseContentModerationConfig(
  value: string | undefined | null
): ContentModerationConfigView {
  if (!value || !value.trim()) {
    return { ...DEFAULT_CONTENT_MODERATION_CONFIG }
  }
  try {
    const parsed = JSON.parse(value) as Partial<ContentModerationConfigView>
    const thresholds = {
      ...DEFAULT_CONTENT_MODERATION_THRESHOLDS,
      ...(parsed.thresholds ?? {}),
    }
    const apiKeys = Array.isArray(parsed.api_keys)
      ? parsed.api_keys.map((key) => String(key ?? '').trim()).filter(Boolean)
      : []
    return {
      enabled: Boolean(parsed.enabled),
      mode: normalizeMode(parsed.mode),
      base_url: String(parsed.base_url || DEFAULT_CONTENT_MODERATION_CONFIG.base_url),
      model: String(parsed.model || DEFAULT_CONTENT_MODERATION_CONFIG.model),
      api_key_count: parsed.api_key_count,
      api_key_masks: parsed.api_key_masks,
      api_keys: apiKeys,
      timeout_ms:
        typeof parsed.timeout_ms === 'number' && parsed.timeout_ms > 0
          ? parsed.timeout_ms
          : DEFAULT_CONTENT_MODERATION_CONFIG.timeout_ms,
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
  return JSON.stringify({
    enabled: config.enabled,
    mode: normalizeMode(config.mode),
    base_url: config.base_url,
    model: config.model,
    api_keys: config.api_keys,
    timeout_ms: config.timeout_ms,
    thresholds: config.thresholds,
  })
}
