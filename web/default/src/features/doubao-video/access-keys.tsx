/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useState } from 'react'
import dayjs from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Copy01Icon, Delete02Icon, Key01Icon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { SectionPageLayout } from '@/components/layout'
import {
  createAccessKey,
  deleteAccessKey,
  listAccessKeys,
  updateAccessKey,
} from './api'
import type { AccessKey, CreatedAccessKey } from './types'

function unixTime(value: number) {
  return value > 0 ? dayjs.unix(value).format('YYYY-MM-DD HH:mm') : '—'
}

export function DoubaoVideoAccessKeys() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [createDialog, setCreateDialog] = useState(false)
  const [keyName, setKeyName] = useState('')
  const [createdKey, setCreatedKey] = useState<CreatedAccessKey | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<AccessKey | null>(null)
  const keysQuery = useQuery({
    queryKey: ['doubao-video', 'access-keys'],
    queryFn: listAccessKeys,
  })
  const keys = keysQuery.data?.data ?? []
  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['doubao-video', 'access-keys'] })

  const createMutation = useMutation({
    mutationFn: () => createAccessKey(keyName),
    onSuccess: async (response) => {
      if (!response.success) return
      setCreatedKey(response.data)
      setCreateDialog(false)
      setKeyName('')
      toast.success(t('Access key created'))
      await refresh()
    },
  })
  const statusMutation = useMutation({
    mutationFn: (key: AccessKey) =>
      updateAccessKey(key.id, key.status === 1 ? 0 : 1),
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Access key status updated'))
      await refresh()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteAccessKey(id),
    onSuccess: async (response) => {
      if (!response.success) return
      setDeleteTarget(null)
      toast.success(t('Access key deleted'))
      await refresh()
    },
  })

  const copy = async (value: string) => {
    await navigator.clipboard.writeText(value)
    toast.success(t('Copied'))
  }
  const endpoint =
    typeof window === 'undefined'
      ? '/api/doubao-video/openapi'
      : `${window.location.origin}/api/doubao-video/openapi`

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Access Keys')}</SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(
            'A dedicated key for accessing the material library, not an API key for calling model services.'
          )}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          <Button
            onClick={() => setCreateDialog(true)}
            disabled={keys.length >= 2}
          >
            <HugeiconsIcon icon={Key01Icon} data-icon='inline-start' />
            {t('Create access key')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <Card className='mb-3'>
            <CardHeader>
              <CardTitle className='text-sm'>
                {t('Official-compatible request format')}
              </CardTitle>
              <CardDescription>
                {t(
                  'Use Action and Version=2024-01-01 with HMAC-SHA256 signing. The upstream channel URL and credentials remain private.'
                )}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className='bg-muted flex items-center gap-2 rounded-lg px-3 py-2'>
                <code className='min-w-0 flex-1 truncate text-xs'>
                  {endpoint}
                </code>
                <Button
                  size='icon'
                  variant='ghost'
                  onClick={() => copy(endpoint)}
                  aria-label={t('Copy endpoint')}
                >
                  <HugeiconsIcon icon={Copy01Icon} />
                </Button>
              </div>
            </CardContent>
          </Card>
          <div className='bg-card overflow-hidden rounded-xl border'>
            {keys.length === 0 ? (
              <Empty className='min-h-52 border-0'>
                <EmptyHeader>
                  <EmptyTitle>{t('No access keys')}</EmptyTitle>
                  <EmptyDescription>
                    {t(
                      'Create an AK/SK pair to call the material API with the official signing format.'
                    )}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Name')}</TableHead>
                    <TableHead>{t('Access Key ID')}</TableHead>
                    <TableHead>{t('Status')}</TableHead>
                    <TableHead>{t('Last used')}</TableHead>
                    <TableHead>{t('Created at')}</TableHead>
                    <TableHead className='text-right'>{t('Actions')}</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {keys.map((key) => (
                    <TableRow key={key.id}>
                      <TableCell className='font-medium'>{key.name}</TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1'>
                          <code className='text-xs'>{key.access_key_id}</code>
                          <Button
                            size='icon-xs'
                            variant='ghost'
                            onClick={() => copy(key.access_key_id)}
                            aria-label={t('Copy Access Key ID')}
                          >
                            <HugeiconsIcon icon={Copy01Icon} />
                          </Button>
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant={key.status === 1 ? 'success' : 'secondary'}
                        >
                          {key.status === 1 ? t('Enabled') : t('Disabled')}
                        </Badge>
                      </TableCell>
                      <TableCell>{unixTime(key.last_used_at)}</TableCell>
                      <TableCell>{unixTime(key.created_at)}</TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-1'>
                          <Button
                            size='sm'
                            variant='outline'
                            onClick={() => statusMutation.mutate(key)}
                            disabled={statusMutation.isPending}
                          >
                            {key.status === 1 ? t('Disable') : t('Enable')}
                          </Button>
                          <Button
                            size='icon-sm'
                            variant='ghost'
                            onClick={() => setDeleteTarget(key)}
                            aria-label={t('Delete')}
                          >
                            <HugeiconsIcon icon={Delete02Icon} />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog open={createDialog} onOpenChange={setCreateDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Create access key')}</DialogTitle>
            <DialogDescription>
              {t(
                'You can create up to two keys. The Secret Access Key is displayed only once.'
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='doubao-key-name'>{t('Key name')}</FieldLabel>
              <Input
                id='doubao-key-name'
                maxLength={64}
                value={keyName}
                onChange={(event) => setKeyName(event.target.value)}
                placeholder={t('For example: material uploader')}
              />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button variant='outline' onClick={() => setCreateDialog(false)}>
              {t('Cancel')}
            </Button>
            <Button
              onClick={() => createMutation.mutate()}
              disabled={!keyName.trim() || createMutation.isPending}
            >
              {t('Create')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={createdKey !== null}
        onOpenChange={(open) => !open && setCreatedKey(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Save your Secret Access Key')}</DialogTitle>
            <DialogDescription>
              {t(
                'This secret is displayed only once. Store it securely before closing this dialog.'
              )}
            </DialogDescription>
          </DialogHeader>
          {createdKey && (
            <FieldGroup>
              <Field>
                <FieldLabel>{t('Access Key ID')}</FieldLabel>
                <div className='flex gap-2'>
                  <Input
                    readOnly
                    value={createdKey.key.access_key_id}
                    className='font-mono'
                  />
                  <Button
                    size='icon'
                    variant='outline'
                    onClick={() => copy(createdKey.key.access_key_id)}
                    aria-label={t('Copy Access Key ID')}
                  >
                    <HugeiconsIcon icon={Copy01Icon} />
                  </Button>
                </div>
              </Field>
              <Field>
                <FieldLabel>{t('Secret Access Key')}</FieldLabel>
                <div className='flex gap-2'>
                  <Input
                    readOnly
                    value={createdKey.secret_access_key}
                    className='font-mono'
                  />
                  <Button
                    size='icon'
                    variant='outline'
                    onClick={() => copy(createdKey.secret_access_key)}
                    aria-label={t('Copy Secret Access Key')}
                  >
                    <HugeiconsIcon icon={Copy01Icon} />
                  </Button>
                </div>
              </Field>
            </FieldGroup>
          )}
          <DialogFooter>
            <Button onClick={() => setCreatedKey(null)}>
              {t('I have saved it')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete access key?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Applications using this key will immediately lose access. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={() =>
                deleteTarget && deleteMutation.mutate(deleteTarget.id)
              }
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
