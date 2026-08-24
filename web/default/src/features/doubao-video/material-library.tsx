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
import { FileUploadIcon, RefreshIcon } from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
  listMaterialGroups,
  listMaterials,
  syncMaterials,
  uploadMaterial,
} from './api'

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

export function DoubaoVideoMaterialLibrary() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [keyword, setKeyword] = useState('')
  const [groupDialog, setGroupDialog] = useState(false)
  const [uploadDialog, setUploadDialog] = useState(false)
  const [groupName, setGroupName] = useState('')
  const [groupDescription, setGroupDescription] = useState('')
  const [uploadGroup, setUploadGroup] = useState('')
  const [uploadName, setUploadName] = useState('')
  const [uploadFile, setUploadFile] = useState<File | null>(null)

  const groupsQuery = useQuery({
    queryKey: ['doubao-video', 'groups'],
    queryFn: () => listMaterialGroups(),
  })
  const assetsQuery = useQuery({
    queryKey: ['doubao-video', 'assets'],
    queryFn: () => listMaterials({}),
  })
  const groups = groupsQuery.data?.data ?? []
  const assets = assetsQuery.data?.data?.items ?? []
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
    ])
  }

  const createMutation = useMutation({
    mutationFn: () =>
      createMaterialGroup({ name: groupName, description: groupDescription }),
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Material group created'))
      setGroupDialog(false)
      setGroupName('')
      setGroupDescription('')
      await refresh()
    },
  })
  const uploadMutation = useMutation({
    mutationFn: () => {
      if (!uploadFile || !uploadGroup)
        throw new Error(t('Select a material group and file'))
      return uploadMaterial({
        groupId: Number(uploadGroup),
        name: uploadName || uploadFile.name,
        file: uploadFile,
      })
    },
    onSuccess: async (response) => {
      if (!response.success) return
      toast.success(t('Material uploaded and submitted for review'))
      setUploadDialog(false)
      setUploadGroup('')
      setUploadName('')
      setUploadFile(null)
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
          <Tabs defaultValue='groups'>
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
                <Button onClick={() => setGroupDialog(true)}>
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
                        <TableHead>{t('Group ID')}</TableHead>
                        <TableHead>{t('Type')}</TableHead>
                        <TableHead>{t('Assets')}</TableHead>
                        <TableHead>{t('Status')}</TableHead>
                        <TableHead>{t('Created at')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredGroups.map((group) => (
                        <TableRow key={group.id}>
                          <TableCell>
                            <div className='font-medium'>{group.name}</div>
                            <div className='text-muted-foreground text-xs'>
                              {group.description || '—'}
                            </div>
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
            <DialogTitle>{t('Create material group')}</DialogTitle>
            <DialogDescription>
              {t(
                'The group is created in the configured DoubaoVideo2.0 upstream account.'
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
              onClick={() => createMutation.mutate()}
              disabled={!groupName.trim() || createMutation.isPending}
            >
              {t('Create')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={uploadDialog} onOpenChange={setUploadDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('Upload material')}</DialogTitle>
            <DialogDescription>
              {t(
                'The file is stored temporarily, then submitted to the selected upstream material group for review.'
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
