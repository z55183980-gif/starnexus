import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  ChevronLeft,
  ChevronRight,
  Pencil,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  createRoutingNode,
  deleteRoutingNode,
  getRoutingNodeBoundUsers,
  getRoutingNodes,
  reconcileRoutingNodes,
  updateRoutingNode,
} from '@/features/node-routing/api'
import type {
  RoutingNode,
  RoutingNodeInput,
} from '@/features/node-routing/types'
import { SettingsSection } from '../components/settings-section'

const emptyNode: RoutingNodeInput = {
  key: '',
  name: '',
  origin: '',
  enabled: true,
  sort: 0,
}

export function NodeRoutingSettingsSection() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editing, setEditing] = useState<RoutingNode | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<RoutingNode | null>(null)
  const [boundUsersNode, setBoundUsersNode] = useState<RoutingNode | null>(null)
  const [boundUsersPage, setBoundUsersPage] = useState(1)
  const [form, setForm] = useState<RoutingNodeInput>(emptyNode)

  const nodesQuery = useQuery({
    queryKey: ['routing-nodes', 'all'],
    queryFn: () => getRoutingNodes(true),
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
      const result = editing
        ? await updateRoutingNode(editing.id, form)
        : await createRoutingNode(form)
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

  const deleteMutation = useMutation({
    mutationFn: async (node: RoutingNode) => {
      const result = await deleteRoutingNode(node.id)
      if (!result.success) throw new Error(result.message || t('Delete failed'))
    },
    onSuccess: async () => {
      await refreshNodes()
      toast.success(t('Routing node deleted'))
      setDeleteTarget(null)
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

  const nodes = nodesQuery.data?.data || []
  const boundUsers = boundUsersQuery.data?.data?.items || []
  const boundUsersTotal = boundUsersQuery.data?.data?.total || 0
  const boundUsersTotalPages = Math.max(1, Math.ceil(boundUsersTotal / 20))

  return (
    <SettingsSection
      title={t('User Node Routing')}
      description={t(
        'Manage available origin nodes. Binding changes are synchronized automatically to Cloudflare KV and may take up to 60 seconds to take effect. Run a full synchronization if routing data becomes inconsistent.'
      )}
    >
      <div className='flex flex-wrap gap-2'>
        <Button
          disabled={nodesQuery.isError}
          onClick={() => {
            setEditing(null)
            setForm(emptyNode)
            setDialogOpen(true)
          }}
        >
          <Plus data-icon='inline-start' />
          {t('Add node')}
        </Button>
        <Button
          variant='outline'
          disabled={reconcileMutation.isPending}
          onClick={() => reconcileMutation.mutate()}
        >
          <RefreshCw data-icon='inline-start' />
          {t('Synchronize all routing')}
        </Button>
      </div>

      <div className='overflow-hidden rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Node')}</TableHead>
              <TableHead>{t('Origin')}</TableHead>
              <TableHead>{t('Status')}</TableHead>
              <TableHead className='text-right'>{t('Bound users')}</TableHead>
              <TableHead className='w-24 text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {nodes.map((node) => (
              <TableRow key={node.id}>
                <TableCell>
                  <div className='flex flex-col'>
                    <span className='font-medium'>{node.name}</span>
                    <span className='text-muted-foreground font-mono text-xs'>
                      {node.key}
                    </span>
                  </div>
                </TableCell>
                <TableCell className='font-mono text-xs'>
                  {node.origin}
                </TableCell>
                <TableCell>
                  <Badge variant={node.enabled ? 'default' : 'secondary'}>
                    {node.enabled ? t('Enabled') : t('Disabled')}
                  </Badge>
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {node.binding_count > 0 ? (
                    <Button
                      variant='link'
                      size='sm'
                      className='h-auto min-w-8 px-1 tabular-nums'
                      title={t('View bound users')}
                      onClick={() => {
                        setBoundUsersPage(1)
                        setBoundUsersNode(node)
                      }}
                    >
                      {node.binding_count}
                    </Button>
                  ) : (
                    node.binding_count
                  )}
                </TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      size='icon-sm'
                      variant='ghost'
                      title={t('Edit')}
                      onClick={() => {
                        setEditing(node)
                        setForm({
                          key: node.key,
                          name: node.name,
                          origin: node.origin,
                          enabled: node.enabled,
                          sort: node.sort,
                        })
                        setDialogOpen(true)
                      }}
                    >
                      <Pencil />
                    </Button>
                    <Button
                      size='icon-sm'
                      variant='ghost'
                      title={t('Delete')}
                      disabled={
                        node.binding_count > 0 || deleteMutation.isPending
                      }
                      onClick={() => setDeleteTarget(node)}
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {!nodesQuery.isLoading && nodes.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={5}
                  className='text-muted-foreground py-8 text-center'
                >
                  {nodesQuery.isError
                    ? t('Failed to load')
                    : t('No routing nodes')}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {editing ? t('Edit node') : t('Add node')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'The origin must be a proxied hostname under the allowed domain.'
              )}
            </DialogDescription>
          </DialogHeader>
          <div className='grid gap-4'>
            <div className='grid gap-2'>
              <Label htmlFor='routing-node-key'>{t('Node key')}</Label>
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
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='routing-node-name'>{t('Display name')}</Label>
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
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='routing-node-origin'>
                {t('Origin hostname')}
              </Label>
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
            </div>
            <div className='grid gap-2'>
              <Label htmlFor='routing-node-sort'>{t('Sort order')}</Label>
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
            </div>
            <div className='flex items-center justify-between rounded-lg border p-3'>
              <Label htmlFor='routing-node-enabled'>{t('Enabled')}</Label>
              <Switch
                id='routing-node-enabled'
                checked={form.enabled}
                disabled={Boolean(editing?.binding_count)}
                onCheckedChange={(enabled) =>
                  setForm((current) => ({ ...current, enabled }))
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant='outline' onClick={() => setDialogOpen(false)}>
              {t('Cancel')}
            </Button>
            <Button
              disabled={
                saveMutation.isPending ||
                !form.key.trim() ||
                !form.name.trim() ||
                !form.origin.trim()
              }
              onClick={() => saveMutation.mutate()}
            >
              {saveMutation.isPending ? t('Saving...') : t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(boundUsersNode)}
        onOpenChange={(open) => {
          if (!open) setBoundUsersNode(null)
        }}
      >
        <DialogContent className='sm:max-w-2xl'>
          <DialogHeader>
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
                <EmptyTitle>{t('No users are bound to this node')}</EmptyTitle>
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
                  <ChevronLeft />
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
                  <ChevronRight />
                </Button>
              </div>
            </DialogFooter>
          )}
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => !open && setDeleteTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Delete routing node {{name}}?', {
                name: deleteTarget?.name || '',
              })}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteMutation.isPending}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              disabled={deleteMutation.isPending || !deleteTarget}
              onClick={() =>
                deleteTarget && deleteMutation.mutate(deleteTarget)
              }
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </SettingsSection>
  )
}
