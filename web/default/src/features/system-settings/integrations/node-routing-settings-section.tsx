import { useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, RefreshCw, Trash2 } from 'lucide-react'
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
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
  const [form, setForm] = useState<RoutingNodeInput>(emptyNode)

  const nodesQuery = useQuery({
    queryKey: ['routing-nodes', 'all'],
    queryFn: () => getRoutingNodes(true),
  })

  useEffect(() => {
    if (!dialogOpen) return
    setForm(
      editing
        ? {
            key: editing.key,
            name: editing.name,
            origin: editing.origin,
            enabled: editing.enabled,
            sort: editing.sort,
          }
        : emptyNode
    )
  }, [dialogOpen, editing])

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
        throw new Error(result.message || t('Rebuild failed'))
      return result.data?.started
    },
    onSuccess: (started) =>
      toast.success(
        started
          ? t('Routing rebuild started')
          : t('A routing rebuild is already running')
      ),
    onError: (error: Error) => toast.error(error.message),
  })

  const nodes = nodesQuery.data?.data || []

  return (
    <SettingsSection
      title={t('User Node Routing')}
      description={t(
        'Manage available origin nodes. Binding changes may take up to 60 seconds to propagate through Cloudflare KV.'
      )}
    >
      <div className='flex flex-wrap gap-2'>
        <Button
          disabled={nodesQuery.isError}
          onClick={() => {
            setEditing(null)
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
          {t('Rebuild routing')}
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
                  {node.binding_count}
                </TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      size='icon-sm'
                      variant='ghost'
                      title={t('Edit')}
                      onClick={() => {
                        setEditing(node)
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
