/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMemo, useState } from 'react'
import dayjs from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Delete02Icon,
  Edit02Icon,
  FileUploadIcon,
  FolderOpenIcon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
  Progress,
  ProgressLabel,
  ProgressValue,
} from '@/components/ui/progress'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
import {
  createMaterialGroup,
  deleteMaterial,
  getMaterialStorageUsage,
  listMaterialGroups,
  listMaterials,
  syncMaterials,
  updateMaterialGroup,
  uploadMaterial,
} from './api'
import type { MaterialAsset, MaterialGroup } from './types'

function statusBadge(status: string) {
  const normalized = status.toLowerCase()
  if (normalized === 'active') return 'success' as const
  if (normalized === 'rejected' || normalized === 'failed') {
    return 'destructive' as const
  }
  return 'warning' as const
}

function unixTime(value: number) {
  return value > 0 ? dayjs.unix(value).format('YYYY-MM-DD HH:mm') : '—'
}

function formatBytes(value: number) {
  if (value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1
  )
  const amount = value / 1024 ** index
  return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(amount)} ${units[index]}`
}

export function DoubaoVideoMaterialLibrary() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [activeTab, setActiveTab] = useState<'groups' | 'assets'>('groups')
  const [assetGroupId, setAssetGroupId] = useState<number | null>(null)
  const [groupDialog, setGroupDialog] = useState(false)
  const [editingGroup, setEditingGroup] = useState<MaterialGroup | null>(null)
  const [uploadDialog, setUploadDialog] = useState(false)
  const [groupName, setGroupName] = useState('')
  const [groupDescription, setGroupDescription] = useState('')
  const [uploadGroup, setUploadGroup] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<MaterialAsset | null>(null)

  const groupsQuery = useQuery({
    queryKey: ['doubao-video', 'groups'],
    queryFn: () => listMaterialGroups(),
  })
  const assetsQuery = useQuery({
    queryKey: ['doubao-video', 'assets', assetGroupId],
    queryFn: () => listMaterials({ groupId: assetGroupId ?? undefined }),
  })
  const storageQuery = useQuery({
    queryKey: ['doubao-video', 'storage'],
    queryFn: getMaterialStorageUsage,
  })
  const groups = useMemo(
    () => groupsQuery.data?.data ?? [],
    [groupsQuery.data?.data]
  )
  const assets = assetsQuery.data?.data?.items ?? []
  const storage = storageQuery.data?.data
  const storagePercent = storage
    ? Math.min(100, (storage.used_bytes / storage.limit_bytes) * 100)
    : 0
  const filteredGroups = useMemo(() => {
    const query = keyword.trim().toLowerCase()
    return query
      ? groups.filter((group) =>
          `${group.name} ${group.provider_group_id}`
            .toLowerCase()
            .includes(query)
        )
      : groups
  }, [groups, keyword])

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['doubao-video', 'groups'] }),
      queryClient.invalidateQueries({ queryKey: ['doubao-video', 'assets'] }),
      queryClient.invalidateQueries({ queryKey: ['doubao-video', 'storage'] }),
    ])
  }

  const createMutation = useMutation({
    mutationFn: () =>
      createMaterialGroup({ name: groupName, description: groupDescription }),
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Material group created'))
      setGroupDialog(false)
      setEditingGroup(null)
      setGroupName('')
      setGroupDescription('')
      await refresh()
    },
    onError: (error) => toast.error(error.message),
  })
  const updateMutation = useMutation({
    mutationFn: () => {
      if (!editingGroup) throw new Error(t('Select a material group'))
      return updateMaterialGroup(editingGroup.id, {
        name: groupName,
        description: groupDescription,
      })
    },
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Material group updated'))
      setGroupDialog(false)
      setEditingGroup(null)
      setGroupName('')
      setGroupDescription('')
      await refresh()
    },
    onError: (error) => toast.error(error.message),
  })
  const uploadMutation = useMutation({
    mutationFn: () => {
      if (!uploadFile || !uploadGroup)
        throw new Error(t('Select a material group and file'))
      if (storage && uploadFile.size > storage.remaining_bytes) {
        throw new Error(
          t(
            'The material library has reached its limit. Delete existing materials or contact support to adjust it.'
          )
        )
      }
      return uploadMaterial({
        groupId: Number(uploadGroup),
        name: uploadName || uploadFile.name,
        file: uploadFile,
      })
    },
    onSuccess: async (response) => {
      if (!response.success) {
        toast.error(
          response.message ||
            t(
              'The material library has reached its limit. Delete existing materials or contact support to adjust it.'
            )
        )
        return
      }
      toast.success(t('Material uploaded and submitted for review'))
      setUploadDialog(false)
      setUploadGroup('')
      setUploadName('')
      setUploadFile(null)
      await refresh()
    },
    onError: (error) => toast.error(error.message),
  })
  const deleteMutation = useMutation({
    mutationFn: (id: number) => deleteMaterial(id),
    onSuccess: async (response) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to delete material'))
        return
      }
      toast.success(t('Material deleted'))
      setDeleteTarget(null)
      await refresh()
    },
    onError: (error) => toast.error(error.message),
  })
  const syncMutation = useMutation({
    mutationFn: syncMaterials,
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Review status synchronized'))
      await refresh()
    },
  })
  const groupMutationPending =
    createMutation.isPending || updateMutation.isPending
  const openCreateGroupDialog = () => {
    setEditingGroup(null)
    setGroupName('')
    setGroupDescription('')
    setGroupDialog(true)
  }
  const openEditGroupDialog = (group: MaterialGroup) => {
    setEditingGroup(group)
    setGroupName(group.name)
    setGroupDescription(group.description)
    setGroupDialog(true)
  }
  const submitGroup = () => {
    if (!groupName.trim() || groupMutationPending) return
    if (editingGroup) updateMutation.mutate()
    else createMutation.mutate()
  }

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Material Library')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Description>
          {t(
            'Upload materials to the DoubaoVideo2.0 upstream library and track review results.'
          )}
        </SectionPageLayout.Description>
        <SectionPageLayout.Actions>
          <Button
            variant='outline'
            onClick={() => syncMutation.mutate()}
            disabled={syncMutation.isPending}
          >
            <HugeiconsIcon icon={RefreshIcon} data-icon='inline-start' />
            {t('Sync review status')}
          </Button>
          <Button
            onClick={() => setUploadDialog(true)}
            disabled={groups.length === 0}
          >
            <HugeiconsIcon icon={FileUploadIcon} data-icon='inline-start' />
            {t('Upload material')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          {storage && (
            <Alert
              variant={
                storage.remaining_bytes === 0 ? 'destructive' : 'default'
              }
            >
              <AlertTitle>{t('Material library storage')}</AlertTitle>
              <AlertDescription>
                <div className='flex flex-col gap-2'>
                  <Progress value={storagePercent}>
                    <ProgressLabel>{t('Storage usage')}</ProgressLabel>
                    <ProgressValue>
                      {() =>
                        `${formatBytes(storage.used_bytes)} / ${formatBytes(storage.limit_bytes)}`
                      }
                    </ProgressValue>
                  </Progress>
                  <span>
                    {storage.remaining_bytes === 0
                      ? t(
                          'The material library has reached its limit. Delete existing materials or contact support to adjust it.'
                        )
                      : t('{{remaining}} remaining', {
                          remaining: formatBytes(storage.remaining_bytes),
                        })}
                  </span>
                </div>
              </AlertDescription>
            </Alert>
          )}
          <Tabs
            value={activeTab}
            onValueChange={(value) =>
              setActiveTab(value as 'groups' | 'assets')
            }
          >
            <TabsList>
              <TabsTrigger value='groups'>{t('Material groups')}</TabsTrigger>
              <TabsTrigger value='assets'>{t('Materials')}</TabsTrigger>
            </TabsList>
            <TabsContent value='groups' className='mt-3 space-y-3'>
              <div className='bg-card flex flex-wrap items-center justify-between gap-2 rounded-xl border p-3'>
                <Input
                  className='max-w-xs'
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder={t('Search group name or ID')}
                />
                <Button onClick={openCreateGroupDialog}>
                  {t('Create material group')}
                </Button>
              </div>
              <div className='bg-card overflow-hidden rounded-xl border'>
                {filteredGroups.length === 0 ? (
                  <Empty className='min-h-52 border-0'>
                    <EmptyHeader>
                      <EmptyTitle>{t('No material groups')}</EmptyTitle>
                      <EmptyDescription>
                        {t(
                          'Create a group before uploading your first material.'
                        )}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Group name')}</TableHead>
                        <TableHead>{t('Description')}</TableHead>
                        <TableHead>{t('Group ID')}</TableHead>
                        <TableHead>{t('Type')}</TableHead>
                        <TableHead>{t('Assets')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead>{t('Created at')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredGroups.map((group) => (
                        <TableRow key={group.id}>
                          <TableCell className='font-medium'>
                            {group.name}
                          </TableCell>
                          <TableCell className='max-w-sm text-sm'>
                            <span className='line-clamp-2'>
                              {group.description || '—'}
                            </span>
                          </TableCell>
                          <TableCell className='font-mono text-xs'>
                            {group.provider_group_id}
                          </TableCell>
                          <TableCell>{group.group_type}</TableCell>
                          <TableCell>{group.asset_count}</TableCell>
                          <TableCell>
                            <Badge variant={statusBadge(group.status)}>
                              {group.status}
                            </Badge>
                          </TableCell>
                          <TableCell>{unixTime(group.created_at)}</TableCell>
                          <TableCell>
                            <div className='flex justify-end gap-1'>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='text-primary h-8 px-2'
                                onClick={() => {
                                  setAssetGroupId(group.id)
                                  setActiveTab('assets')
                                }}
                              >
                                <HugeiconsIcon icon={FolderOpenIcon} />
                                {t('View materials')}
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='text-primary h-8 px-2'
                                onClick={() => openEditGroupDialog(group)}
                              >
                                <HugeiconsIcon icon={Edit02Icon} />
                                {t('Edit')}
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </div>
            </TabsContent>
            <TabsContent value='assets' className='mt-3'>
              <div className='bg-card overflow-hidden rounded-xl border'>
                {assets.length === 0 ? (
                  <Empty className='min-h-52 border-0'>
                    <EmptyHeader>
                      <EmptyTitle>{t('No materials')}</EmptyTitle>
                      <EmptyDescription>
                        {t(
                          'Uploaded materials and review results will appear here.'
                        )}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                ) : (
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Material name')}</TableHead>
                        <TableHead>{t('Asset ID')}</TableHead>
                        <TableHead>{t('Type')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead>{t('Last synchronized')}</TableHead>
                        <TableHead>{t('Review result')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {assets.map((asset) => (
                        <TableRow key={asset.id}>
                          <TableCell className='font-medium'>
                            {asset.name}
                          </TableCell>
                          <TableCell>
                            <code className='text-xs'>
                              asset://{asset.provider_asset_id}
                            </code>
                          </TableCell>
                          <TableCell>{asset.asset_type}</TableCell>
                          <TableCell>
                            <Badge variant={statusBadge(asset.status)}>
                              {asset.status}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {unixTime(asset.last_synced_at)}
                          </TableCell>
                          <TableCell className='max-w-xs text-sm'>
                            {asset.error_message || '—'}
                          </TableCell>
                          <TableCell className='text-right'>
                            <Button
                              variant='ghost'
                              size='sm'
                              onClick={() => setDeleteTarget(asset)}
                            >
                              <HugeiconsIcon
                                icon={Delete02Icon}
                                data-icon='inline-start'
                              />
                              {t('Delete')}
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </div>
            </TabsContent>
          </Tabs>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <Dialog open={groupDialog} onOpenChange={setGroupDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingGroup
                ? t('Edit material group')
                : t('Create material group')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Create a material group to organize and manage your materials.'
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='doubao-group-name'>
                {t('Group name')}
              </FieldLabel>
              <Input
                id='doubao-group-name'
                maxLength={64}
                value={groupName}
                onChange={(event) => setGroupName(event.target.value)}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor='doubao-group-description'>
                {t('Description')}
              </FieldLabel>
              <Textarea
                id='doubao-group-description'
                maxLength={300}
                value={groupDescription}
                onChange={(event) => setGroupDescription(event.target.value)}
              />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button variant='outline' onClick={() => setGroupDialog(false)}>
              {t('Cancel')}
            </Button>
            <Button
              onClick={submitGroup}
              disabled={!groupName.trim() || groupMutationPending}
            >
              {editingGroup ? t('Update') : t('Create')}
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
            <AlertDialogTitle>{t('Delete material?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The material will be deleted from the upstream API key account and its storage will be released. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending}
              onClick={() =>
                deleteTarget && deleteMutation.mutate(deleteTarget.id)
              }
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog open={uploadDialog} onOpenChange={setUploadDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Upload material')}</DialogTitle>
            <DialogDescription>
              {t(
                'The file is retained while the matching upstream material exists, then submitted to the selected upstream material group for review.'
              )}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor='doubao-upload-group'>
                {t('Material group')}
              </FieldLabel>
              <Select
                items={groups.map((group) => ({
                  value: String(group.id),
                  label: group.name,
                }))}
                value={uploadGroup}
                onValueChange={(value) => setUploadGroup(value ?? '')}
              >
                <SelectTrigger id='doubao-upload-group' className='w-full'>
                  <SelectValue placeholder={t('Select material group')} />
                </SelectTrigger>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {groups.map((group) => (
                      <SelectItem key={group.id} value={String(group.id)}>
                        {group.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor='doubao-upload-name'>
                {t('Material name')}
              </FieldLabel>
              <Input
                id='doubao-upload-name'
                maxLength={128}
                value={uploadName}
                onChange={(event) => setUploadName(event.target.value)}
                placeholder={t('Defaults to the file name')}
              />
            </Field>
            <Field>
              <FieldLabel htmlFor='doubao-upload-file'>{t('File')}</FieldLabel>
              <Input
                id='doubao-upload-file'
                type='file'
                accept='image/*,video/*,audio/*'
                onChange={(event) =>
                  setUploadFile(event.target.files?.[0] ?? null)
                }
              />
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button variant='outline' onClick={() => setUploadDialog(false)}>
              {t('Cancel')}
            </Button>
            <Button
              onClick={() => uploadMutation.mutate()}
              disabled={!uploadGroup || !uploadFile || uploadMutation.isPending}
            >
              {t('Upload and submit')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
