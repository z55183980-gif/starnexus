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
import { useMemo } from 'react'
import { useStatus } from '@/hooks/use-status'
import type { SystemStatus } from '@/features/auth/types'
import {
  DEFAULT_SUPPORT_CHANNELS_CONFIG,
} from '@/features/system-settings/support/constants'
import type { SupportChannelsConfig } from '@/features/system-settings/support/types'
import { parseSupportChannelsConfig } from '@/features/system-settings/support/utils'

function extractSupportChannelsRaw(
  status: SystemStatus | null
): string | SupportChannelsConfig | undefined {
  const raw =
    status?.support_channels ??
    (status?.data as Record<string, unknown> | undefined)?.support_channels

  if (typeof raw === 'string') {
    return raw
  }
  if (raw && typeof raw === 'object') {
    return raw as SupportChannelsConfig
  }

  if (typeof window !== 'undefined') {
    try {
      const saved = window.localStorage.getItem('status')
      if (!saved) return undefined
      const parsed = JSON.parse(saved) as SystemStatus
      const cached =
        parsed?.support_channels ??
        (parsed?.data as Record<string, unknown> | undefined)?.support_channels
      if (typeof cached === 'string') return cached
      if (cached && typeof cached === 'object') {
        return cached as SupportChannelsConfig
      }
    } catch {
      return undefined
    }
  }

  return undefined
}

export function useSupportChannels(): SupportChannelsConfig {
  const { status } = useStatus()

  return useMemo(() => {
    const raw = extractSupportChannelsRaw(status)
    if (typeof raw === 'string') {
      return parseSupportChannelsConfig(raw)
    }
    if (raw && typeof raw === 'object') {
      return parseSupportChannelsConfig(JSON.stringify(raw))
    }
    return DEFAULT_SUPPORT_CHANNELS_CONFIG
  }, [status])
}
