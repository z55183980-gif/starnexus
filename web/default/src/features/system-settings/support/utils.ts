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
import { DEFAULT_SUPPORT_CHANNELS_CONFIG } from './constants'
import type { SupportChannel, SupportChannelsConfig } from './types'

export function parseSupportChannelsConfig(
  value: string | undefined
): SupportChannelsConfig {
  if (!value?.trim()) {
    return structuredClone(DEFAULT_SUPPORT_CHANNELS_CONFIG)
  }
  try {
    const parsed = JSON.parse(value) as Partial<SupportChannelsConfig>
    return {
      enabled:
        typeof parsed.enabled === 'boolean'
          ? parsed.enabled
          : DEFAULT_SUPPORT_CHANNELS_CONFIG.enabled,
      panelTitle:
        typeof parsed.panelTitle === 'string'
          ? parsed.panelTitle
          : DEFAULT_SUPPORT_CHANNELS_CONFIG.panelTitle,
      panelSubtitle:
        typeof parsed.panelSubtitle === 'string'
          ? parsed.panelSubtitle
          : DEFAULT_SUPPORT_CHANNELS_CONFIG.panelSubtitle,
      channels: Array.isArray(parsed.channels)
        ? parsed.channels
            .filter(
              (item): item is SupportChannel =>
                item !== null &&
                typeof item === 'object' &&
                typeof (item as SupportChannel).id === 'string' &&
                typeof (item as SupportChannel).label === 'string' &&
                typeof (item as SupportChannel).type === 'string'
            )
            .map((item) => ({
              id: item.id,
              label: item.label,
              type: item.type,
              enabled: Boolean(item.enabled),
              url: item.url ?? '',
              widgetId: item.widgetId ?? '',
              imageUrl: item.imageUrl ?? '',
              openInNewTab: item.openInNewTab ?? true,
            }))
        : DEFAULT_SUPPORT_CHANNELS_CONFIG.channels,
    }
  } catch {
    return structuredClone(DEFAULT_SUPPORT_CHANNELS_CONFIG)
  }
}

export function stringifySupportChannelsConfig(config: SupportChannelsConfig) {
  return JSON.stringify(config)
}

export function formatSupportChannelsForEditor(value: string) {
  const config = parseSupportChannelsConfig(value)
  return JSON.stringify(config, null, 2)
}

export function normalizeSupportChannelsJson(value: string) {
  const config = parseSupportChannelsConfig(value)
  return stringifySupportChannelsConfig(config)
}

export function getChannelSummary(channel: SupportChannel): string {
  if (channel.type === 'chatway') {
    return channel.widgetId || '—'
  }
  if (channel.type === 'qrcode') {
    return channel.imageUrl || '—'
  }
  return channel.url || '—'
}
