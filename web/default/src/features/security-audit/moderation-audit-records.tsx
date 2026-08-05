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
import { Fragment, useDeferredValue, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, RefreshCw, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { formatTimestampToDate } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from '@/components/ui/hover-card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CopyButton } from '@/components/copy-button'
import {
  CONTENT_MODERATION_CATEGORIES,
  CONTENT_MODERATION_CATEGORY_GROUPS,
} from '@/features/system-settings/request-limits/content-moderation-utils'
import { listContentModerationLogs } from './api'
import type { PromptAuditLog } from './types'

const PAGE_SIZE = 20
const MODERATION_PREFIX = 'moderation:'

function parseMatchedCategories(matchedWords: string): string[] {
  try {
    const parsed = JSON.parse(matchedWords) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed
      .map((item) => String(item ?? ''))
      .filter((item) => item.startsWith(MODERATION_PREFIX))
      .map((item) => item.slice(MODERATION_PREFIX.length))
      .filter((item) =>
        CONTENT_MODERATION_CATEGORIES.includes(
          item as (typeof CONTENT_MODERATION_CATEGORIES)[number]
        )
      )
  } catch {
    return []
  }
}

function getHighestCategory(log: PromptAuditLog): string {
  const categories = parseMatchedCategories(log.matched_words)
  return categories[0] || '-'
}

function formatScorePercent(score: number): string {
  if (!Number.isFinite(score)) return '-'
  return `${(score * 100).toFixed(1)}%`
}

function summarizePrompt(prompt: string, maxLength = 48): string {
  const text = prompt.replace(/\s+/g, ' ').trim()
  if (text.length <= maxLength) return text
  return `${text.slice(0, maxLength)}...`
}

function getResultBadge(log: PromptAuditLog) {
  if (log.action === 'blocked') {
    return { variant: 'destructive' as const, labelKey: 'blocked' }
  }
  if (log.action === 'hit') {
    return { variant: 'secondary' as const, labelKey: 'Observed hit' }
  }
  return { variant: 'outline' as const, labelKey: log.action }
}

function getDisposalLabel(log: PromptAuditLog, t: (key: string) => string) {
  if (log.action === 'blocked') {
    return t('Intercepted this time')
  }
  if (log.action === 'hit') {
    return t('Observed only, not blocked')
  }
  return t(log.action)
}

export function ModerationAuditRecords({
  refreshNonce = 0,
}: {
  refreshNonce?: number
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [action, setAction] = useState('all')
  const [category, setCategory] = useState('all')
  const [keyword, setKeyword] = useState('')
  const deferredKeyword = useDeferredValue(keyword.trim())

  const logsQuery = useQuery({
    queryKey: [
      'security-audit',
      'moderation-logs',
      page,
      action,
      category,
      deferredKeyword,
    ],
    queryFn: () =>
      listContentModerationLogs({
        p: page,
        page_size: PAGE_SIZE,
        action: action === 'all' ? undefined : action,
        category: category === 'all' ? undefined : category,
        keyword: deferredKeyword || undefined,
      }),
    placeholderData: (previous) => previous,
  })
  const refetchLogs = logsQuery.refetch

  useEffect(() => {
    if (refreshNonce <= 0) return
    void refetchLogs()
  }, [refreshNonce, refetchLogs])

  const pageData = logsQuery.data?.data
  const logs = pageData?.items ?? []
  const total = pageData?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  const actionItems = [
    { value: 'all', label: t('All results') },
    { value: 'blocked', label: t('blocked') },
    { value: 'hit', label: t('Observed hit') },
  ]
  const categoryItems = [
    { value: 'all', label: t('All categories') },
    ...CONTENT_MODERATION_CATEGORIES.map((item) => ({
      value: item,
      label: t(item),
    })),
  ]

  return (
    <div className='bg-card ring-foreground/10 overflow-hidden rounded-xl ring-1'>
      <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='min-w-0'>
          <h3 className='text-sm font-medium'>{t('Audit Records')}</h3>
        </div>
        <Button
          variant='outline'
          size='sm'
          disabled={logsQuery.isFetching}
          onClick={() => void logsQuery.refetch()}
        >
          {logsQuery.isFetching ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <RefreshCw data-icon='inline-start' />
          )}
          {t('Refresh')}
        </Button>
      </div>

      <div className='bg-muted/40 flex flex-col gap-2 border-b px-4 py-3 lg:flex-row lg:items-center'>
        <Select
          items={actionItems}
          value={action}
          onValueChange={(value) => {
            if (!value) return
            setAction(value)
            setPage(1)
          }}
        >
          <SelectTrigger className='w-full lg:w-40'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {actionItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>

        <Select
          items={categoryItems}
          value={category}
          onValueChange={(value) => {
            if (!value) return
            setCategory(value)
            setPage(1)
          }}
        >
          <SelectTrigger className='w-full lg:w-48'>
            <SelectValue />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='all'>{t('All categories')}</SelectItem>
            </SelectGroup>
            <SelectSeparator />
            {CONTENT_MODERATION_CATEGORY_GROUPS.map((group, groupIndex) => (
              <Fragment key={group.label}>
                <SelectGroup>
                  <SelectLabel>{t(group.label)}</SelectLabel>
                  {group.categories.map((item) => (
                    <SelectItem key={item} value={item}>
                      {t(item)}
                    </SelectItem>
                  ))}
                </SelectGroup>
                {groupIndex < CONTENT_MODERATION_CATEGORY_GROUPS.length - 1 && (
                  <SelectSeparator />
                )}
              </Fragment>
            ))}
          </SelectContent>
        </Select>

        <div className='relative min-w-0 flex-1'>
          <Search className='text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
          <Input
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value)
              setPage(1)
            }}
            className='pl-8'
            placeholder={t('Search by user / key / summary')}
          />
        </div>
      </div>

      {logsQuery.isLoading ? (
        <div className='space-y-3 p-4'>
          {Array.from({ length: 6 }).map((_, index) => (
            <Skeleton key={index} className='h-10 w-full' />
          ))}
        </div>
      ) : logsQuery.isError ? (
        <Empty className='min-h-64 rounded-none'>
          <EmptyHeader>
            <EmptyTitle>{t('Failed to load')}</EmptyTitle>
          </EmptyHeader>
          <Button variant='outline' onClick={() => void logsQuery.refetch()}>
            {t('Retry')}
          </Button>
        </Empty>
      ) : logs.length === 0 ? (
        <Empty className='min-h-64 rounded-none'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Search />
            </EmptyMedia>
            <EmptyTitle>{t('No audit records')}</EmptyTitle>
            <EmptyDescription>
              {t('Content moderation results will appear here.')}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <>
          <div className='overflow-x-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='whitespace-nowrap'>
                    {t('Time')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('Model')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('User')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('API Key')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('Node')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('Result')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('Highest score')}
                  </TableHead>
                  <TableHead className='whitespace-nowrap'>
                    {t('Disposal')}
                  </TableHead>
                  <TableHead className='min-w-48'>
                    {t('Input summary')}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log) => {
                  const result = getResultBadge(log)
                  const highestCategory = getHighestCategory(log)
                  return (
                    <TableRow key={log.id}>
                      <TableCell className='text-muted-foreground text-xs whitespace-nowrap'>
                        {formatTimestampToDate(log.created_at, 'seconds')}
                      </TableCell>
                      <TableCell className='max-w-36 truncate text-sm'>
                        {log.model_name || '-'}
                      </TableCell>
                      <TableCell>
                        <div className='max-w-40'>
                          <div className='truncate text-sm font-medium'>
                            {log.username || `#${log.user_id}`}
                          </div>
                          <div className='text-muted-foreground text-xs'>
                            UID {log.user_id}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className='max-w-32 truncate text-sm'>
                        {log.token_name || '-'}
                      </TableCell>
                      <TableCell className='max-w-40 truncate font-mono text-xs'>
                        {log.endpoint || '-'}
                      </TableCell>
                      <TableCell>
                        <Badge variant={result.variant}>
                          {t(result.labelKey)}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-sm whitespace-nowrap'>
                        <span className='font-mono text-xs'>
                          {highestCategory === '-'
                            ? highestCategory
                            : t(highestCategory)}
                        </span>{' '}
                        <span className='text-muted-foreground'>
                          {formatScorePercent(log.score)}
                        </span>
                      </TableCell>
                      <TableCell className='max-w-40 text-sm'>
                        {getDisposalLabel(log, t)}
                      </TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1'>
                          <HoverCard>
                            <HoverCardTrigger
                              render={
                                <button
                                  type='button'
                                  className='hover:text-foreground max-w-64 min-w-0 cursor-pointer truncate text-left text-sm underline-offset-2 hover:underline'
                                >
                                  {summarizePrompt(log.prompt)}
                                </button>
                              }
                            />
                            <HoverCardContent
                              align='end'
                              side='left'
                              className='max-h-80 w-[28rem] max-w-[90vw] overflow-y-auto'
                            >
                              <pre className='font-sans text-xs leading-5 break-words whitespace-pre-wrap'>
                                {log.prompt}
                              </pre>
                            </HoverCardContent>
                          </HoverCard>
                          <CopyButton
                            value={log.prompt}
                            className='size-7'
                            tooltip={t('Copy to clipboard')}
                            successTooltip={t('Copied!')}
                            aria-label={t('Copy to clipboard')}
                          />
                        </div>
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
          </div>

          <div className='flex items-center justify-between border-t px-4 py-3'>
            <span className='text-muted-foreground text-xs'>
              {t('{{total}} records', { total })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1 || logsQuery.isFetching}
                onClick={() => setPage((current) => Math.max(1, current - 1))}
              >
                <ChevronLeft className='size-4' />
              </Button>
              <span className='text-muted-foreground text-xs'>
                {page} / {totalPages}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= totalPages || logsQuery.isFetching}
                onClick={() =>
                  setPage((current) => Math.min(totalPages, current + 1))
                }
              >
                <ChevronRight className='size-4' />
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
