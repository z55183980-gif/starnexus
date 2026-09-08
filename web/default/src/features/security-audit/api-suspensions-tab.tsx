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
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatTimestampToDate } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from '@/components/ui/input-group'
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
import { listSuspendedAPIUsers, restoreUserAPIAccess } from './api'
import type { SuspendedAPIUser } from './types'

const pageSize = 20

function getUserInitials(user: SuspendedAPIUser) {
  const label = user.display_name.trim() || user.username.trim()
  return label.slice(0, 2).toUpperCase() || String(user.id).slice(-2)
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

  return (
    <div className='flex flex-col gap-4'>
      <Alert>
        <HugeiconsIcon icon={AlertCircleIcon} strokeWidth={2} />
        <AlertTitle>{t('API-only suspension')}</AlertTitle>
        <AlertDescription>
          {t(
            'A structured upstream cyber policy error suspends token API calls while web console login remains available. The user is also added to prompt monitoring.'
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
                    {formatTimestampToDate(user.api_suspended_at)}
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
    </div>
  )
}
