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
import { useDeferredValue, useEffect, useMemo, useRef, useState } from 'react'
import {
  type InfiniteData,
  useInfiniteQuery,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query'
import {
  ChevronDown,
  Eye,
  LoaderCircle,
  Plus,
  RefreshCw,
  Search,
  Settings2,
  ShieldCheck,
  Trash2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { SectionPageLayout } from '@/components/layout'
import { searchUsers } from '@/features/users/api'
import {
  clearPromptAuditLogs,
  createPromptAuditPolicy,
  deletePromptAuditPolicy,
  listPromptAuditLogsAfter,
  listPromptAuditLogsBefore,
  listPromptAuditPolicies,
  updatePromptAuditPolicy,
} from './api'
import { ContentModerationTab } from './content-moderation-tab'
import type {
  PromptAuditLog,
  PromptAuditLogCursorResponse,
  PromptAuditPolicy,
} from './types'

const policiesQueryKey = ['security-audit', 'policies'] as const
const delaySecondsOptions = [3, 5, 9, 15, 20, 30, 45] as const

function getActionBadge(log: PromptAuditLog) {
  if (log.action === 'blocked') return 'destructive' as const
  if (log.action === 'delayed') return 'secondary' as const
  if (log.hit) return 'outline' as const
  return 'ghost' as const
}

function AddUserPopover({
  excludedUserIds,
  onCreated,
}: {
  excludedUserIds: Set<number>
  onCreated: () => Promise<void>
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [creatingUserId, setCreatingUserId] = useState<number | null>(null)
  const deferredKeyword = useDeferredValue(keyword.trim())
  const usersQuery = useQuery({
    queryKey: ['security-audit', 'users', deferredKeyword],
    queryFn: () => searchUsers({ keyword: deferredKeyword, page_size: 20 }),
    enabled: open,
    staleTime: 15_000,
  })
  const users = (usersQuery.data?.data?.items ?? []).filter(
    (user) => !excludedUserIds.has(user.id)
  )

  const handleSelect = async (userId: number) => {
    setCreatingUserId(userId)
    try {
      const response = await createPromptAuditPolicy(userId)
      if (!response.success) {
        toast.error(response.message || t('Failed to add audit user'))
        return
      }
      toast.success(t('Audit user added'))
      setOpen(false)
      setKeyword('')
      await onCreated()
    } catch {
      toast.error(t('Failed to add audit user'))
    } finally {
      setCreatingUserId(null)
    }
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        render={
          <Button size='sm'>
            <Plus className='size-4' />
            {t('Add monitored user')}
          </Button>
        }
      />
      <PopoverContent className='w-80 p-0' align='end'>
        <Command shouldFilter={false}>
          <CommandInput
            placeholder={t('Search username...')}
            value={keyword}
            onValueChange={setKeyword}
          />
          <CommandEmpty>
            {usersQuery.isFetching ? t('Loading...') : t('No users found.')}
          </CommandEmpty>
          <CommandList>
            <CommandGroup>
              {users.map((user) => (
                <CommandItem
                  key={user.id}
                  value={`${user.username} ${user.display_name} ${user.id}`}
                  disabled={creatingUserId !== null}
                  onSelect={() => void handleSelect(user.id)}
                >
                  <div className='min-w-0 flex-1'>
                    <div className='truncate font-medium'>{user.username}</div>
                    <div className='text-muted-foreground truncate text-xs'>
                      {user.display_name || '-'} · #{user.id}
                    </div>
                  </div>
                  {creatingUserId === user.id && (
                    <LoaderCircle className='size-4 animate-spin' />
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  )
}

function PromptLogSheet({
  policy,
  open,
  onOpenChange,
}: {
  policy: PromptAuditPolicy | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [clearDialogOpen, setClearDialogOpen] = useState(false)
  const [isClearing, setIsClearing] = useState(false)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const refreshCursorRef = useRef(0)
  const logsQueryKey = ['security-audit', 'prompts', policy?.user_id] as const
  const logsQuery = useInfiniteQuery({
    queryKey: logsQueryKey,
    queryFn: ({ pageParam }) =>
      listPromptAuditLogsBefore(policy!.user_id, pageParam, 50),
    enabled: open && policy !== null,
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      const page = lastPage.data
      if (!page?.has_more) return undefined
      return page.next_cursor
    },
  })
  const logs = useMemo(
    () => logsQuery.data?.pages.flatMap((page) => page.data?.items ?? []) ?? [],
    [logsQuery.data?.pages]
  )

  useEffect(() => {
    refreshCursorRef.current = 0
  }, [policy?.user_id])

  useEffect(() => {
    const latestId = logs.reduce((maximum, log) => Math.max(maximum, log.id), 0)
    refreshCursorRef.current = Math.max(refreshCursorRef.current, latestId)
  }, [logs])

  const handleRefresh = async () => {
    if (!policy || isRefreshing) return
    setIsRefreshing(true)
    try {
      let cursor = refreshCursorRef.current
      let hasMore = true
      const incoming: PromptAuditLog[] = []
      while (hasMore) {
        const response = await listPromptAuditLogsAfter(policy.user_id, cursor)
        const page = response.data
        if (!page) break
        incoming.push(...page.items)
        if (page.next_cursor <= cursor && page.has_more) {
          throw new Error('Prompt audit cursor did not advance')
        }
        cursor = Math.max(cursor, page.next_cursor)
        hasMore = page.has_more
      }
      refreshCursorRef.current = cursor

      if (incoming.length > 0) {
        queryClient.setQueryData<
          InfiniteData<PromptAuditLogCursorResponse, number>
        >(logsQueryKey, (current) => {
          if (!current || current.pages.length === 0) return current
          const knownIds = new Set(
            current.pages.flatMap((page) =>
              (page.data?.items ?? []).map((log) => log.id)
            )
          )
          const additions = incoming
            .filter((log) => !knownIds.has(log.id))
            .sort(
              (left, right) =>
                right.created_at - left.created_at || right.id - left.id
            )
          if (additions.length === 0) return current

          const firstPage = current.pages[0]
          if (!firstPage.data) return current
          return {
            ...current,
            pages: [
              {
                ...firstPage,
                data: {
                  ...firstPage.data,
                  items: [...additions, ...firstPage.data.items],
                },
              },
              ...current.pages.slice(1),
            ],
          }
        })
      }
    } catch {
      toast.error(t('Failed to load'))
    } finally {
      setIsRefreshing(false)
    }
  }

  const handleClear = async () => {
    if (!policy) return
    setIsClearing(true)
    try {
      const response = await clearPromptAuditLogs(policy.user_id)
      if (!response.success) {
        toast.error(response.message || t('Failed to clear prompt records'))
        return
      }
      toast.success(t('Prompt records cleared'))
      setClearDialogOpen(false)
      refreshCursorRef.current = 0
      queryClient.setQueryData<
        InfiniteData<PromptAuditLogCursorResponse, number>
      >(logsQueryKey, (current) => {
        const firstPage = current?.pages[0]
        if (!current || !firstPage?.data) return current
        return {
          pages: [
            {
              ...firstPage,
              data: {
                ...firstPage.data,
                items: [],
                has_more: false,
                next_cursor: 0,
              },
            },
          ],
          pageParams: [0],
        }
      })
    } catch {
      toast.error(t('Failed to clear prompt records'))
    } finally {
      setIsClearing(false)
    }
  }

  return (
    <>
      <Sheet open={open} onOpenChange={onOpenChange}>
        <SheetContent className='w-full sm:max-w-2xl'>
          <SheetHeader className='border-b pr-12'>
            <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
              <div className='min-w-0'>
                <SheetTitle>
                  {t('Prompt records for {{username}}', {
                    username: policy?.username ?? '',
                  })}
                </SheetTitle>
                <SheetDescription>
                  {t('Latest user prompts are shown first')}
                </SheetDescription>
              </div>
              <div className='flex shrink-0 items-center gap-2 self-end sm:self-auto'>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={logsQuery.isLoading || isClearing || isRefreshing}
                  onClick={() => void handleRefresh()}
                >
                  {isRefreshing ? (
                    <Spinner data-icon='inline-start' />
                  ) : (
                    <RefreshCw data-icon='inline-start' />
                  )}
                  {t('Refresh')}
                </Button>
                <Button
                  variant='destructive'
                  size='sm'
                  disabled={
                    logsQuery.isLoading ||
                    logs.length === 0 ||
                    isClearing ||
                    isRefreshing
                  }
                  onClick={() => setClearDialogOpen(true)}
                >
                  <Trash2 data-icon='inline-start' />
                  {t('Clear')}
                </Button>
              </div>
            </div>
          </SheetHeader>
          <div className='min-h-0 flex-1 overflow-y-auto px-4 pb-4'>
            {logsQuery.isLoading ? (
              <div className='space-y-4 pt-2'>
                {Array.from({ length: 5 }).map((_, index) => (
                  <div key={index} className='space-y-2 border-b pb-4'>
                    <Skeleton className='h-4 w-48' />
                    <Skeleton className='h-16 w-full' />
                  </div>
                ))}
              </div>
            ) : logsQuery.isError ? (
              <Empty className='min-h-72'>
                <EmptyHeader>
                  <EmptyTitle>{t('Failed to load')}</EmptyTitle>
                </EmptyHeader>
                <Button
                  variant='outline'
                  onClick={() => void logsQuery.refetch()}
                >
                  {t('Retry')}
                </Button>
              </Empty>
            ) : logs.length === 0 ? (
              <Empty className='min-h-72'>
                <EmptyHeader>
                  <EmptyMedia variant='icon'>
                    <Search />
                  </EmptyMedia>
                  <EmptyTitle>{t('No prompt records')}</EmptyTitle>
                  <EmptyDescription>
                    {t('Recorded user prompts will appear here.')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <>
                <ol className='divide-y'>
                  {logs.map((log) => (
                    <li key={log.id} className='py-4'>
                      <div className='mb-2 flex flex-wrap items-center gap-2'>
                        <time className='text-muted-foreground text-xs'>
                          {formatTimestampToDate(log.created_at, 'seconds')}
                        </time>
                        <Badge variant={getActionBadge(log)}>
                          {t(log.action)}
                        </Badge>
                        {log.model_name && (
                          <span className='text-muted-foreground text-xs'>
                            {log.model_name}
                          </span>
                        )}
                        {log.truncated && (
                          <Badge variant='outline'>{t('Truncated')}</Badge>
                        )}
                      </div>
                      <pre className='font-sans text-sm leading-6 break-words whitespace-pre-wrap'>
                        {log.prompt}
                      </pre>
                    </li>
                  ))}
                </ol>
                {logsQuery.hasNextPage && (
                  <div className='flex justify-center border-t pt-4'>
                    <Button
                      variant='outline'
                      size='sm'
                      disabled={logsQuery.isFetchingNextPage}
                      onClick={() => void logsQuery.fetchNextPage()}
                    >
                      {logsQuery.isFetchingNextPage ? (
                        <LoaderCircle className='size-4 animate-spin' />
                      ) : (
                        <ChevronDown className='size-4' />
                      )}
                      {t('More')}
                    </Button>
                  </div>
                )}
              </>
            )}
          </div>
        </SheetContent>
      </Sheet>

      <AlertDialog open={clearDialogOpen} onOpenChange={setClearDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Clear all prompt records?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This deletes all existing prompt records for {{username}}. Monitoring stays enabled and new records will continue to be added.',
                { username: policy?.username ?? '' }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={isClearing}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={isClearing}
              onClick={(event) => {
                event.preventDefault()
                void handleClear()
              }}
            >
              {isClearing && (
                <LoaderCircle
                  data-icon='inline-start'
                  className='animate-spin'
                />
              )}
              {t('Clear')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

export function SecurityAudit() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [view, setView] = useState<'policies' | 'moderation'>('moderation')
  const [moderationSettingsOpen, setModerationSettingsOpen] = useState(false)
  const [moderationRefreshNonce, setModerationRefreshNonce] = useState(0)
  const [viewingPolicy, setViewingPolicy] = useState<PromptAuditPolicy | null>(
    null
  )
  const [deletingPolicy, setDeletingPolicy] =
    useState<PromptAuditPolicy | null>(null)
  const [pendingPolicyId, setPendingPolicyId] = useState<number | null>(null)

  const policiesQuery = useQuery({
    queryKey: policiesQueryKey,
    queryFn: listPromptAuditPolicies,
  })
  const policies = useMemo(
    () => policiesQuery.data?.data ?? [],
    [policiesQuery.data?.data]
  )
  const excludedUserIds = useMemo(
    () => new Set(policies.map((policy) => policy.user_id)),
    [policies]
  )

  const refreshPolicies = async () => {
    await queryClient.invalidateQueries({ queryKey: policiesQueryKey })
  }

  const updatePolicy = async (
    policy: PromptAuditPolicy,
    changes: Partial<
      Pick<
        PromptAuditPolicy,
        'monitor_enabled' | 'delay_on_hit' | 'delay_seconds' | 'block_on_hit'
      >
    >
  ) => {
    setPendingPolicyId(policy.id)
    try {
      const response = await updatePromptAuditPolicy(policy.id, changes)
      if (!response.success) {
        toast.error(response.message || t('Failed to update audit policy'))
        return
      }
      await refreshPolicies()
    } catch {
      toast.error(t('Failed to update audit policy'))
    } finally {
      setPendingPolicyId(null)
    }
  }

  const handleDelete = async () => {
    if (!deletingPolicy) return
    setPendingPolicyId(deletingPolicy.id)
    try {
      const response = await deletePromptAuditPolicy(deletingPolicy.id)
      if (!response.success) {
        toast.error(response.message || t('Failed to delete audit user'))
        return
      }
      toast.success(t('Audit user deleted'))
      setDeletingPolicy(null)
      await refreshPolicies()
    } catch {
      toast.error(t('Failed to delete audit user'))
    } finally {
      setPendingPolicyId(null)
    }
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Security Audit')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {view === 'moderation'
            ? t('Configure content audit policies and view audit records')
            : t(
                'Monitor selected users with local sensitive-word rules after the upstream account-risk audit.'
              )}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          {view === 'moderation' ? (
            <>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setModerationRefreshNonce((value) => value + 1)}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Refresh status')}
              </Button>
              <Button size='sm' onClick={() => setModerationSettingsOpen(true)}>
                <Settings2 data-icon='inline-start' />
                {t('Content audit settings')}
              </Button>
            </>
          ) : (
            <AddUserPopover
              excludedUserIds={excludedUserIds}
              onCreated={refreshPolicies}
            />
          )}
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Tabs
            value={view}
            onValueChange={(value) => {
              const nextView = value as 'policies' | 'moderation'
              setView(nextView)
              if (nextView === 'policies') {
                void policiesQuery.refetch()
              }
            }}
          >
            <TabsList variant='line'>
              <TabsTrigger value='moderation'>
                {t('Content Moderation (API)')}
              </TabsTrigger>
              <TabsTrigger value='policies'>
                {t('User behavior rules')}
              </TabsTrigger>
            </TabsList>
            <TabsContent value='policies' className='mt-4 flex flex-col gap-4'>
              <Alert>
                <AlertTitle>
                  {t('User behavior rules run after API content audit')}
                </AlertTitle>
                <AlertDescription>
                  {t(
                    'API content audit always runs first. These local rules only add per-user monitoring, delay, or blocking and do not replace upstream account-risk classification.'
                  )}
                </AlertDescription>
              </Alert>
              <div className='overflow-hidden rounded-lg border'>
                {policiesQuery.isLoading ? (
                  <div className='space-y-3 p-4'>
                    {Array.from({ length: 5 }).map((_, index) => (
                      <Skeleton key={index} className='h-12 w-full' />
                    ))}
                  </div>
                ) : policiesQuery.isError ? (
                  <Empty className='min-h-72 rounded-none'>
                    <EmptyHeader>
                      <EmptyTitle>{t('Failed to load')}</EmptyTitle>
                    </EmptyHeader>
                    <Button
                      variant='outline'
                      onClick={() => void policiesQuery.refetch()}
                    >
                      {t('Retry')}
                    </Button>
                  </Empty>
                ) : policies.length === 0 ? (
                  <Empty className='min-h-72 rounded-none'>
                    <EmptyHeader>
                      <EmptyMedia variant='icon'>
                        <ShieldCheck />
                      </EmptyMedia>
                      <EmptyTitle>{t('No monitored users')}</EmptyTitle>
                      <EmptyDescription>
                        {t('Add a user to start monitoring prompts.')}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('User')}</TableHead>
                        <TableHead className='text-center'>
                          {t('Monitor local rules')}
                        </TableHead>
                        <TableHead className='text-center'>
                          {t('Delay on local hit')}
                        </TableHead>
                        <TableHead className='text-center'>
                          {t('Block on local hit')}
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {policies.map((policy) => {
                        const disabled = pendingPolicyId === policy.id
                        return (
                          <TableRow key={policy.id}>
                            <TableCell>
                              <div className='max-w-72 min-w-40'>
                                <div className='flex items-center gap-2'>
                                  <span className='truncate font-medium'>
                                    {policy.username || `#${policy.user_id}`}
                                  </span>
                                  {policy.created_by === 0 && (
                                    <Badge
                                      variant='secondary'
                                      className='shrink-0'
                                    >
                                      {t('System created')}
                                    </Badge>
                                  )}
                                </div>
                                <div className='text-muted-foreground truncate text-xs'>
                                  {policy.display_name ||
                                    policy.email ||
                                    `#${policy.user_id}`}
                                </div>
                              </div>
                            </TableCell>
                            <TableCell className='text-center'>
                              <Switch
                                checked={policy.monitor_enabled}
                                disabled={disabled}
                                aria-label={t(
                                  'Monitor prompts for {{username}}',
                                  {
                                    username: policy.username,
                                  }
                                )}
                                onCheckedChange={(checked) =>
                                  void updatePolicy(policy, {
                                    monitor_enabled: checked,
                                  })
                                }
                              />
                            </TableCell>
                            <TableCell>
                              <div className='flex items-center justify-center gap-2'>
                                <Switch
                                  checked={policy.delay_on_hit}
                                  disabled={disabled}
                                  aria-label={t(
                                    'Delay sensitive prompts for {{username}}',
                                    {
                                      username: policy.username,
                                    }
                                  )}
                                  onCheckedChange={(checked) =>
                                    void updatePolicy(policy, {
                                      delay_on_hit: checked,
                                      ...(checked
                                        ? { block_on_hit: false }
                                        : {}),
                                    })
                                  }
                                />
                                <Select
                                  items={delaySecondsOptions.map((seconds) => ({
                                    value: String(seconds),
                                    label: `${seconds} ${t('seconds')}`,
                                  }))}
                                  value={String(policy.delay_seconds || 3)}
                                  onValueChange={(value) =>
                                    void updatePolicy(policy, {
                                      delay_seconds: Number(value),
                                    })
                                  }
                                >
                                  <SelectTrigger
                                    size='sm'
                                    className='w-20'
                                    disabled={disabled}
                                    aria-label={`${t('Delay on hit')}: ${policy.username}`}
                                  >
                                    <SelectValue />
                                  </SelectTrigger>
                                  <SelectContent alignItemWithTrigger={false}>
                                    <SelectGroup>
                                      {delaySecondsOptions.map((seconds) => (
                                        <SelectItem
                                          key={seconds}
                                          value={String(seconds)}
                                        >
                                          {seconds} {t('seconds')}
                                        </SelectItem>
                                      ))}
                                    </SelectGroup>
                                  </SelectContent>
                                </Select>
                              </div>
                            </TableCell>
                            <TableCell className='text-center'>
                              <Switch
                                checked={policy.block_on_hit}
                                disabled={disabled}
                                aria-label={t(
                                  'Block sensitive prompts for {{username}}',
                                  {
                                    username: policy.username,
                                  }
                                )}
                                onCheckedChange={(checked) =>
                                  void updatePolicy(policy, {
                                    block_on_hit: checked,
                                    ...(checked ? { delay_on_hit: false } : {}),
                                  })
                                }
                              />
                            </TableCell>
                            <TableCell>
                              <div className='flex justify-end gap-1'>
                                <Button
                                  variant='ghost'
                                  size='sm'
                                  onClick={() => setViewingPolicy(policy)}
                                >
                                  <Eye className='size-4' />
                                  {t('View')}
                                </Button>
                                <Button
                                  variant='ghost'
                                  size='icon-sm'
                                  disabled={disabled}
                                  aria-label={t('Delete audit user')}
                                  onClick={() => setDeletingPolicy(policy)}
                                >
                                  <Trash2 className='size-4' />
                                </Button>
                              </div>
                            </TableCell>
                          </TableRow>
                        )
                      })}
                    </TableBody>
                  </Table>
                )}
              </div>
            </TabsContent>
            <TabsContent value='moderation' className='mt-4'>
              <ContentModerationTab
                settingsOpen={moderationSettingsOpen}
                onSettingsOpenChange={setModerationSettingsOpen}
                refreshNonce={moderationRefreshNonce}
              />
            </TabsContent>
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PromptLogSheet
        policy={viewingPolicy}
        open={viewingPolicy !== null}
        onOpenChange={(open) => !open && setViewingPolicy(null)}
      />

      <AlertDialog
        open={deletingPolicy !== null}
        onOpenChange={(open) => !open && setDeletingPolicy(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete audit user?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This permanently deletes the audit policy and all prompt records for {{username}}.',
                { username: deletingPolicy?.username ?? '' }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pendingPolicyId !== null}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              disabled={pendingPolicyId !== null}
              onClick={() => void handleDelete()}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
