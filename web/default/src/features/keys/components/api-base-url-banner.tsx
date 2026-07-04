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

type ApiBaseUrlDisplayItem = {
  title: string
  url: string
}

function normalizeApiBaseUrl(value: unknown): string {
  if (typeof value !== 'string') return ''
  return value.trim().replace(/\/+$/, '')
}

function appendApiBaseUrlItem(
  items: ApiBaseUrlDisplayItem[],
  seen: Set<string>,
  title: unknown,
  url: unknown,
  fallbackTitle: string
) {
  const normalizedUrl = normalizeApiBaseUrl(url)
  if (!normalizedUrl || seen.has(normalizedUrl)) return

  seen.add(normalizedUrl)
  items.push({
    title:
      typeof title === 'string' && title.trim() ? title.trim() : fallbackTitle,
    url: normalizedUrl,
  })
}

function normalizeApiBaseUrlItems(
  value: unknown,
  fallbackValue: unknown,
  fallbackTitle: string
): ApiBaseUrlDisplayItem[] {
  const items: ApiBaseUrlDisplayItem[] = []
  const seen = new Set<string>()

  if (Array.isArray(value)) {
    value.forEach((item) => {
      if (typeof item === 'string') {
        appendApiBaseUrlItem(items, seen, fallbackTitle, item, fallbackTitle)
        return
      }
      if (item && typeof item === 'object') {
        const data = item as Record<string, unknown>
        appendApiBaseUrlItem(items, seen, data.title, data.url, fallbackTitle)
      }
    })
  }

  if (items.length === 0 && typeof value === 'string') {
    try {
      return normalizeApiBaseUrlItems(
        JSON.parse(value) as unknown,
        fallbackValue,
        fallbackTitle
      )
    } catch {
      appendApiBaseUrlItem(items, seen, fallbackTitle, value, fallbackTitle)
    }
  }

  if (items.length === 0) {
    appendApiBaseUrlItem(
      items,
      seen,
      fallbackTitle,
      fallbackValue,
      fallbackTitle
    )
  }

  return items
}

export function ApiBaseUrlBanner() {
  const { t } = useTranslation()
  const { status } = useStatus()
  const fallbackTitle = t('API Request Address')
  const apiBaseUrlItems = normalizeApiBaseUrlItems(
    status?.api_base_urls ?? status?.data?.api_base_urls,
    status?.api_base_url ?? status?.data?.api_base_url,
    fallbackTitle
  )

  if (apiBaseUrlItems.length === 0) return null

  return (
    <div
      className={cn(
        'border-border/70 bg-muted/30 flex min-w-0 flex-col gap-2 rounded-md border px-3 py-2 text-sm'
      )}
    >
      <div className='flex min-w-0 items-center gap-2'>
        <Globe2 className='text-muted-foreground size-4 shrink-0' />
        <span className='text-muted-foreground shrink-0'>{fallbackTitle}</span>
      </div>
      <div className='grid min-w-0 grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3'>
        {apiBaseUrlItems.map((item) => (
          <div
            key={item.url}
            className='border-border/60 bg-background/70 flex min-w-0 items-center gap-2 rounded-md border px-2 py-1.5'
          >
            <span className='text-foreground max-w-28 shrink-0 truncate font-medium'>
              {item.title}
            </span>
            <code className='min-w-0 flex-1 truncate font-mono text-sm'>
              {item.url}
            </code>
            <CopyButton
              value={item.url}
              tooltip={t('Copy to clipboard')}
              successTooltip={t('Copied!')}
              aria-label={t('Copy to clipboard')}
              className='size-7 shrink-0'
              iconClassName='size-3.5'
            />
          </div>
        ))}
      </div>
    </div>
  )
}
