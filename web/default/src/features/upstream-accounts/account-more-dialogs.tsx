import { useEffect, useState } from 'react'
import {
  Add01Icon,
  Delete02Icon,
  Edit02Icon,
  Loading03Icon,
} from '@hugeicons/core-free-icons'
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
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
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
  createUpstreamAccountScheduledTest,
  deleteUpstreamAccountScheduledTest,
  getUpstreamAccountStats,
  listUpstreamAccountScheduledTestResults,
  listUpstreamAccountScheduledTests,
  updateUpstreamAccountScheduledTest,
} from './api'
import type {
  UpstreamAccount,
  UpstreamAccountScheduledTestPlan,
  UpstreamAccountScheduledTestPlanPayload,
  UpstreamAccountScheduledTestResult,
  UpstreamAccountStats,
} from './types'

function formatTime(timestamp?: number | null) {
  return timestamp ? new Date(timestamp * 1000).toLocaleString() : '-'
}

export function AccountStatsDialog({
  open,
  account,
  onOpenChange,
}: {
  open: boolean
  account: UpstreamAccount | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [stats, setStats] = useState<UpstreamAccountStats | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open || !account) return
    let active = true
    setLoading(true)
    setStats(null)
    void getUpstreamAccountStats(account.id, 30)
      .then((response) => {
        if (!active) return
        if (!response.success || !response.data) {
          throw new Error(response.message || t('Failed to load statistics'))
        }
        setStats(response.data)
      })
      .catch((error) => {
        if (active)
          toast.error(
            error instanceof Error
              ? error.message
              : t('Failed to load statistics')
          )
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [account, open, t])

  const metrics = stats
    ? [
        { label: t('Selected requests'), value: stats.selected_count },
        { label: t('Successful requests'), value: stats.success_count },
        { label: t('Failed requests'), value: stats.error_count },
        { label: t('Connection tests'), value: stats.test_count },
      ]
    : []

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Account statistics')}</DialogTitle>
          <DialogDescription>
            {t('Statistics for {{name}} over the last 30 days.', {
              name: account?.name || '-',
            })}
          </DialogDescription>
        </DialogHeader>
        {loading ? (
          <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {Array.from({ length: 4 }).map((_, index) => (
              <Skeleton key={index} className='h-20' />
            ))}
          </div>
        ) : stats ? (
          <div className='flex flex-col gap-4'>
            <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
              {metrics.map((metric) => (
                <div key={metric.label} className='rounded-lg border p-3'>
                  <div className='text-muted-foreground text-xs'>
                    {metric.label}
                  </div>
                  <div className='mt-1 text-xl font-semibold tabular-nums'>
                    {metric.value.toLocaleString()}
                  </div>
                </div>
              ))}
            </div>
            <div className='flex items-center justify-between rounded-lg border p-3'>
              <span className='text-sm font-medium'>{t('Success rate')}</span>
              <Badge variant='success'>{stats.success_rate.toFixed(1)}%</Badge>
            </div>
            <div className='flex flex-col gap-2'>
              <div className='text-sm font-medium'>
                {t('Recent connection tests')}
              </div>
              <div className='max-h-72 overflow-auto rounded-lg border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Time')}</TableHead>
                      <TableHead>{t('Model')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('First output')}</TableHead>
                      <TableHead>{t('Total latency')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {stats.recent_tests.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={5}
                          className='text-muted-foreground py-8 text-center'
                        >
                          {t('No test records')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      stats.recent_tests.map((test) => (
                        <TableRow key={`${test.created_at}-${test.model}`}>
                          <TableCell>{formatTime(test.created_at)}</TableCell>
                          <TableCell>{test.model || '-'}</TableCell>
                          <TableCell>
                            <Badge
                              variant={test.success ? 'success' : 'destructive'}
                            >
                              {test.success ? t('Success') : test.result}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {test.first_output_latency_ms} ms
                          </TableCell>
                          <TableCell>{test.latency_ms} ms</TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>
          </div>
        ) : null}
        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

const defaultPlanDraft: UpstreamAccountScheduledTestPlanPayload = {
  name: '',
  model: '',
  interval_minutes: 60,
  enabled: true,
  auto_recover: true,
}

export function AccountScheduledTestsDialog({
  open,
  account,
  onOpenChange,
}: {
  open: boolean
  account: UpstreamAccount | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [plans, setPlans] = useState<UpstreamAccountScheduledTestPlan[]>([])
  const [draft, setDraft] = useState(defaultPlanDraft)
  const [editingId, setEditingId] = useState<number | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const [results, setResults] = useState<
    UpstreamAccountScheduledTestResult[] | null
  >(null)
  const [resultsPlanId, setResultsPlanId] = useState<number | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const loadPlans = async () => {
    if (!account) return
    setLoading(true)
    try {
      const response = await listUpstreamAccountScheduledTests(account.id)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to load scheduled tests'))
      }
      setPlans(response.data)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load scheduled tests')
      )
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (!open || !account) return
    setDraft(defaultPlanDraft)
    setEditingId(null)
    setResults(null)
    setResultsPlanId(null)
    void loadPlans()
    // The account id identifies the dialog data source.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [account?.id, open])

  const save = async () => {
    if (!account || !draft.name.trim()) {
      toast.error(t('Plan name is required'))
      return
    }
    setSaving(true)
    try {
      const payload = {
        ...draft,
        name: draft.name.trim(),
        model: draft.model.trim(),
      }
      const response = editingId
        ? await updateUpstreamAccountScheduledTest(
            account.id,
            editingId,
            payload
          )
        : await createUpstreamAccountScheduledTest(account.id, payload)
      if (!response.success) {
        throw new Error(response.message || t('Save failed'))
      }
      toast.success(
        editingId
          ? t('Scheduled test plan updated')
          : t('Scheduled test plan created')
      )
      setDraft(defaultPlanDraft)
      setEditingId(null)
      await loadPlans()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  const edit = (plan: UpstreamAccountScheduledTestPlan) => {
    setEditingId(plan.id)
    setDraft({
      name: plan.name,
      model: plan.model,
      interval_minutes: plan.interval_minutes,
      enabled: plan.enabled,
      auto_recover: plan.auto_recover,
    })
  }

  const toggle = async (
    plan: UpstreamAccountScheduledTestPlan,
    enabled: boolean
  ) => {
    if (!account) return
    try {
      const response = await updateUpstreamAccountScheduledTest(
        account.id,
        plan.id,
        { enabled }
      )
      if (!response.success)
        throw new Error(response.message || t('Update failed'))
      await loadPlans()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Update failed'))
    }
  }

  const remove = async () => {
    if (!account || !deleteId) return
    try {
      const response = await deleteUpstreamAccountScheduledTest(
        account.id,
        deleteId
      )
      if (!response.success)
        throw new Error(response.message || t('Delete failed'))
      setDeleteId(null)
      toast.success(t('Scheduled test plan deleted'))
      await loadPlans()
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Delete failed'))
    }
  }

  const showResults = async (planId: number) => {
    if (!account) return
    setResultsPlanId(planId)
    setResults(null)
    try {
      const response = await listUpstreamAccountScheduledTestResults(
        account.id,
        planId
      )
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to load test results'))
      }
      setResults(response.data)
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : t('Failed to load test results')
      )
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-4xl'>
          <DialogHeader>
            <DialogTitle>{t('Scheduled tests')}</DialogTitle>
            <DialogDescription>
              {t('Manage recurring connection tests for {{name}}.', {
                name: account?.name || '-',
              })}
            </DialogDescription>
          </DialogHeader>
          <FieldGroup>
            <div className='grid gap-3 sm:grid-cols-3'>
              <Field>
                <FieldLabel htmlFor='scheduled-test-name'>
                  {t('Plan name')}
                </FieldLabel>
                <Input
                  id='scheduled-test-name'
                  value={draft.name}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      name: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor='scheduled-test-model'>
                  {t('Model (optional)')}
                </FieldLabel>
                <Input
                  id='scheduled-test-model'
                  value={draft.model}
                  placeholder={t('Default model')}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      model: event.target.value,
                    }))
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor='scheduled-test-interval'>
                  {t('Interval (minutes)')}
                </FieldLabel>
                <Input
                  id='scheduled-test-interval'
                  type='number'
                  min={5}
                  max={43200}
                  value={draft.interval_minutes}
                  onChange={(event) =>
                    setDraft((current) => ({
                      ...current,
                      interval_minutes: Number(event.target.value) || 5,
                    }))
                  }
                />
              </Field>
            </div>
            <div className='flex flex-wrap items-center gap-5'>
              <label className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={draft.enabled}
                  onCheckedChange={(enabled) =>
                    setDraft((current) => ({ ...current, enabled }))
                  }
                />
                {t('Enabled')}
              </label>
              <label className='flex items-center gap-2 text-sm'>
                <Switch
                  checked={draft.auto_recover}
                  onCheckedChange={(autoRecover) =>
                    setDraft((current) => ({
                      ...current,
                      auto_recover: autoRecover,
                    }))
                  }
                />
                {t('Auto recover after a successful test')}
              </label>
              <Button onClick={() => void save()} disabled={saving}>
                <HugeiconsIcon
                  icon={
                    saving ? Loading03Icon : editingId ? Edit02Icon : Add01Icon
                  }
                  className={saving ? 'animate-spin' : undefined}
                  strokeWidth={2}
                  data-icon='inline-start'
                />
                {editingId ? t('Update plan') : t('Add plan')}
              </Button>
              {editingId && (
                <Button
                  variant='outline'
                  onClick={() => {
                    setEditingId(null)
                    setDraft(defaultPlanDraft)
                  }}
                >
                  {t('Cancel')}
                </Button>
              )}
            </div>
          </FieldGroup>
          <div className='overflow-auto rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Test plan')}</TableHead>
                  <TableHead>{t('Test interval')}</TableHead>
                  <TableHead>{t('Next run')}</TableHead>
                  <TableHead>{t('Last run')}</TableHead>
                  <TableHead>{t('Enabled')}</TableHead>
                  <TableHead>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={6}>
                      <Skeleton className='h-10' />
                    </TableCell>
                  </TableRow>
                ) : plans.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground py-8 text-center'
                    >
                      {t('No scheduled tests')}
                    </TableCell>
                  </TableRow>
                ) : (
                  plans.map((plan) => (
                    <TableRow key={plan.id}>
                      <TableCell>
                        <div className='font-medium'>{plan.name}</div>
                        <div className='text-muted-foreground text-xs'>
                          {plan.model || t('Default model')}
                        </div>
                      </TableCell>
                      <TableCell>
                        {t('{{minutes}} minutes', {
                          minutes: plan.interval_minutes,
                        })}
                      </TableCell>
                      <TableCell>{formatTime(plan.next_run_at)}</TableCell>
                      <TableCell>{formatTime(plan.last_run_at)}</TableCell>
                      <TableCell>
                        <Switch
                          checked={plan.enabled}
                          onCheckedChange={(enabled) =>
                            void toggle(plan, enabled)
                          }
                        />
                      </TableCell>
                      <TableCell>
                        <div className='flex items-center gap-1'>
                          <Button
                            variant='ghost'
                            size='sm'
                            onClick={() => void showResults(plan.id)}
                          >
                            {t('Results')}
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label={t('Edit')}
                            title={t('Edit')}
                            onClick={() => edit(plan)}
                          >
                            <HugeiconsIcon icon={Edit02Icon} strokeWidth={2} />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon-sm'
                            aria-label={t('Delete')}
                            title={t('Delete')}
                            className='hover:bg-destructive/10 hover:text-destructive'
                            onClick={() => setDeleteId(plan.id)}
                          >
                            <HugeiconsIcon
                              icon={Delete02Icon}
                              strokeWidth={2}
                            />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          {resultsPlanId && (
            <div className='flex flex-col gap-2'>
              <div className='text-sm font-medium'>{t('Test results')}</div>
              <div className='max-h-52 overflow-auto rounded-lg border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Time')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead>{t('Model')}</TableHead>
                      <TableHead>{t('First output')}</TableHead>
                      <TableHead>{t('Total latency')}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {results === null ? (
                      <TableRow>
                        <TableCell colSpan={5}>
                          <Skeleton className='h-8' />
                        </TableCell>
                      </TableRow>
                    ) : results.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={5}
                          className='text-muted-foreground py-6 text-center'
                        >
                          {t('No scheduled test results')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      results.map((result) => (
                        <TableRow key={result.id}>
                          <TableCell>{formatTime(result.created_at)}</TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                result.success ? 'success' : 'destructive'
                              }
                            >
                              {result.success ? t('Success') : result.result}
                            </Badge>
                          </TableCell>
                          <TableCell>{result.model || '-'}</TableCell>
                          <TableCell>
                            {result.first_output_latency_ms} ms
                          </TableCell>
                          <TableCell>{result.latency_ms} ms</TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant='outline' onClick={() => onOpenChange(false)}>
              {t('Close')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog
        open={deleteId !== null}
        onOpenChange={(open) => !open && setDeleteId(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t('Delete scheduled test plan?')}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {t('The plan and its test results will be permanently deleted.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              variant='destructive'
              onClick={() => void remove()}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
