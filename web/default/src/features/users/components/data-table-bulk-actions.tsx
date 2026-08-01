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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { type Table } from '@tanstack/react-table'
import {
  Delete02Icon,
  UserBlock01Icon,
  UserCheck01Icon,
  UserDollarIcon,
  UserGroupIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getCurrencyDisplay, getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars } from '@/lib/format'
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
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import {
  adjustUserQuota,
  batchUpdateUserGroup,
  deleteUser,
  getGroups,
  manageUser,
} from '../api'
import { USER_ROLE } from '../constants'
import { getBulkUserStatusTargets } from '../lib/bulk-user-status-actions'
import { type ApiResponse, type QuotaAdjustMode, type User } from '../types'
import { useUsers } from './users-provider'

interface DataTableBulkActionsProps {
  table: Table<User>
}

type ConfirmationAction = 'enable' | 'disable' | 'delete'
type RunningAction = ConfirmationAction | 'quota' | 'group' | null

function getRejectedMessage(reason: unknown): string | undefined {
  return reason instanceof Error ? reason.message : undefined
}

export function DataTableBulkActions({ table }: DataTableBulkActionsProps) {
  const { t } = useTranslation()
  const { triggerRefresh } = useUsers()
  const [confirmationAction, setConfirmationAction] =
    useState<ConfirmationAction | null>(null)
  const [quotaDialogOpen, setQuotaDialogOpen] = useState(false)
  const [groupDialogOpen, setGroupDialogOpen] = useState(false)
  const [runningAction, setRunningAction] = useState<RunningAction>(null)
  const [quotaMode, setQuotaMode] = useState<QuotaAdjustMode>('add')
  const [amount, setAmount] = useState('')
  const [selectedGroup, setSelectedGroup] = useState('')

  const selectedUsers = table
    .getFilteredSelectedRowModel()
    .rows.map((row) => row.original)
  const selectedIds = selectedUsers.map((user) => user.id)
  const selectedCount = selectedUsers.length
  const statusTargets = getBulkUserStatusTargets(selectedUsers)
  const enableTargets = statusTargets.enable
  const disableTargets = statusTargets.disable
  const hasRootSelected = selectedUsers.some(
    (user) => user.role === USER_ROLE.ROOT
  )
  const isProcessing = runningAction !== null

  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    enabled: selectedCount > 0,
  })
  const groups = useMemo(() => groupsData?.data ?? [], [groupsData?.data])
  const groupItems = useMemo(
    () => groups.map((group) => ({ label: group, value: group })),
    [groups]
  )

  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyLabel = getCurrencyLabel()
  const tokensOnly = currencyMeta.kind === 'tokens'
  const amountNumber = Number(amount)
  const quotaValue = parseQuotaFromDollars(amountNumber)
  const quotaIsValid =
    amount.trim() !== '' &&
    Number.isFinite(amountNumber) &&
    (quotaMode === 'override' ? true : amountNumber > 0 && quotaValue > 0)

  const clearAfterSuccess = () => {
    table.resetRowSelection()
    triggerRefresh()
  }

  const runExistingOperations = async (
    action: Exclude<RunningAction, 'group' | null>,
    targetUsers: User[],
    operation: (user: User) => Promise<ApiResponse>
  ) => {
    if (targetUsers.length === 0) return 0

    setRunningAction(action)
    try {
      const results = await Promise.allSettled(targetUsers.map(operation))
      const successfulCount = results.filter(
        (result) => result.status === 'fulfilled' && result.value.success
      ).length
      const failedResult = results.find(
        (result) => result.status === 'rejected' || !result.value.success
      )

      if (successfulCount === targetUsers.length) {
        toast.success(
          t('Batch operation completed for {{count}} users', {
            count: successfulCount,
          })
        )
      } else if (successfulCount > 0) {
        toast.warning(
          t('{{success}} of {{total}} user operations completed', {
            success: successfulCount,
            total: targetUsers.length,
          })
        )
      } else {
        const message =
          failedResult?.status === 'fulfilled'
            ? failedResult.value.message
            : getRejectedMessage(failedResult?.reason)
        toast.error(message || t('Failed to update selected users'))
      }

      if (successfulCount > 0) {
        clearAfterSuccess()
      }
      return successfulCount
    } finally {
      setRunningAction(null)
    }
  }

  const handleConfirmedAction = async () => {
    if (!confirmationAction) return

    const action = confirmationAction
    const targetUsers =
      action === 'enable'
        ? enableTargets
        : action === 'disable'
          ? disableTargets
          : selectedUsers
    const successfulCount = await runExistingOperations(
      action,
      targetUsers,
      (user) =>
        action === 'delete' ? deleteUser(user.id) : manageUser(user.id, action)
    )
    if (successfulCount > 0) {
      setConfirmationAction(null)
    }
  }

  const handleQuotaSubmit = async () => {
    if (!quotaIsValid) return

    const successfulCount = await runExistingOperations(
      'quota',
      selectedUsers,
      (user) =>
        adjustUserQuota({
          id: user.id,
          action: 'add_quota',
          mode: quotaMode,
          value: quotaMode === 'override' ? quotaValue : Math.abs(quotaValue),
        })
    )
    if (successfulCount > 0) {
      setQuotaDialogOpen(false)
      setQuotaMode('add')
      setAmount('')
    }
  }

  const handleGroupSubmit = async () => {
    if (!selectedGroup) return

    setRunningAction('group')
    try {
      const result = await batchUpdateUserGroup({
        ids: selectedIds,
        group: selectedGroup,
      })
      if (!result.success) {
        toast.error(result.message || t('Failed to update selected users'))
        return
      }

      toast.success(
        t('Batch operation completed for {{count}} users', {
          count: result.data?.updated_count ?? selectedCount,
        })
      )
      setGroupDialogOpen(false)
      setSelectedGroup('')
      clearAfterSuccess()
    } catch (error) {
      toast.error(
        getRejectedMessage(error) || t('Failed to update selected users')
      )
    } finally {
      setRunningAction(null)
    }
  }

  const openGroupDialog = () => {
    setSelectedGroup(groups[0] ?? '')
    setGroupDialogOpen(true)
  }

  const confirmationContent = confirmationAction
    ? {
        enable: {
          title: t('Enable {{count}} selected users?', {
            count: enableTargets.length,
          }),
          description:
            enableTargets.length < selectedCount
              ? t(
                  'Only currently disabled users will be enabled; other selected users will be skipped.'
                )
              : t(
                  'These users will be able to sign in and use their API keys again.'
                ),
          confirm: t('Enable users'),
          destructive: false,
        },
        disable: {
          title: t('Disable {{count}} selected users?', {
            count: disableTargets.length,
          }),
          description:
            disableTargets.length < selectedCount
              ? t(
                  'Only currently enabled non-Root users will be disabled; other selected users will be skipped.'
                )
              : t(
                  'These users will be signed out and their API keys will stop working until re-enabled.'
                ),
          confirm: t('Disable users'),
          destructive: true,
        },
        delete: {
          title: t('Delete {{count}} selected users?', {
            count: selectedCount,
          }),
          description: t(
            'This permanently deletes the selected users and cannot be undone.'
          ),
          confirm: t('Delete users'),
          destructive: true,
        },
      }[confirmationAction]
    : null

  const rootRestrictedMessage = t(
    'Root user cannot be included in this operation'
  )
  const enableActionLabel =
    enableTargets.length > 0
      ? t('Enable eligible users ({{count}})', {
          count: enableTargets.length,
        })
      : t('No disabled users selected')
  const disableActionLabel =
    disableTargets.length > 0
      ? t('Disable eligible users ({{count}})', {
          count: disableTargets.length,
        })
      : t('No enabled non-Root users selected')

  return (
    <>
      <BulkActionsToolbar
        table={table}
        entityName={t('User')}
        entityNamePlural={t('Users')}
      >
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setConfirmationAction('enable')}
                disabled={isProcessing || enableTargets.length === 0}
                aria-label={enableActionLabel}
              />
            }
          >
            <HugeiconsIcon icon={UserCheck01Icon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>
            <p>{enableActionLabel}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setConfirmationAction('disable')}
                disabled={isProcessing || disableTargets.length === 0}
                aria-label={disableActionLabel}
              />
            }
          >
            <HugeiconsIcon icon={UserBlock01Icon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>
            <p>{disableActionLabel}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={() => setQuotaDialogOpen(true)}
                disabled={isProcessing}
                aria-label={t('Adjust quota for selected users')}
              />
            }
          >
            <HugeiconsIcon icon={UserDollarIcon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Adjust quota for selected users')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                onClick={openGroupDialog}
                disabled={isProcessing || groups.length === 0}
                aria-label={t('Change group for selected users')}
              />
            }
          >
            <HugeiconsIcon icon={UserGroupIcon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Change group for selected users')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='destructive'
                size='icon'
                onClick={() => setConfirmationAction('delete')}
                disabled={isProcessing || hasRootSelected}
                aria-label={t('Delete selected users')}
              />
            }
          >
            <HugeiconsIcon icon={Delete02Icon} strokeWidth={2} />
          </TooltipTrigger>
          <TooltipContent>
            <p>
              {hasRootSelected
                ? rootRestrictedMessage
                : t('Delete selected users')}
            </p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={confirmationAction !== null}
        onOpenChange={(open) => !open && setConfirmationAction(null)}
        title={confirmationContent?.title ?? ''}
        desc={confirmationContent?.description ?? ''}
        destructive={confirmationContent?.destructive}
        handleConfirm={handleConfirmedAction}
        isLoading={isProcessing}
        confirmText={
          isProcessing ? (
            <>
              <Spinner data-icon='inline-start' />
              {t('Processing...')}
            </>
          ) : (
            confirmationContent?.confirm
          )
        }
      />

      <Dialog open={quotaDialogOpen} onOpenChange={setQuotaDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Adjust quota for {{count}} selected users', {
                count: selectedCount,
              })}
            </DialogTitle>
            <DialogDescription>
              {t(
                'The selected operation and amount will be applied to every selected user.'
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel>{t('Mode')}</FieldLabel>
              <ToggleGroup
                value={[quotaMode]}
                onValueChange={(value) => {
                  const nextMode = value[0] as QuotaAdjustMode | undefined
                  if (nextMode) {
                    setQuotaMode(nextMode)
                    setAmount('')
                  }
                }}
                variant='outline'
                spacing={1}
              >
                <ToggleGroupItem value='add'>{t('Add')}</ToggleGroupItem>
                <ToggleGroupItem value='subtract'>
                  {t('Subtract')}
                </ToggleGroupItem>
                <ToggleGroupItem value='override'>
                  {t('Override')}
                </ToggleGroupItem>
              </ToggleGroup>
            </Field>
            <Field data-invalid={amount.trim() !== '' && !quotaIsValid}>
              <FieldLabel htmlFor='batch-user-quota'>
                {t('Amount')} ({currencyLabel})
              </FieldLabel>
              <Input
                id='batch-user-quota'
                type='number'
                step={tokensOnly ? 1 : 0.000001}
                min={quotaMode === 'override' ? undefined : 0}
                placeholder={
                  tokensOnly
                    ? t('Enter amount in tokens')
                    : t('Enter amount in {{currency}}', {
                        currency: currencyLabel,
                      })
                }
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
                aria-invalid={amount.trim() !== '' && !quotaIsValid}
              />
              <FieldDescription>
                {t(
                  'This changes each selected user independently using the existing quota operation.'
                )}
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setQuotaDialogOpen(false)}
              disabled={isProcessing}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleQuotaSubmit}
              disabled={!quotaIsValid || isProcessing}
            >
              {runningAction === 'quota' && (
                <Spinner data-icon='inline-start' />
              )}
              {isProcessing ? t('Processing...') : t('Apply')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={groupDialogOpen} onOpenChange={setGroupDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t('Change group for {{count}} selected users', {
                count: selectedCount,
              })}
            </DialogTitle>
            <DialogDescription>
              {t('All selected users will be moved to this group.')}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='batch-user-group'>{t('Group')}</FieldLabel>
              <Select
                items={groupItems}
                value={selectedGroup}
                onValueChange={(value) => setSelectedGroup(value ?? '')}
              >
                <SelectTrigger id='batch-user-group' className='w-full'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {groups.map((group) => (
                      <SelectItem key={group} value={group}>
                        {group}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                {t(
                  'Only the ownership group changes; other user settings stay unchanged.'
                )}
              </FieldDescription>
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setGroupDialogOpen(false)}
              disabled={isProcessing}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleGroupSubmit}
              disabled={!selectedGroup || isProcessing}
            >
              {runningAction === 'group' && (
                <Spinner data-icon='inline-start' />
              )}
              {isProcessing ? t('Processing...') : t('Apply')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
