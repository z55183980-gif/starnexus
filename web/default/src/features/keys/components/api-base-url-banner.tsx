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
import { Globe2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useStatus } from '@/hooks/use-status'
import { CopyButton } from '@/components/copy-button'

function normalizeApiBaseUrl(value: unknown): string {
  if (typeof value !== 'string') return ''
  return value.trim().replace(/\/+$/, '')
}

export function ApiBaseUrlBanner() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const apiBaseUrl = normalizeApiBaseUrl(
    status?.api_base_url ?? status?.data?.api_base_url
  )

  if (!apiBaseUrl) return null

  return (
    <div
      className={cn(
        'border-border/70 bg-muted/30 flex min-w-0 items-center gap-2 rounded-md border px-3 py-2 text-sm'
      )}
    >
      <Globe2 className='text-muted-foreground size-4 shrink-0' />
      <span className='text-muted-foreground shrink-0'>
        {t('API Request Address')}
      </span>
      <span className='inline-flex min-w-0 max-w-full items-center gap-1'>
        <code className='min-w-0 truncate font-mono text-sm'>{apiBaseUrl}</code>
        <CopyButton
          value={apiBaseUrl}
          tooltip={t('Copy to clipboard')}
          successTooltip={t('Copied!')}
          aria-label={t('Copy to clipboard')}
          className='size-7 shrink-0'
          iconClassName='size-3.5'
        />
      </span>
    </div>
  )
}
