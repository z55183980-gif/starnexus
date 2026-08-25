/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useMemo, useRef, useState } from 'react'
import dayjs from 'dayjs'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft01Icon,
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
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
import {
  createMaterialGroup,
  deleteMaterial,
  deleteMaterialGroup,
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

function assetTypeTranslationKey(assetType: string) {
  switch (assetType.toLowerCase()) {
    case 'image':
      return 'Image'
    case 'video':
      return 'Video'
    case 'audio':
      return 'Audio'
    default:
      return null
  }
}

function assetStatusTranslationKey(status: string) {
  switch (status.toLowerCase()) {
    case 'active':
      return 'Approved'
    case 'processing':
      return 'Under review'
    case 'rejected':
      return 'Rejected'
    case 'failed':
      return 'Failed'
    default:
      return null
  }
}

function AssetPreview({ asset }: { asset: MaterialAsset }) {
  const { t } = useTranslation()
  const [previewFailed, setPreviewFailed] = useState(false)
  if (!asset.preview_url) return '—'

  const typeKey = assetTypeTranslationKey(asset.asset_type)
  const isImage = asset.asset_type.toLowerCase() === 'image'
  const isVideo = asset.asset_type.toLowerCase() === 'video'
  return (
    <div className='flex items-center gap-2'>
      {isImage && !previewFailed ? (
        <a
          href={asset.preview_url}
          target='_blank'
          rel='noreferrer'
          aria-label={t('Preview')}
          className='group focus-visible:ring-ring block rounded-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        >
          <img
            src={asset.preview_url}
            alt={asset.name}
            className='size-10 cursor-pointer rounded-md border object-cover transition-transform duration-150 ease-out group-hover:scale-105 group-hover:shadow-sm group-active:scale-95'
            loading='lazy'
            onError={() => setPreviewFailed(true)}
          />
        </a>
      ) : isVideo && !previewFailed ? (
        <a
          href={asset.preview_url}
          target='_blank'
          rel='noreferrer'
          aria-label={t('Preview')}
          className='group focus-visible:ring-ring block rounded-md focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:outline-none'
        >
          <video
            src={asset.preview_url}
            aria-label={asset.name}
            className='size-10 cursor-pointer rounded-md border object-cover transition-transform duration-150 ease-out group-hover:scale-105 group-hover:shadow-sm group-active:scale-95'
            muted
            playsInline
            preload='metadata'
            onError={() => setPreviewFailed(true)}
          />
        </a>
      ) : (
        <Badge variant='secondary'>
          {typeKey ? t(typeKey) : asset.asset_type}
        </Badge>
      )}
    </div>
  )
}

function AssetDetailDialog(props: {
  asset: MaterialAsset | null
  group?: MaterialGroup
  onOpenChange: (open: boolean) => void
}) {
  const { asset, group, onOpenChange } = props
  const { t } = useTranslation()
  const [previewFailed, setPreviewFailed] = useState(false)

  if (!asset) return null

  const isImage = asset.asset_type.toLowerCase() === 'image'
  const isVideo = asset.asset_type.toLowerCase() === 'video'
  const typeKey = assetTypeTranslationKey(asset.asset_type)
  const statusKey = assetStatusTranslationKey(asset.status)

  return (
    <Dialog open={asset !== null} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Details')}</DialogTitle>
        </DialogHeader>
        <div className='flex min-h-48 items-center justify-center'>
          {isImage && asset.preview_url && !previewFailed ? (
            <img
              src={asset.preview_url}
              alt={asset.name}
              className='max-h-72 max-w-full rounded-lg object-contain'
              onError={() => setPreviewFailed(true)}
            />
          ) : isVideo && asset.preview_url && !previewFailed ? (
            <video
              src={asset.preview_url}
              aria-label={asset.name}
              className='max-h-96 max-w-full rounded-lg'
              controls
              playsInline
              preload='metadata'
              onError={() => setPreviewFailed(true)}
            />
          ) : (
            <Badge variant='secondary'>
              {typeKey ? t(typeKey) : asset.asset_type}
            </Badge>
          )}
        </div>
        <div className='overflow-hidden rounded-lg border'>
          <dl className='divide-y'>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Asset ID')}
              </dt>
              <dd className='px-4 py-3 text-sm'>{asset.provider_asset_id}</dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Material name')}
              </dt>
              <dd className='px-4 py-3 text-sm'>{asset.name}</dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Type')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {typeKey ? t(typeKey) : asset.asset_type}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('URL')}
              </dt>
              <dd className='px-4 py-3 font-mono text-xs break-all'>
                {asset.preview_url || '—'}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Sync status')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {asset.last_synced_at > 0
                  ? t('Synchronized')
                  : t('Not synchronized yet')}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Asset status')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {statusKey ? t(statusKey) : asset.status}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Material group')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {group ? `${group.name} (${group.provider_group_id})` : '—'}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Failure reason')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {asset.error_message || '—'}
              </dd>
            </div>
            <div className='grid grid-cols-[7rem_minmax(0,1fr)]'>
              <dt className='bg-muted/40 text-muted-foreground px-4 py-3 text-sm'>
                {t('Created at')}
              </dt>
              <dd className='px-4 py-3 text-sm'>
                {unixTime(asset.created_at)}
              </dd>
            </div>
          </dl>
        </div>
      </DialogContent>
    </Dialog>
  )
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
  const [assetGroupId, setAssetGroupId] = useState<number | null>(null)
  const [groupDialog, setGroupDialog] = useState(false)
  const [editingGroup, setEditingGroup] = useState<MaterialGroup | null>(null)
  const [uploadDialog, setUploadDialog] = useState(false)
  const [groupName, setGroupName] = useState('')
  const [groupDescription, setGroupDescription] = useState('')
  const [uploadGroup, setUploadGroup] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)
  const uploadFileInputRef = useRef<HTMLInputElement>(null)
  const [deleteGroupTarget, setDeleteGroupTarget] =
    useState<MaterialGroup | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<MaterialAsset | null>(null)
  const [detailTarget, setDetailTarget] = useState<MaterialAsset | null>(null)

  const groupsQuery = useQuery({
    queryKey: ['doubao-video', 'groups'],
    queryFn: () => listMaterialGroups(),
  })
  const assetsQuery = useQuery({
    queryKey: ['doubao-video', 'assets', assetGroupId],
    queryFn: () => listMaterials({ groupId: assetGroupId ?? undefined }),
    enabled: assetGroupId !== null,
  })
  const storageQuery = useQuery({
    queryKey: ['doubao-video', 'storage'],
    queryFn: getMaterialStorageUsage,
  })
  const groups = useMemo(
    () => groupsQuery.data?.data ?? [],
    [groupsQuery.data?.data]
  )
  const groupsById = useMemo(
    () => new Map(groups.map((group) => [group.id, group])),
    [groups]
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
      if (!editingGroup) throw new Error(t('Select material group'))
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
  const deleteGroupMutation = useMutation({
    mutationFn: (id: number) => deleteMaterialGroup(id),
    onSuccess: async (response, deletedGroupId) => {
      if (!response.success) {
        toast.error(response.message || t('Failed to delete material group'))
        return
      }
      toast.success(t('Material group deleted'))
      setDeleteGroupTarget(null)
      if (assetGroupId === deletedGroupId) setAssetGroupId(null)
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
  const openUploadDialog = (groupId?: number) => {
    setUploadGroup(groupId ? String(groupId) : '')
    setUploadName('')
    setUploadFile(null)
    if (uploadFileInputRef.current) uploadFileInputRef.current.value = ''
    setUploadDialog(true)
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
        {assetGroupId === null && storage && (
          <SectionPageLayout.Actions>
            <Alert
              variant={
                storage.remaining_bytes === 0 ? 'destructive' : 'default'
              }
              className='w-72 max-w-full py-1.5'
            >
              <div className='flex items-center justify-between gap-3'>
                <AlertTitle>{t('Material library storage')}</AlertTitle>
                <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
                  {`${formatBytes(storage.used_bytes)} / ${formatBytes(storage.limit_bytes)}`}
                </span>
              </div>
              <AlertDescription className='flex flex-col gap-1'>
                <Progress value={storagePercent} className='gap-0'>
                  <ProgressLabel className='sr-only'>
                    {t('Storage usage')}
                  </ProgressLabel>
                  <ProgressValue className='sr-only'>
                    {() =>
                      `${formatBytes(storage.used_bytes)} / ${formatBytes(storage.limit_bytes)}`
                    }
                  </ProgressValue>
                </Progress>
                {storage.remaining_bytes === 0 && (
                  <span className='text-xs'>
                    {t(
                      'The material library has reached its limit. Delete existing materials or contact support to adjust it.'
                    )}
                  </span>
                )}
              </AlertDescription>
            </Alert>
          </SectionPageLayout.Actions>
        )}
        <SectionPageLayout.Content>
          <div className='flex flex-col gap-3'>
            <div className='flex flex-col gap-3' hidden={assetGroupId !== null}>
              <div className='bg-card flex flex-wrap items-center justify-between gap-2 rounded-xl border p-3'>
                <Input
                  className='max-w-xs'
                  value={keyword}
                  onChange={(event) => setKeyword(event.target.value)}
                  placeholder={t('Search group name or ID')}
                />
                <div className='flex flex-wrap items-center gap-2'>
                  <Button onClick={openCreateGroupDialog}>
                    {t('Create material group')}
                  </Button>
                  <Button
                    onClick={() => openUploadDialog()}
                    disabled={groups.length === 0}
                  >
                    <HugeiconsIcon
                      icon={FileUploadIcon}
                      data-icon='inline-start'
                    />
                    {t('Upload material')}
                  </Button>
                  <Button
                    variant='outline'
                    onClick={() => syncMutation.mutate()}
                    disabled={syncMutation.isPending}
                  >
                    <HugeiconsIcon
                      icon={RefreshIcon}
                      data-icon='inline-start'
                    />
                    {t('Sync review status')}
                  </Button>
                </div>
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
                              {(() => {
                                const key = assetStatusTranslationKey(
                                  group.status
                                )
                                return key ? t(key) : group.status
                              })()}
                            </Badge>
                          </TableCell>
                          <TableCell>{unixTime(group.created_at)}</TableCell>
                          <TableCell>
                            <div className='flex justify-end gap-1'>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='text-primary h-8 px-2'
                                onClick={() => setAssetGroupId(group.id)}
                              >
                                <HugeiconsIcon
                                  icon={FolderOpenIcon}
                                  data-icon='inline-start'
                                />
                                {t('View materials')}
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='text-primary h-8 px-2'
                                onClick={() => openEditGroupDialog(group)}
                              >
                                <HugeiconsIcon
                                  icon={Edit02Icon}
                                  data-icon='inline-start'
                                />
                                {t('Edit')}
                              </Button>
                              <Button
                                variant='ghost'
                                size='sm'
                                className='h-8 px-2'
                                onClick={() => setDeleteGroupTarget(group)}
                              >
                                <HugeiconsIcon
                                  icon={Delete02Icon}
                                  data-icon='inline-start'
                                />
                                {t('Delete')}
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </div>
            </div>
            <div className='flex flex-col gap-3' hidden={assetGroupId === null}>
              <div className='flex items-center justify-between gap-3'>
                <span className='text-muted-foreground text-sm'>
                  {assetGroupId === null
                    ? t('Material group')
                    : (groupsById.get(assetGroupId)?.name ??
                      t('Material group'))}
                </span>
                <div className='flex items-center gap-2'>
                  <Button
                    size='sm'
                    onClick={() =>
                      assetGroupId !== null && openUploadDialog(assetGroupId)
                    }
                  >
                    <HugeiconsIcon
                      icon={FileUploadIcon}
                      data-icon='inline-start'
                    />
                    {t('Upload material')}
                  </Button>
                  <Button
                    variant='outline'
                    size='sm'
                    onClick={() => setAssetGroupId(null)}
                  >
                    <HugeiconsIcon
                      icon={ArrowLeft01Icon}
                      data-icon='inline-start'
                    />
                    {t('Back')}
                  </Button>
                </div>
              </div>
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
                        <TableHead>{t('Asset ID')}</TableHead>
                        <TableHead className='w-60 max-w-60'>
                          {t('Material name')}
                        </TableHead>
                        <TableHead>{t('Resource preview')}</TableHead>
                        <TableHead>{t('Type')}</TableHead>
                        <TableHead>{t('Sync status')}</TableHead>
                        <TableHead>{t('Failure reason')}</TableHead>
                        <TableHead>{t('Asset status')}</TableHead>
                        <TableHead>{t('Material group')}</TableHead>
                        <TableHead>{t('Created at')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {assets.map((asset) => (
                        <TableRow key={asset.id}>
                          <TableCell>
                            <code className='text-xs'>
                              {asset.provider_asset_id}
                            </code>
                          </TableCell>
                          <TableCell className='w-60 max-w-60 font-medium'>
                            <span className='block truncate' title={asset.name}>
                              {asset.name}
                            </span>
                          </TableCell>
                          <TableCell>
                            <AssetPreview asset={asset} />
                          </TableCell>
                          <TableCell>
                            <Badge variant='secondary'>
                              {(() => {
                                const key = assetTypeTranslationKey(
                                  asset.asset_type
                                )
                                return key ? t(key) : asset.asset_type
                              })()}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                asset.last_synced_at > 0 ? 'success' : 'warning'
                              }
                            >
                              {asset.last_synced_at > 0
                                ? t('Synchronized')
                                : t('Not synchronized yet')}
                            </Badge>
                          </TableCell>
                          <TableCell className='max-w-xs whitespace-normal'>
                            {asset.error_message || '—'}
                          </TableCell>
                          <TableCell>
                            <Badge variant={statusBadge(asset.status)}>
                              {(() => {
                                const key = assetStatusTranslationKey(
                                  asset.status
                                )
                                return key ? t(key) : asset.status
                              })()}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {(() => {
                              const group = groupsById.get(asset.asset_group_id)
                              if (!group) return '—'
                              return (
                                <div className='flex flex-col gap-0.5'>
                                  <span>{group.name}</span>
                                  <span className='text-muted-foreground font-mono text-xs'>
                                    {group.provider_group_id}
                                  </span>
                                </div>
                              )
                            })()}
                          </TableCell>
                          <TableCell>{unixTime(asset.created_at)}</TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-1'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() => setDetailTarget(asset)}
                              >
                                {t('Details')}
                              </Button>
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
                            </div>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                )}
              </div>
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <AssetDetailDialog
        key={detailTarget?.id ?? 'empty'}
        asset={detailTarget}
        group={
          detailTarget ? groupsById.get(detailTarget.asset_group_id) : undefined
        }
        onOpenChange={(open) => !open && setDetailTarget(null)}
      />

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
        open={deleteGroupTarget !== null}
        onOpenChange={(open) =>
          !open && !deleteGroupMutation.isPending && setDeleteGroupTarget(null)
        }
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete material group?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'All materials in this group will be deleted from the upstream API key account, their storage will be released, and the material group will be permanently removed. This action cannot be undone.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteGroupMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteGroupMutation.isPending}
              onClick={() =>
                deleteGroupTarget &&
                deleteGroupMutation.mutate(deleteGroupTarget.id)
              }
            >
              {deleteGroupMutation.isPending ? t('Deleting...') : t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

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
                'Submit the material for review. It will be stored long term.'
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
              <div className='border-input bg-background flex min-h-12 items-center justify-between gap-3 rounded-lg border p-2'>
                <span
                  className='text-muted-foreground min-w-0 flex-1 truncate text-sm'
                  title={uploadFile?.name}
                >
                  {uploadFile?.name ?? t('No file selected')}
                </span>
                <Button
                  type='button'
                  variant='outline'
                  size='sm'
                  className='shrink-0'
                  onClick={() => uploadFileInputRef.current?.click()}
                >
                  <HugeiconsIcon
                    icon={FileUploadIcon}
                    data-icon='inline-start'
                  />
                  {t('Choose file')}
                </Button>
              </div>
              <input
                ref={uploadFileInputRef}
                id='doubao-upload-file'
                type='file'
                accept='image/*,video/*,audio/*'
                className='hidden'
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
