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
import { useDeferredValue, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowReloadHorizontalIcon,
  AlertCircleIcon,
  Search01Icon,
  UserBlock01Icon,
  UserCheck01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Check, Eye, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
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
import { Textarea } from '@/components/ui/textarea'
import {
  getSecurityAuditBanConfig,
  listSuspendedAPIUsers,
  restoreUserAPIAccess,
  updateSecurityAuditBanConfig,
} from './api'
import type { SuspendedAPIUser } from './types'

const pageSize = 20
const durationOptions = [
  { value: '3600', labelKey: '1 hour' },
  { value: '7200', labelKey: '2 hours' },
  { value: '86400', labelKey: '1 day' },
  { value: '0', labelKey: 'Permanent' },
] as const

function getUserInitials(user: SuspendedAPIUser) {
  const label = user.display_name.trim() || user.username.trim()
  return label.slice(0, 2).toUpperCase() || String(user.id).slice(-2)
}

function formatEvidence(evidence: string) {
  if (!evidence.trim()) return '-'
  try {
    return JSON.stringify(JSON.parse(evidence), null, 2)
  } catch {
    return evidence
  }
}

export function APISuspensionsTab() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const deferredKeyword = useDeferredValue(keyword.trim())
  const [page, setPage] = useState(1)
  const [restoringUser, setRestoringUser] = useState<SuspendedAPIUser | null>(
    null
  )
  const [restoreReason, setRestoreReason] = useState('')
  const [isRestoring, setIsRestoring] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [channelPickerOpen, setChannelPickerOpen] = useState(false)
  const [channelSearch, setChannelSearch] = useState('')
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>([])
  const [durationSeconds, setDurationSeconds] = useState<
    0 | 3600 | 7200 | 86400
  >(0)
  const [isSavingSettings, setIsSavingSettings] = useState(false)
  const [viewingHit, setViewingHit] = useState<SuspendedAPIUser | null>(null)
  const queryKey = [
    'security-audit',
    'api-suspensions',
    page,
    deferredKeyword,
  ] as const
  const usersQuery = useQuery({
    queryKey,
    queryFn: () =>
      listSuspendedAPIUsers({
        p: page,
        page_size: pageSize,
        keyword: deferredKeyword || undefined,
      }),
  })
  const settingsQuery = useQuery({
    queryKey: ['security-audit', 'api-suspension-settings'],
    queryFn: getSecurityAuditBanConfig,
  })
  const settings = settingsQuery.data?.data
  const channels = settings?.channels ?? []
  const filteredChannels = channels.filter((channel) => {
    const search = channelSearch.trim().toLowerCase()
    return (
      !search ||
      channel.name.toLowerCase().includes(search) ||
      String(channel.id).includes(search)
    )
  })
  const result = usersQuery.data?.data
  const users = result?.items ?? []
  const total = result?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const handleRestore = async () => {
    if (!restoringUser || !restoreReason.trim()) return
    setIsRestoring(true)
    try {
      const response = await restoreUserAPIAccess(
        restoringUser.id,
        restoreReason.trim()
      )
      if (!response.success) {
        toast.error(response.message || t('Failed to restore API access'))
        return
      }
      toast.success(t('API access restored'))
      setRestoringUser(null)
      setRestoreReason('')
      await queryClient.invalidateQueries({
        queryKey: ['security-audit', 'api-suspensions'],
      })
    } catch {
      toast.error(t('Failed to restore API access'))
    } finally {
      setIsRestoring(false)
    }
  }

  const openSettings = () => {
    setSelectedChannelIds(settings?.channel_ids ?? [])
    setDurationSeconds(settings?.duration_seconds ?? 0)
    setChannelSearch('')
    setSettingsOpen(true)
  }

  const handleSaveSettings = async () => {
    setIsSavingSettings(true)
    try {
      const response = await updateSecurityAuditBanConfig({
        channel_ids: selectedChannelIds,
        duration_seconds: durationSeconds,
      })
      if (!response.success) {
        toast.error(response.message || t('Failed to save suspension settings'))
        return
      }
      toast.success(t('Suspension settings saved'))
      setSettingsOpen(false)
      await queryClient.invalidateQueries({
        queryKey: ['security-audit', 'api-suspension-settings'],
      })
    } catch {
      toast.error(t('Failed to save suspension settings'))
    } finally {
      setIsSavingSettings(false)
    }
  }

  return (
    <div className='flex flex-col gap-4'>
      <Alert>
        <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
        <AlertTitle>{t('API-only suspension')}</AlertTitle>
        <AlertDescription>
          {t(
            'A structured upstream cyber policy error from a configured channel suspends token API calls while web console login remains available. The user is also added to prompt monitoring.'
          )}
        </AlertDescription>
      </Alert>

      <div className='bg-card ring-foreground/10 overflow-hidden rounded-xl ring-1'>
        <div className='flex items-center justify-between gap-3 border-b px-4 py-3'>
          <div className='flex min-w-0 items-center gap-2'>
            <HugeiconsIcon
              icon={UserBlock01Icon}
              strokeWidth={2}
              className='text-muted-foreground'
            />
            <h3 className='truncate text-sm font-medium'>
              {t('API suspended users')}
            </h3>
            <Badge variant='secondary' className='rounded-full tabular-nums'>
              {total}
            </Badge>
          </div>
          <div className='flex items-center gap-2'>
            <Button
              variant='outline'
              size='sm'
              aria-label={t('Refresh')}
              disabled={usersQuery.isFetching}
              onClick={() => void usersQuery.refetch()}
            >
              {usersQuery.isFetching ? (
                <Spinner data-icon='inline-start' />
              ) : (
                <HugeiconsIcon
                  icon={ArrowReloadHorizontalIcon}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
              )}
              <span className='hidden sm:inline'>{t('Refresh')}</span>
            </Button>
            <Button
              size='sm'
              disabled={settingsQuery.isLoading || settingsQuery.isError}
              onClick={openSettings}
            >
              <Settings2 data-icon='inline-start' />
              <span className='hidden sm:inline'>
                {t('Suspension settings')}
              </span>
            </Button>
          </div>
        </div>

        <div className='bg-muted/40 border-b px-4 py-3'>
          <Field className='sm:max-w-sm'>
            <FieldLabel htmlFor='api-suspension-search' className='sr-only'>
              {t('Search username, display name, or email')}
            </FieldLabel>
            <InputGroup>
              <InputGroupAddon>
                <HugeiconsIcon icon={Search01Icon} strokeWidth={2} />
              </InputGroupAddon>
              <InputGroupInput
                id='api-suspension-search'
                value={keyword}
                onChange={(event) => {
                  setKeyword(event.target.value)
                  setPage(1)
                }}
                placeholder={t('Search username, display name, or email')}
              />
            </InputGroup>
          </Field>
        </div>

        {usersQuery.isLoading ? (
          <div className='flex flex-col gap-3 p-4'>
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className='h-14 w-full' />
            ))}
          </div>
        ) : usersQuery.isError ? (
          <Empty className='min-h-72 rounded-none'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
              </EmptyMedia>
              <EmptyTitle>{t('Failed to load')}</EmptyTitle>
              <EmptyDescription>
                {t('Could not load suspended API users')}
              </EmptyDescription>
            </EmptyHeader>
            <Button variant='outline' onClick={() => void usersQuery.refetch()}>
              {t('Retry')}
            </Button>
          </Empty>
        ) : users.length === 0 ? (
          <Empty className='min-h-72 rounded-none'>
            <EmptyHeader>
              <EmptyMedia variant='icon'>
                <HugeiconsIcon icon={UserCheck01Icon} strokeWidth={2} />
              </EmptyMedia>
              <EmptyTitle>{t('No suspended API users')}</EmptyTitle>
              <EmptyDescription>
                {t(
                  'Users suspended by cyber policy detection will appear here.'
                )}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <Table className='min-w-[860px]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Suspended at')}</TableHead>
                <TableHead>{t('Trigger evidence')}</TableHead>
                <TableHead>{t('Prompt monitoring')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.map((user) => (
                <TableRow key={user.id}>
                  <TableCell>
                    <div className='flex min-w-44 items-center gap-2.5'>
                      <Avatar size='sm'>
                        <AvatarFallback>{getUserInitials(user)}</AvatarFallback>
                      </Avatar>
                      <div className='min-w-0'>
                        <div className='truncate font-medium'>
                          {user.username}
                        </div>
                        <div className='text-muted-foreground truncate text-xs'>
                          {user.display_name || user.email || '-'} · UID{' '}
                          {user.id}
                        </div>
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs whitespace-nowrap'>
                    <div className='flex flex-col gap-1'>
                      <span>
                        {formatTimestampToDate(user.api_suspended_at)}
                      </span>
                      <span>
                        {user.api_suspended_until > 0
                          ? `${t('Until')} ${formatTimestampToDate(user.api_suspended_until)}`
                          : t('Permanent')}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className='flex min-w-56 flex-col gap-1 text-xs'>
                      <div className='flex items-center gap-2'>
                        <Badge variant='destructive'>cyber_policy</Badge>
                        <span className='max-w-36 truncate font-medium'>
                          {user.model_name || '-'}
                        </span>
                      </div>
                      <span className='text-muted-foreground'>
                        {t('Channel')} #{user.channel_id || '-'} · {t('Token')}{' '}
                        #{user.token_id || '-'}
                      </span>
                      <span className='text-muted-foreground'>
                        {t('Account')} #{user.upstream_account_id || '-'} ·{' '}
                        {t('Node')} {user.node_name || '-'}
                      </span>
                      <span
                        className='text-muted-foreground max-w-64 truncate font-mono'
                        title={user.request_id || undefined}
                      >
                        {user.request_id || '-'}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        user.prompt_monitoring_added ? 'secondary' : 'outline'
                      }
                      className='rounded-full'
                    >
                      {user.prompt_monitoring_added
                        ? t('Enabled')
                        : t('Not added')}
                    </Badge>
                  </TableCell>
                  <TableCell className='text-right'>
                    <div className='flex justify-end gap-2'>
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => setViewingHit(user)}
                      >
                        <Eye data-icon='inline-start' />
                        {t('Hit details')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => {
                          setRestoringUser(user)
                          setRestoreReason('')
                        }}
                      >
                        {t('Restore API access')}
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        {total > 0 && (
          <div className='flex items-center justify-between gap-3 border-t px-4 py-3'>
            <span className='text-muted-foreground text-xs'>
              {t('{{total}} suspended users', { total })}
            </span>
            <div className='flex items-center gap-2'>
              <Button
                variant='outline'
                size='sm'
                disabled={page <= 1 || usersQuery.isFetching}
                onClick={() => setPage((value) => Math.max(1, value - 1))}
              >
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-xs tabular-nums'>
                {page} / {totalPages}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={page >= totalPages || usersQuery.isFetching}
                onClick={() => setPage((value) => value + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        )}
      </div>

      <Dialog
        open={restoringUser !== null}
        onOpenChange={(open) => {
          if (!open && !isRestoring) {
            setRestoringUser(null)
            setRestoreReason('')
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Restore API access?')}</DialogTitle>
            <DialogDescription>
              {t(
                'Token API calls for {{username}} will be allowed again. Prompt monitoring remains enabled.',
                { username: restoringUser?.username ?? '' }
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='api-restore-reason'>
                {t('Restore reason')}
              </FieldLabel>
              <Textarea
                id='api-restore-reason'
                value={restoreReason}
                maxLength={255}
                placeholder={t('Enter the review conclusion or restore reason')}
                onChange={(event) => setRestoreReason(event.target.value)}
              />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button
              variant='outline'
              disabled={isRestoring}
              onClick={() => {
                setRestoringUser(null)
                setRestoreReason('')
              }}
            >
              {t('Cancel')}
            </Button>
            <Button
              disabled={isRestoring || !restoreReason.trim()}
              onClick={() => void handleRestore()}
            >
              {isRestoring && <Spinner data-icon='inline-start' />}
              {t('Restore')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={settingsOpen}
        onOpenChange={(open) => !isSavingSettings && setSettingsOpen(open)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('API suspension settings')}</DialogTitle>
            <DialogDescription>
              {t(
                'Only structured cyber policy errors from the selected channels suspend users. Leave all channels unselected to disable automatic suspension.'
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel>{t('Effective channels')}</FieldLabel>
              <Popover
                open={channelPickerOpen}
                onOpenChange={setChannelPickerOpen}
              >
                <PopoverTrigger
                  render={
                    <Button
                      type='button'
                      variant='outline'
                      className='w-full justify-between font-normal'
                    />
                  }
                >
                  {selectedChannelIds.length > 0
                    ? t('{{count}} channels selected', {
                        count: selectedChannelIds.length,
                      })
                    : t('Select channels')}
                </PopoverTrigger>
                <PopoverContent className='w-[var(--anchor-width)] p-0'>
                  <Command shouldFilter={false}>
                    <CommandInput
                      value={channelSearch}
                      onValueChange={setChannelSearch}
                      placeholder={t('Search channels...')}
                    />
                    <CommandList className='max-h-64'>
                      <CommandEmpty>{t('No channels found')}</CommandEmpty>
                      <CommandGroup>
                        {filteredChannels.map((channel) => {
                          const selected = selectedChannelIds.includes(
                            channel.id
                          )
                          return (
                            <CommandItem
                              key={channel.id}
                              value={String(channel.id)}
                              onSelect={() =>
                                setSelectedChannelIds((current) =>
                                  selected
                                    ? current.filter((id) => id !== channel.id)
                                    : [...current, channel.id]
                                )
                              }
                            >
                              <Check
                                className={
                                  selected ? 'opacity-100' : 'opacity-0'
                                }
                              />
                              <span className='truncate'>
                                {channel.name} (#{channel.id})
                              </span>
                            </CommandItem>
                          )
                        })}
                      </CommandGroup>
                    </CommandList>
                  </Command>
                </PopoverContent>
              </Popover>
              <FieldDescription>
                {t('Only the selected channels can trigger API suspension.')}
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel>{t('Suspension duration')}</FieldLabel>
              <Select
                items={durationOptions.map((option) => ({
                  value: option.value,
                  label: t(option.labelKey),
                }))}
                value={String(durationSeconds)}
                onValueChange={(value) =>
                  setDurationSeconds(Number(value) as 0 | 3600 | 7200 | 86400)
                }
              >
                <SelectTrigger className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {durationOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {t(option.labelKey)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button
              variant='outline'
              disabled={isSavingSettings}
              onClick={() => setSettingsOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              disabled={isSavingSettings}
              onClick={() => void handleSaveSettings()}
            >
              {isSavingSettings && <Spinner data-icon='inline-start' />}
              {t('Save changes')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={viewingHit !== null}
        onOpenChange={(open) => !open && setViewingHit(null)}
      >
        <DialogContent className='sm:max-w-2xl'>
          <DialogHeader>
            <DialogTitle>{t('Suspension hit details')}</DialogTitle>
            <DialogDescription>
              {t('Latest trigger that suspended API access for {{username}}.', {
                username: viewingHit?.username ?? '',
              })}
            </DialogDescription>
          </DialogHeader>
          {viewingHit && (
            <dl className='grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm'>
              <dt className='text-muted-foreground'>{t('Hit time')}</dt>
              <dd>
                {formatTimestampToDate(viewingHit.api_suspended_at, 'seconds')}
              </dd>
              <dt className='text-muted-foreground'>{t('Channel')}</dt>
              <dd>#{viewingHit.channel_id || '-'}</dd>
              <dt className='text-muted-foreground'>{t('Model')}</dt>
              <dd>{viewingHit.model_name || '-'}</dd>
              <dt className='text-muted-foreground'>{t('Request ID')}</dt>
              <dd className='font-mono break-all'>
                {viewingHit.request_id || '-'}
              </dd>
              <dt className='text-muted-foreground'>{t('Token')}</dt>
              <dd>#{viewingHit.token_id || '-'}</dd>
              <dt className='text-muted-foreground'>{t('Upstream account')}</dt>
              <dd>#{viewingHit.upstream_account_id || '-'}</dd>
              <dt className='text-muted-foreground'>{t('Node')}</dt>
              <dd>{viewingHit.node_name || '-'}</dd>
              <dt className='text-muted-foreground'>
                {t('Suspension expires')}
              </dt>
              <dd>
                {viewingHit.api_suspended_until > 0
                  ? formatTimestampToDate(
                      viewingHit.api_suspended_until,
                      'seconds'
                    )
                  : t('Permanent')}
              </dd>
              <dt className='text-muted-foreground'>
                {t('Upstream evidence')}
              </dt>
              <dd>
                <pre className='bg-muted max-h-64 overflow-auto rounded-lg p-3 text-xs break-words whitespace-pre-wrap'>
                  {formatEvidence(viewingHit.evidence)}
                </pre>
              </dd>
            </dl>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => setViewingHit(null)}>
              {t('Close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
