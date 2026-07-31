/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Edit02Icon,
  Loading03Icon,
  Rocket01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from '@/components/ui/field'
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemTitle,
} from '@/components/ui/item'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { MultiSelect } from '@/components/multi-select'
import { getGroups } from '@/features/channels/api'
import {
  FIELD_DESCRIPTIONS,
  FIELD_PLACEHOLDERS,
} from '@/features/channels/constants'
import { channelsQueryKeys } from '@/features/channels/lib'
import { getUpstreamPoolCapabilities, publishUpstreamPoolChannel } from './api'
import type { ApiResponse, UpstreamAccountPool } from './types'

type PublishPoolChannelDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  pool: UpstreamAccountPool
  onSaved?: () => void
}

function requireResponseData<T>(response: ApiResponse<T>): T {
  if (!response.success || response.data === undefined) {
    throw new Error(response.message || 'Request failed')
  }
  return response.data
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message) return error.message
  return fallback
}

export function PublishPoolChannelDrawer({
  open,
  onOpenChange,
  pool,
  onSaved,
}: PublishPoolChannelDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const initializedPoolId = useRef<number | null>(null)
  const [groups, setGroups] = useState<string[]>([])
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState('')

  const capabilitiesQuery = useQuery({
    queryKey: ['upstream-account-pools', pool.id, 'capabilities'],
    queryFn: async () =>
      requireResponseData(await getUpstreamPoolCapabilities(pool.id)),
    enabled: open,
  })
  const groupsQuery = useQuery({
    queryKey: ['groups'],
    queryFn: async () => requireResponseData(await getGroups()),
    enabled: open,
  })

  const capabilities = capabilitiesQuery.data
  const isPublished = capabilities
    ? Boolean(capabilities.published_channel_id)
    : pool.channel_count > 0

  useEffect(() => {
    if (!open) {
      initializedPoolId.current = null
      return
    }
    if (!capabilities || initializedPoolId.current === pool.id) return
    setGroups(
      capabilities.published_groups.length > 0
        ? capabilities.published_groups
        : ['default']
    )
    setSubmitError('')
    initializedPoolId.current = pool.id
  }, [capabilities, open, pool.id])

  const groupOptions = useMemo(() => {
    const options = new Set([...(groupsQuery.data || []), ...groups])
    return Array.from(options).map((group) => ({ label: group, value: group }))
  }, [groups, groupsQuery.data])

  const handleSave = async () => {
    if (groups.length === 0) {
      setSubmitError(t('At least one group is required'))
      return
    }
    setSaving(true)
    setSubmitError('')
    try {
      const result = requireResponseData(
        await publishUpstreamPoolChannel(pool.id, groups)
      )
      toast.success(
        t(
          result.created
            ? 'Channel created successfully'
            : 'Channel updated successfully'
        )
      )
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.all }),
        queryClient.invalidateQueries({
          queryKey: ['upstream-account-pools'],
        }),
        queryClient.invalidateQueries({
          queryKey: ['upstream-account-pools', pool.id, 'capabilities'],
        }),
      ])
      onSaved?.()
      onOpenChange(false)
    } catch (error) {
      setSubmitError(errorMessage(error, t('Failed to create channel')))
    } finally {
      setSaving(false)
    }
  }

  const loading = capabilitiesQuery.isLoading || groupsQuery.isLoading
  const loadError = capabilitiesQuery.error || groupsQuery.error
  const canPublish =
    !loading &&
    !loadError &&
    Boolean(capabilities?.models.length) &&
    groups.length > 0 &&
    !saving

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex h-dvh w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-xl'>
        <SheetHeader className='border-b px-5 py-4'>
          <SheetTitle>
            {t(isPublished ? 'Edit local channel' : 'Publish as local channel')}
          </SheetTitle>
          <SheetDescription>
            {t('Configure a channel backed by a local account pool.')}
          </SheetDescription>
        </SheetHeader>

        <div className='flex-1 space-y-6 overflow-y-auto px-5 py-5'>
          {loading ? (
            <div className='space-y-3'>
              <Skeleton className='h-20 w-full' />
              <Skeleton className='h-10 w-full' />
            </div>
          ) : loadError ? (
            <Alert variant='destructive'>
              <AlertDescription>
                {errorMessage(loadError, t('Failed to load'))}
              </AlertDescription>
            </Alert>
          ) : capabilities ? (
            <>
              <Item variant='outline' className='items-start'>
                <ItemContent>
                  <ItemTitle>{pool.name}</ItemTitle>
                  <ItemDescription>
                    {pool.platform} · {pool.credential_type}
                  </ItemDescription>
                  <div className='mt-2 flex flex-wrap gap-1.5'>
                    <Badge variant='secondary'>
                      {t('Accounts')}: {capabilities.account_count}
                    </Badge>
                    <Badge variant='secondary'>
                      {t('Schedulable')}:{' '}
                      {capabilities.schedulable_account_count}
                    </Badge>
                    <Badge variant='secondary'>
                      {t('Models')}: {capabilities.models.length}
                    </Badge>
                    {capabilities.passthrough_account_count > 0 && (
                      <Badge variant='outline'>
                        {t('Passthrough')}:{' '}
                        {capabilities.passthrough_account_count}
                      </Badge>
                    )}
                    {capabilities.proxy_configured_account_count > 0 && (
                      <Badge variant='outline'>
                        {t('Proxy')}:{' '}
                        {capabilities.proxy_configured_account_count}
                      </Badge>
                    )}
                    {capabilities.header_override_account_count > 0 && (
                      <Badge variant='outline'>
                        {t('Header override')}:{' '}
                        {capabilities.header_override_account_count}
                      </Badge>
                    )}
                  </div>
                  {capabilities.models.length > 0 ? (
                    <div className='mt-2 flex max-h-40 flex-wrap gap-1 overflow-y-auto'>
                      {capabilities.models.map((model) => (
                        <Badge key={model} variant='outline'>
                          {model}
                        </Badge>
                      ))}
                    </div>
                  ) : (
                    <Alert variant='destructive' className='mt-2'>
                      <AlertDescription>
                        {t('No concrete account models found in this pool')}
                      </AlertDescription>
                    </Alert>
                  )}
                </ItemContent>
              </Item>

              <FieldGroup>
                <Field data-invalid={groups.length === 0 || undefined}>
                  <FieldLabel>{t('Groups *')}</FieldLabel>
                  <MultiSelect
                    options={groupOptions}
                    selected={groups}
                    onChange={(values) => {
                      setGroups(values)
                      setSubmitError('')
                    }}
                    placeholder={t(FIELD_PLACEHOLDERS.GROUP)}
                  />
                  <FieldDescription>
                    {t(FIELD_DESCRIPTIONS.GROUP)}
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </>
          ) : null}

          {submitError && (
            <Alert variant='destructive'>
              <AlertDescription>{submitError}</AlertDescription>
            </Alert>
          )}
        </div>

        <SheetFooter className='border-t px-5 py-4 sm:flex-row sm:justify-end'>
          <Button
            type='button'
            variant='outline'
            disabled={saving}
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' disabled={!canPublish} onClick={handleSave}>
            <HugeiconsIcon
              icon={
                saving ? Loading03Icon : isPublished ? Edit02Icon : Rocket01Icon
              }
              className={saving ? 'animate-spin' : undefined}
              strokeWidth={2}
            />
            {t(isPublished ? 'Save changes' : 'Publish as local channel')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
