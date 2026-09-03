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
import { useState, useCallback } from 'react'
import { Check, Copy, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { type ApiKey } from '../types'
import { useApiKeys } from './api-keys-provider'

export function ApiKeyCell({ apiKey }: { apiKey: ApiKey }) {
  const { t } = useTranslation()
  const {
    resolveRealKey,
    resolvedKeys,
    loadingKeys,
    copiedKeyId,
    markKeyCopied,
  } = useApiKeys()
  const [popoverOpen, setPopoverOpen] = useState(false)

  const isLoading = !!loadingKeys[apiKey.id]
  const resolvedFullKey = resolvedKeys[apiKey.id]
  const isCopied = copiedKeyId === apiKey.id
  const maskedKey = `sk-${apiKey.key}`

  const handlePopoverOpen = useCallback(
    (open: boolean) => {
      setPopoverOpen(open)
      if (open && !resolvedFullKey) {
        resolveRealKey(apiKey.id)
      }
    },
    [resolvedFullKey, resolveRealKey, apiKey.id]
  )

  const handleCopy = useCallback(async () => {
    const realKey = resolvedFullKey || (await resolveRealKey(apiKey.id))
    if (realKey) {
      const ok = await copyToClipboard(realKey)
      if (ok) markKeyCopied(apiKey.id)
    }
  }, [resolvedFullKey, resolveRealKey, apiKey.id, markKeyCopied])

  return (
    <div className='flex items-center'>
      <Popover open={popoverOpen} onOpenChange={handlePopoverOpen}>
        <PopoverTrigger
          render={
            <Button
              variant='ghost'
              size='sm'
              className='text-muted-foreground h-7 font-mono text-xs'
            />
          }
        >
          {maskedKey}
        </PopoverTrigger>
        <PopoverContent
          className='w-auto max-w-[min(90vw,28rem)]'
          align='start'
        >
          <div className='space-y-2'>
            <p className='text-muted-foreground text-xs'>{t('Full API Key')}</p>
            {isLoading ? (
              <div className='flex items-center gap-2 py-2'>
                <Loader2 className='size-3.5 animate-spin' />
                <span className='text-muted-foreground text-xs'>
                  {t('Loading...')}
                </span>
              </div>
            ) : (
              <input
                readOnly
                value={resolvedFullKey || maskedKey}
                autoFocus
                onFocus={(e) => e.target.select()}
                className='bg-muted/50 w-full min-w-[280px] rounded-md border px-3 py-2 font-mono text-xs outline-none'
              />
            )}
          </div>
        </PopoverContent>
      </Popover>
      <Tooltip>
        <TooltipTrigger
          render={
            <Button
              variant='ghost'
              size='icon'
              className='size-7 shrink-0'
              onClick={handleCopy}
              disabled={isLoading}
            />
          }
        >
          {isLoading ? (
            <Loader2 className='size-3.5 animate-spin' />
          ) : isCopied ? (
            <Check className='size-3.5 text-green-600' />
          ) : (
            <Copy className='size-3.5' />
          )}
        </TooltipTrigger>
        <TooltipContent>
          {isLoading
            ? t('Loading...')
            : isCopied
              ? t('Copied!')
              : t('Copy API key')}
        </TooltipContent>
      </Tooltip>
    </div>
  )
}

export function UsageCell({ apiKey }: { apiKey: ApiKey }) {
  const { t } = useTranslation()

  if (!apiKey.usage_available) {
    return (
      <span className='text-muted-foreground text-xs'>
        {t('Usage unavailable')}
      </span>
    )
  }

  return (
    <div className='flex min-w-[8.5rem] flex-col gap-1 text-xs tabular-nums'>
      <div>
        <span className='text-muted-foreground'>{t('Today')}: </span>
        <span className='font-medium'>{formatQuota(apiKey.today_quota)}</span>
      </div>
      <div>
        <span className='text-muted-foreground'>{t('Last 30 days')}: </span>
        <span className='font-medium'>
          {formatQuota(apiKey.thirty_day_quota)}
        </span>
      </div>
    </div>
  )
}
