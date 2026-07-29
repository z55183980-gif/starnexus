import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ArrowLeft01Icon,
  ArrowRight01Icon,
  Copy01Icon,
  Edit02Icon,
  Key01Icon,
  RefreshIcon,
  UserRemove01Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
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
  CardFooter,
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
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSet,
  FieldTitle,
} from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
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
import { Textarea } from '@/components/ui/textarea'
import { SectionPageLayout } from '@/components/layout'
import {
  getRoutingNodeBoundUsers,
  getRoutingNodeMonitorEnrollmentToken,
  getRoutingNodes,
  reconcileRoutingNodes,
  removeRoutingNodeUserBinding,
  rotateRoutingNodeMonitorEnrollmentToken,
  rotateRoutingNodeMonitorToken,
  updateRoutingNode,
} from '@/features/node-routing/api'
import type {
  RoutingNode,
  RoutingNodeBoundUser,
  RoutingNodeInput,
  RoutingNodeMonitorEnrollment,
  RoutingNodeMonitorSharedEnrollment,
} from '@/features/node-routing/types'
import {
  RoutingNodeDatabaseOverview,
  RoutingNodeMonitorBadge,
  RoutingNodeNetworkTraffic,
  RoutingNodeResourceOverview,
} from './node-monitor-view'

const emptyNode: RoutingNodeInput = {
  key: '',
  name: '',
  origin: '',
  type: 'application',
  enabled: true,
  visible: true,
  sort: 0,
  monitor_enabled: true,
}

const nodeCardTone = {
  card: 'border-chart-2/25 bg-chart-2/[0.025] ring-chart-2/10 before:bg-chart-2/80',
  header: 'border-chart-2/15 bg-chart-2/[0.025]',
  footer: 'border-chart-2/15 bg-chart-2/[0.06]',
} as const

const databaseNodeCardTone = {
  card: 'border-chart-3/30 bg-chart-3/[0.035] ring-chart-3/10 before:bg-chart-3/75',
  header: 'border-chart-3/20 bg-chart-3/[0.04]',
  footer: 'border-chart-3/20 bg-chart-3/[0.075]',
} as const

const fixedNodeKeys = new Set(['s1', 's2', 's3', 's4', 'spg'])

export function NodeRoutingSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<RoutingNode | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [visibilityDialogOpen, setVisibilityDialogOpen] = useState(false)
  const [visibilityDraft, setVisibilityDraft] = useState<
    Record<number, boolean>
  >({})
  const [boundUsersNode, setBoundUsersNode] = useState<RoutingNode | null>(null)
  const [boundUsersPage, setBoundUsersPage] = useState(1)
  const [boundUsersEditing, setBoundUsersEditing] = useState(false)
  const [removeBindingTarget, setRemoveBindingTarget] =
    useState<RoutingNodeBoundUser | null>(null)
  const [monitorEnrollment, setMonitorEnrollment] =
    useState<RoutingNodeMonitorEnrollment | null>(null)
  const [sharedMonitorEnrollment, setSharedMonitorEnrollment] =
    useState<RoutingNodeMonitorSharedEnrollment | null>(null)
  const [form, setForm] = useState<RoutingNodeInput>(emptyNode)

  const nodesQuery = useQuery({
    queryKey: ['routing-nodes', 'all'],
    queryFn: () => getRoutingNodes(true),
    refetchInterval: 15_000,
  })

  const boundUsersQuery = useQuery({
    queryKey: ['routing-node-bound-users', boundUsersNode?.id, boundUsersPage],
    queryFn: () =>
      getRoutingNodeBoundUsers(boundUsersNode!.id, boundUsersPage, 20),
    enabled: Boolean(boundUsersNode),
  })

  const refreshNodes = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: ['routing-nodes'] }),
      queryClient.invalidateQueries({ queryKey: ['routing-nodes', 'all'] }),
    ])
  }

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!editing) throw new Error(t('Save failed'))
      const result = await updateRoutingNode(editing.id, form)
      if (!result.success) throw new Error(result.message || t('Save failed'))
      return result
    },
    onSuccess: async () => {
      await refreshNodes()
      toast.success(t('Routing node saved'))
      setDialogOpen(false)
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const visibilityMutation = useMutation({
    mutationFn: async () => {
      const changes = nodes
        .filter(
          (node) =>
            fixedNodeKeys.has(node.key) &&
            visibilityDraft[node.id] !== undefined &&
            visibilityDraft[node.id] !== node.visible
        )
        .map((node) =>
          updateRoutingNode(node.id, {
            key: node.key,
            name: node.name,
            origin: node.origin,
            type: node.type,
            enabled: node.enabled,
            visible: visibilityDraft[node.id],
            sort: node.sort,
            monitor_enabled: node.monitor_enabled,
          })
        )
      const results = await Promise.all(changes)
      const failed = results.find((result) => !result.success)
      if (failed) throw new Error(failed.message || t('Save failed'))
    },
    onSuccess: async () => {
      await refreshNodes()
      setVisibilityDialogOpen(false)
      toast.success(t('Saved successfully'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const reconcileMutation = useMutation({
    mutationFn: async () => {
      const result = await reconcileRoutingNodes()
      if (!result.success)
        throw new Error(result.message || t('Routing synchronization failed'))
      return result.data?.started
    },
    onSuccess: (started) =>
      toast.success(
        started
          ? t('Routing synchronization started')
          : t('A routing synchronization is already running')
      ),
    onError: (error: Error) => toast.error(error.message),
  })

  const tokenMutation = useMutation({
    mutationFn: async (node: RoutingNode) => {
      const result = await rotateRoutingNodeMonitorToken(node.id)
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to generate monitor token'))
      }
      return result.data
    },
    onSuccess: async (enrollment) => {
      await refreshNodes()
      setMonitorEnrollment(enrollment)
      toast.success(t('Monitor token generated'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const sharedEnrollmentMutation = useMutation({
    mutationFn: async () => {
      const result = await rotateRoutingNodeMonitorEnrollmentToken()
      if (!result.success || !result.data) {
        throw new Error(
          result.message || t('Failed to generate enrollment key')
        )
      }
      return result.data
    },
    onSuccess: (enrollment) => {
      setSharedMonitorEnrollment(enrollment)
      toast.success(t('Enrollment key generated'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const viewSharedEnrollmentMutation = useMutation({
    mutationFn: async () => {
      const result = await getRoutingNodeMonitorEnrollmentToken()
      if (!result.success || !result.data) {
        throw new Error(result.message || t('Failed to load enrollment key'))
      }
      if (result.data.token) return result.data

      const generated = await rotateRoutingNodeMonitorEnrollmentToken()
      if (!generated.success || !generated.data) {
        throw new Error(
          generated.message || t('Failed to generate enrollment key')
        )
      }
      return generated.data
    },
    onSuccess: (enrollment) => setSharedMonitorEnrollment(enrollment),
    onError: (error: Error) => toast.error(error.message),
  })

  const removeBindingMutation = useMutation({
    mutationFn: async (user: RoutingNodeBoundUser) => {
      const result = await removeRoutingNodeUserBinding(user.user_id)
      if (!result.success) {
        throw new Error(result.message || t('Failed to remove binding'))
      }
    },
    onSuccess: async () => {
      if (boundUsers.length === 1 && boundUsersPage > 1) {
        setBoundUsersPage((page) => page - 1)
      }
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['routing-node-bound-users', boundUsersNode?.id],
        }),
        refreshNodes(),
      ])
      setRemoveBindingTarget(null)
      toast.success(t('User binding removed'))
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const nodes = nodesQuery.data?.data || []
  const fixedNodes = nodes.filter((node) => fixedNodeKeys.has(node.key))
  const visibleNodes = fixedNodes.filter((node) => node.visible)
  const boundUsers = boundUsersQuery.data?.data?.items || []
  const boundUsersTotal = boundUsersQuery.data?.data?.total || 0
  const boundUsersTotalPages = Math.max(1, Math.ceil(boundUsersTotal / 20))
  const reportOrigin =
    typeof window === 'undefined' ? '' : window.location.origin
  const enrollmentConfig = monitorEnrollment
    ? [
        'NODE_MONITOR_ENABLED=true',
        `NODE_MONITOR_REPORT_URL=${reportOrigin}${monitorEnrollment.report_path}`,
        `NODE_MONITOR_TOKEN=${monitorEnrollment.token}`,
      ].join('\n')
    : ''
  const sharedEnrollmentConfig = sharedMonitorEnrollment
    ? [
        'NODE_MONITOR_ENABLED=true',
        `NODE_MONITOR_REPORT_URL=${reportOrigin}${sharedMonitorEnrollment.report_path}`,
        `NODE_MONITOR_ENROLLMENT_TOKEN=${sharedMonitorEnrollment.token}`,
      ].join('\n')
    : ''

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Routing Management')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Description>
        {t(
          'Manage available origin nodes. Binding changes are synchronized automatically to Cloudflare KV and may take up to 60 seconds to take effect. Run a full synchronization if routing data becomes inconsistent.'
        )}
      </SectionPageLayout.Description>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          disabled={
            viewSharedEnrollmentMutation.isPending ||
            sharedEnrollmentMutation.isPending
          }
          title={t(
            'View the shared enrollment key. Existing node monitor tokens remain valid when this key is rotated.'
          )}
          onClick={() => viewSharedEnrollmentMutation.mutate()}
        >
          {viewSharedEnrollmentMutation.isPending ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <HugeiconsIcon icon={Key01Icon} data-icon='inline-start' />
          )}
          {t('View enrollment key')}
        </Button>
        <Button
          disabled={nodesQuery.isError}
          onClick={() => {
            setVisibilityDraft(
              Object.fromEntries(
                fixedNodes.map((node) => [node.id, node.visible])
              )
            )
            setVisibilityDialogOpen(true)
          }}
        >
          <HugeiconsIcon icon={Edit02Icon} data-icon='inline-start' />
          {t('Edit')}
        </Button>
        <Button
          variant='outline'
          disabled={reconcileMutation.isPending}
          onClick={() => reconcileMutation.mutate()}
        >
          {reconcileMutation.isPending ? (
            <Spinner data-icon='inline-start' />
          ) : (
            <HugeiconsIcon icon={RefreshIcon} data-icon='inline-start' />
          )}
          {t('Synchronize all routing')}
        </Button>
      </SectionPageLayout.Actions>

      <SectionPageLayout.Content>
        {nodesQuery.isLoading ? (
          <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {Array.from({ length: 4 }).map((_, index) => (
              <Card key={index} className='min-h-[calc(100dvh-10rem)]'>
                <CardHeader>
                  <Skeleton className='h-5 w-32' />
                  <Skeleton className='h-4 w-48' />
                </CardHeader>
                <CardContent className='grid grid-cols-2 gap-4'>
                  {Array.from({ length: 4 }).map((__, metricIndex) => (
                    <Skeleton key={metricIndex} className='h-12 w-full' />
                  ))}
                  <Skeleton className='col-span-2 h-52 w-full' />
                </CardContent>
                <CardFooter className='grid grid-cols-3 gap-2'>
                  {Array.from({ length: 3 }).map((__, actionIndex) => (
                    <Skeleton key={actionIndex} className='h-8 w-full' />
                  ))}
                </CardFooter>
              </Card>
            ))}
          </div>
        ) : nodes.length === 0 ? (
          <Empty className='border'>
            <EmptyHeader>
              <EmptyTitle>
                {nodesQuery.isError
                  ? t('Failed to load')
                  : t('No routing nodes')}
              </EmptyTitle>
              {nodesQuery.isError && (
                <EmptyDescription>
                  {t('Please try again later')}
                </EmptyDescription>
              )}
            </EmptyHeader>
          </Empty>
        ) : visibleNodes.length === 0 ? (
          <Empty className='border'>
            <EmptyHeader>
              <EmptyTitle>{t('No visible nodes')}</EmptyTitle>
              <EmptyDescription>
                {t('Use Edit to show server cards on the main page.')}
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4'>
            {visibleNodes.map((node) => {
              const databaseNode = node.type === 'database'
              const cardTone = databaseNode
                ? databaseNodeCardTone
                : nodeCardTone
              return (
                <Card
                  key={node.id}
                  className={cn(
                    'relative min-h-[calc(100dvh-10rem)] border pt-5 transition-[transform,box-shadow] before:absolute before:inset-x-0 before:top-0 before:h-1 hover:-translate-y-0.5 hover:shadow-md motion-reduce:transform-none',
                    cardTone.card,
                    !node.enabled && 'opacity-75 saturate-50'
                  )}
                >
                  <CardHeader className={cn('border-b pb-4', cardTone.header)}>
                    <CardTitle className='flex min-w-0 items-center gap-2'>
                      <span className='truncate'>{node.name}</span>
                      <Badge
                        variant={
                          databaseNode
                            ? 'secondary'
                            : node.enabled
                              ? 'default'
                              : 'secondary'
                        }
                      >
                        {databaseNode
                          ? t('Database')
                          : node.enabled
                            ? t('Enabled')
                            : t('Disabled')}
                      </Badge>
                    </CardTitle>
                    <CardDescription className='flex min-w-0 items-center gap-2'>
                      <span className='font-mono'>{node.key}</span>
                      <span aria-hidden='true'>·</span>
                      <span className='truncate font-mono'>
                        {databaseNode ? t('Database') : node.origin}
                      </span>
                    </CardDescription>
                  </CardHeader>

                  <CardContent className='flex flex-1 flex-col gap-4'>
                    {!databaseNode && (
                      <div className='flex flex-wrap items-center justify-between gap-2'>
                        <RoutingNodeMonitorBadge node={node} />
                        <Badge
                          variant={
                            node.binding_count > 0 ? 'secondary' : 'outline'
                          }
                          render={<button type='button' />}
                          className='cursor-pointer tabular-nums'
                          title={t('View bound users')}
                          aria-label={t('View bound users')}
                          onClick={() => {
                            setBoundUsersPage(1)
                            setBoundUsersEditing(false)
                            setBoundUsersNode(node)
                          }}
                        >
                          {t('Bound users')}: {node.binding_count}
                        </Badge>
                      </div>
                    )}

                    {!databaseNode && <Separator />}

                    {databaseNode && (
                      <>
                        <RoutingNodeDatabaseOverview node={node} />
                        <Separator />
                      </>
                    )}

                    <RoutingNodeResourceOverview node={node} />

                    <Separator />

                    <RoutingNodeNetworkTraffic node={node} />

                    <div className='text-muted-foreground mt-auto text-xs'>
                      {node.monitor_status?.reported_at
                        ? t('Last report: {{time}}', {
                            time: new Date(
                              node.monitor_status.reported_at * 1000
                            ).toLocaleString(),
                          })
                        : t('No monitoring data has been reported yet')}
                    </div>
                  </CardContent>

                  <CardFooter
                    className={cn(
                      'grid gap-2 p-2',
                      databaseNode ? 'grid-cols-2' : 'grid-cols-3',
                      cardTone.footer
                    )}
                  >
                    {!databaseNode && (
                      <Button
                        size='sm'
                        variant='ghost'
                        onClick={() => {
                          setBoundUsersPage(1)
                          setBoundUsersEditing(false)
                          setBoundUsersNode(node)
                        }}
                      >
                        <HugeiconsIcon
                          icon={UserRemove01Icon}
                          data-icon='inline-start'
                        />
                        {t('Users')}
                      </Button>
                    )}
                    <Button
                      size='sm'
                      variant='ghost'
                      onClick={() => {
                        setEditing(node)
                        setForm({
                          key: node.key,
                          name: node.name,
                          origin: node.origin,
                          type: node.type,
                          enabled: node.enabled,
                          visible: node.visible,
                          sort: node.sort,
                          monitor_enabled: node.monitor_enabled,
                        })
                        setDialogOpen(true)
                      }}
                    >
                      <HugeiconsIcon
                        icon={Edit02Icon}
                        data-icon='inline-start'
                      />
                      {t('Edit')}
                    </Button>
                    <Button
                      size='sm'
                      variant='ghost'
                      title={
                        node.monitor_configured
                          ? t('Rotate monitor token')
                          : t('Generate monitor token')
                      }
                      disabled={tokenMutation.isPending}
                      onClick={() => tokenMutation.mutate(node)}
                    >
                      <HugeiconsIcon
                        icon={Key01Icon}
                        data-icon='inline-start'
                      />
                      {t('Token')}
                    </Button>
                  </CardFooter>
                </Card>
              )
            })}
          </div>
        )}

        <Dialog
          open={visibilityDialogOpen}
          onOpenChange={setVisibilityDialogOpen}
        >
          <DialogContent className='sm:max-w-md'>
            <DialogHeader>
              <DialogTitle>{t('Edit')}</DialogTitle>
              <DialogDescription>
                {t('Choose which server cards are shown on the main page.')}
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              {fixedNodes.map((node) => (
                <Field
                  key={node.id}
                  orientation='horizontal'
                  className='bg-muted/35 rounded-lg border p-3'
                >
                  <FieldContent>
                    <FieldTitle>{node.name}</FieldTitle>
                    <FieldDescription className='font-mono'>
                      {node.key} ·{' '}
                      {node.type === 'database' ? 'PostgreSQL' : node.origin}
                    </FieldDescription>
                  </FieldContent>
                  <Switch
                    checked={visibilityDraft[node.id] ?? node.visible}
                    aria-label={`${t('Show')} ${node.name}`}
                    onCheckedChange={(checked) =>
                      setVisibilityDraft((current) => ({
                        ...current,
                        [node.id]: checked,
                      }))
                    }
                  />
                </Field>
              ))}
            </FieldGroup>
            <DialogFooter>
              <Button
                variant='outline'
                onClick={() => setVisibilityDialogOpen(false)}
              >
                {t('Cancel')}
              </Button>
              <Button
                disabled={visibilityMutation.isPending}
                onClick={() => visibilityMutation.mutate()}
              >
                {visibilityMutation.isPending && (
                  <Spinner data-icon='inline-start' />
                )}
                {t('Save')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogContent className='sm:max-w-lg'>
            <DialogHeader>
              <DialogTitle>{t('Edit node')}</DialogTitle>
              <DialogDescription>
                {form.type === 'database'
                  ? t('Database')
                  : t(
                      'The origin must be a proxied hostname under the allowed domain.'
                    )}
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor='routing-node-key'>
                  {t('Node key')}
                </FieldLabel>
                <Input
                  id='routing-node-key'
                  value={form.key}
                  disabled={Boolean(editing)}
                  placeholder='s5'
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      key: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor='routing-node-name'>
                  {t('Display name')}
                </FieldLabel>
                <Input
                  id='routing-node-name'
                  value={form.name}
                  placeholder='S5'
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      name: event.target.value,
                    }))
                  }
                />
              </Field>
              {form.type === 'application' && (
                <Field>
                  <FieldLabel htmlFor='routing-node-origin'>
                    {t('Origin hostname')}
                  </FieldLabel>
                  <Input
                    id='routing-node-origin'
                    value={form.origin}
                    placeholder='origin-s5.dkby.com'
                    onChange={(event) =>
                      setForm((current) => ({
                        ...current,
                        origin: event.target.value,
                      }))
                    }
                  />
                </Field>
              )}
              <Field>
                <FieldLabel htmlFor='routing-node-sort'>
                  {t('Sort order')}
                </FieldLabel>
                <Input
                  id='routing-node-sort'
                  type='number'
                  value={form.sort}
                  onChange={(event) =>
                    setForm((current) => ({
                      ...current,
                      sort: Number(event.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field orientation='horizontal'>
                <FieldLabel htmlFor='routing-node-enabled'>
                  {t('Enabled')}
                </FieldLabel>
                <Switch
                  id='routing-node-enabled'
                  checked={form.enabled}
                  disabled={Boolean(editing?.binding_count)}
                  onCheckedChange={(enabled) =>
                    setForm((current) => ({ ...current, enabled }))
                  }
                />
              </Field>
              <FieldSet>
                <FieldLegend variant='label'>
                  {t('Node monitoring')}
                </FieldLegend>
                <FieldDescription>
                  {t(
                    'The node reports system metrics with a dedicated token. No server password is required.'
                  )}
                </FieldDescription>
                <FieldGroup className='gap-3'>
                  <Field orientation='horizontal'>
                    <FieldContent>
                      <FieldTitle id='routing-node-monitor-label'>
                        {t('Enable monitoring')}
                      </FieldTitle>
                      <FieldDescription>
                        {t(
                          'Report load, CPU, memory, disk, uptime, and version'
                        )}
                      </FieldDescription>
                    </FieldContent>
                    <Switch
                      aria-labelledby='routing-node-monitor-label'
                      checked={form.monitor_enabled}
                      onCheckedChange={(monitor_enabled) =>
                        setForm((current) => ({
                          ...current,
                          monitor_enabled,
                        }))
                      }
                    />
                  </Field>
                </FieldGroup>
              </FieldSet>
            </FieldGroup>
            <DialogFooter>
              <Button variant='outline' onClick={() => setDialogOpen(false)}>
                {t('Cancel')}
              </Button>
              <Button
                disabled={
                  saveMutation.isPending ||
                  !form.key.trim() ||
                  !form.name.trim() ||
                  (form.type === 'application' && !form.origin.trim())
                }
                onClick={() => saveMutation.mutate()}
              >
                {saveMutation.isPending && <Spinner data-icon='inline-start' />}
                {saveMutation.isPending ? t('Saving...') : t('Save')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          open={Boolean(boundUsersNode)}
          onOpenChange={(open) => {
            if (!open) {
              setBoundUsersNode(null)
              setBoundUsersEditing(false)
              setRemoveBindingTarget(null)
            }
          }}
        >
          <DialogContent className='sm:max-w-2xl'>
            <Button
              variant={boundUsersEditing ? 'secondary' : 'outline'}
              size='sm'
              className='absolute top-2 right-10'
              disabled={boundUsersQuery.isLoading || boundUsersTotal === 0}
              onClick={() => setBoundUsersEditing((editing) => !editing)}
            >
              {!boundUsersEditing && (
                <HugeiconsIcon icon={Edit02Icon} data-icon='inline-start' />
              )}
              {boundUsersEditing ? t('Done') : t('Edit')}
            </Button>
            <DialogHeader className='pr-32'>
              <DialogTitle>{t('Bound users')}</DialogTitle>
              <DialogDescription>
                {t('Users currently bound to {{node}}', {
                  node: boundUsersNode?.name || '',
                })}
              </DialogDescription>
            </DialogHeader>

            {boundUsersQuery.isLoading ? (
              <div className='flex flex-col gap-2'>
                {Array.from({ length: 5 }).map((_, index) => (
                  <Skeleton key={index} className='h-9 w-full' />
                ))}
              </div>
            ) : boundUsersQuery.isError ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>{t('Failed to load bound users')}</EmptyTitle>
                  <EmptyDescription>
                    {boundUsersQuery.error instanceof Error
                      ? boundUsersQuery.error.message
                      : t('Please try again later')}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : boundUsers.length === 0 ? (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>
                    {t('No users are bound to this node')}
                  </EmptyTitle>
                </EmptyHeader>
              </Empty>
            ) : (
              <ScrollArea className='max-h-[50vh]'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className='w-24'>{t('User ID')}</TableHead>
                      <TableHead>{t('Username')}</TableHead>
                      <TableHead>{t('Display name')}</TableHead>
                      {boundUsersEditing && (
                        <TableHead className='w-32 text-right'>
                          {t('Actions')}
                        </TableHead>
                      )}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {boundUsers.map((user) => (
                      <TableRow key={user.user_id}>
                        <TableCell className='font-mono tabular-nums'>
                          {user.user_id}
                        </TableCell>
                        <TableCell className='font-medium'>
                          {user.username}
                        </TableCell>
                        <TableCell className='text-muted-foreground'>
                          {user.display_name || '-'}
                        </TableCell>
                        {boundUsersEditing && (
                          <TableCell className='text-right'>
                            <Button
                              variant='destructive'
                              size='sm'
                              disabled={removeBindingMutation.isPending}
                              onClick={() => setRemoveBindingTarget(user)}
                            >
                              <HugeiconsIcon
                                icon={UserRemove01Icon}
                                data-icon='inline-start'
                              />
                              {t('Remove binding')}
                            </Button>
                          </TableCell>
                        )}
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </ScrollArea>
            )}

            {boundUsersTotal > 20 && (
              <DialogFooter className='items-center sm:justify-between'>
                <span className='text-muted-foreground text-sm tabular-nums'>
                  {t('Page {{page}} of {{total}}', {
                    page: boundUsersPage,
                    total: boundUsersTotalPages,
                  })}
                </span>
                <div className='flex gap-1'>
                  <Button
                    size='icon-sm'
                    variant='outline'
                    title={t('Previous page')}
                    disabled={boundUsersPage <= 1 || boundUsersQuery.isFetching}
                    onClick={() => setBoundUsersPage((page) => page - 1)}
                  >
                    <HugeiconsIcon icon={ArrowLeft01Icon} strokeWidth={2} />
                  </Button>
                  <Button
                    size='icon-sm'
                    variant='outline'
                    title={t('Next page')}
                    disabled={
                      boundUsersPage >= boundUsersTotalPages ||
                      boundUsersQuery.isFetching
                    }
                    onClick={() => setBoundUsersPage((page) => page + 1)}
                  >
                    <HugeiconsIcon icon={ArrowRight01Icon} strokeWidth={2} />
                  </Button>
                </div>
              </DialogFooter>
            )}
          </DialogContent>
        </Dialog>

        <AlertDialog
          open={Boolean(removeBindingTarget)}
          onOpenChange={(open) => !open && setRemoveBindingTarget(null)}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {t('Remove binding for {{user}}?', {
                  user:
                    removeBindingTarget?.display_name ||
                    removeBindingTarget?.username ||
                    '',
                })}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {t('The user will return to automatic node routing.')}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={removeBindingMutation.isPending}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                variant='destructive'
                disabled={
                  removeBindingMutation.isPending || !removeBindingTarget
                }
                onClick={() =>
                  removeBindingTarget &&
                  removeBindingMutation.mutate(removeBindingTarget)
                }
              >
                {removeBindingMutation.isPending && (
                  <Spinner data-icon='inline-start' />
                )}
                {t('Remove binding')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <Dialog
          open={Boolean(monitorEnrollment)}
          onOpenChange={(open) => {
            if (!open) setMonitorEnrollment(null)
          }}
        >
          <DialogContent className='sm:max-w-2xl'>
            <DialogHeader>
              <DialogTitle>{t('Node monitor deployment')}</DialogTitle>
              <DialogDescription>
                {t(
                  'This token is shown only once. Add these environment variables to the matching node and restart it.'
                )}{' '}
                {t('Keep the existing NODE_NAME unchanged.')}
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor='node-monitor-config'>
                  {t('Environment variables')}
                </FieldLabel>
                <Textarea
                  id='node-monitor-config'
                  readOnly
                  value={enrollmentConfig}
                  className='min-h-40 font-mono text-xs'
                />
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button
                variant='outline'
                onClick={async () => {
                  await navigator.clipboard.writeText(enrollmentConfig)
                  toast.success(t('Configuration copied'))
                }}
              >
                <HugeiconsIcon icon={Copy01Icon} data-icon='inline-start' />
                {t('Copy configuration')}
              </Button>
              <Button onClick={() => setMonitorEnrollment(null)}>
                {t('Done')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog
          open={Boolean(sharedMonitorEnrollment)}
          onOpenChange={(open) => {
            if (!open) setSharedMonitorEnrollment(null)
          }}
        >
          <DialogContent className='sm:max-w-2xl'>
            <DialogHeader>
              <DialogTitle>{t('Automatic node enrollment')}</DialogTitle>
              <DialogDescription>
                {t(
                  'Use this enrollment key on every node. NODE_NAME values such as xingyuapi-prod-2 are mapped to routing node keys such as s2 automatically.'
                )}{' '}
                {t('Keep the existing NODE_NAME unchanged.')}
              </DialogDescription>
            </DialogHeader>
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor='node-monitor-shared-config'>
                  {t('Shared enrollment configuration')}
                </FieldLabel>
                <FieldDescription>
                  {t(
                    'If NODE_NAME does not match the routing node key, set NODE_MONITOR_NODE_KEY as an override.'
                  )}
                </FieldDescription>
                <Textarea
                  id='node-monitor-shared-config'
                  readOnly
                  value={sharedEnrollmentConfig}
                  className='min-h-40 font-mono text-xs'
                />
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button
                variant='outline'
                disabled={sharedEnrollmentMutation.isPending}
                onClick={() => sharedEnrollmentMutation.mutate()}
              >
                {sharedEnrollmentMutation.isPending && (
                  <Spinner data-icon='inline-start' />
                )}
                {t('Regenerate enrollment key')}
              </Button>
              <Button
                variant='outline'
                onClick={async () => {
                  await navigator.clipboard.writeText(sharedEnrollmentConfig)
                  toast.success(t('Configuration copied'))
                }}
              >
                <HugeiconsIcon icon={Copy01Icon} data-icon='inline-start' />
                {t('Copy configuration')}
              </Button>
              <Button onClick={() => setSharedMonitorEnrollment(null)}>
                {t('Done')}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
