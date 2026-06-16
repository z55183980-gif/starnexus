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
import { useTranslation } from 'react-i18next'
import { getOptionValue, useSystemOptions } from '../hooks/use-system-options'
import { SupportSettingsSection } from './support-settings-section'
import { stringifySupportChannelsConfig } from './utils'
import { DEFAULT_SUPPORT_CHANNELS_CONFIG } from './constants'
import type { SupportSettings } from './types'

const defaultSupportSettings: SupportSettings = {
  SupportChannels: stringifySupportChannelsConfig(DEFAULT_SUPPORT_CHANNELS_CONFIG),
}

export function SupportSettings() {
  const { t } = useTranslation()
  const { data, isLoading } = useSystemOptions()

  const settings = useMemo(
    () => getOptionValue(data?.data, defaultSupportSettings),
    [data?.data]
  )

  if (isLoading) {
    return (
      <div className='flex items-center justify-center py-12'>
        <div className='text-muted-foreground'>{t('Loading settings...')}</div>
      </div>
    )
  }

  return (
    <div className='flex h-full w-full flex-1 flex-col'>
      <div className='faded-bottom h-full w-full overflow-y-auto scroll-smooth pe-4 pb-12'>
        <div className='space-y-4'>
          <SupportSettingsSection defaultValue={settings.SupportChannels} />
        </div>
      </div>
    </div>
  )
}
