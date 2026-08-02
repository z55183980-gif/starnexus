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
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/ui/skeleton'
import {
  getOptionValue,
  useSystemOptions,
} from '@/features/system-settings/hooks/use-system-options'
import { ContentModerationSection } from '@/features/system-settings/request-limits/content-moderation-section'
import {
  DEFAULT_CONTENT_MODERATION_CONFIG,
  stringifyContentModerationConfig,
} from '@/features/system-settings/request-limits/content-moderation-utils'

const defaultOptions = {
  ContentModerationConfig: stringifyContentModerationConfig(
    DEFAULT_CONTENT_MODERATION_CONFIG
  ),
}

export function ContentModerationTab() {
  const { t } = useTranslation()
  const { data, isLoading, isError, refetch } = useSystemOptions()

  if (isLoading) {
    return (
      <div className='space-y-3 py-2'>
        <Skeleton className='h-10 w-48' />
        <Skeleton className='h-24 w-full' />
        <Skeleton className='h-24 w-full' />
      </div>
    )
  }

  if (isError) {
    return (
      <div className='text-muted-foreground flex flex-col items-start gap-3 py-6 text-sm'>
        <span>{t('Failed to load')}</span>
        <button
          type='button'
          className='text-foreground underline'
          onClick={() => void refetch()}
        >
          {t('Retry')}
        </button>
      </div>
    )
  }

  const settings = getOptionValue(data?.data, defaultOptions)
  return (
    <ContentModerationSection
      defaultValue={settings.ContentModerationConfig}
      embedded
    />
  )
}
