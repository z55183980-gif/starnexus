import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  acknowledgeRoutingNodeAlert,
  getRoutingNodeAlerts,
  resolveRoutingNodeAlert,
  silenceRoutingNodeAlert,
} from '@/features/node-routing/api'
import type { RoutingNodeAlertState } from '@/features/node-routing/types'
import { useRoutingNodeAlertUnread } from '@/features/node-routing/use-routing-node-alert-unread'

type AlertAction = 'acknowledge' | 'silence' | 'resolve'

function alertRuleLabel(ruleKey: string, t: (key: string) => string) {
  const labels: Record<string, string> = {
    heartbeat_age: t('Monitor heartbeat overdue'),
    cpu_usage: t('CPU usage high'),
    load_percent: t('System load high'),
    memory_percent: t('Memory usage high'),
    disk_percent: t('Disk usage high'),
  }
  return labels[ruleKey] || ruleKey
}

function formatAlertValue(alert: RoutingNodeAlertState) {
  if (alert.rule_key === 'heartbeat_age') {
    return `${Math.round(alert.current_value)}s`
  }
  return `${alert.current_value.toFixed(1)}%`
}

function formatAlertTime(timestamp: number) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

export function NodeAlertsPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { unreadCount, markRead, isMarkingRead } =
    useRoutingNodeAlertUnread()
  const alertsQuery = useQuery({
    queryKey: ['routing-node-alerts', 'active'],
    queryFn: () => getRoutingNodeAlerts({ limit: 50 }),
    refetchInterval: 15_000,
  })
  const alerts = alertsQuery.data?.data || []
  const criticalCount = alerts.filter(
    (alert) => alert.severity === 'critical'
  ).length
  const warningCount = alerts.filter(
    (alert) => alert.severity === 'warning'
  ).length

  const actionMutation = useMutation({
    mutationFn: async ({ id, action }: { id: number; action: AlertAction }) => {
      const result =
        action === 'acknowledge'
          ? await acknowledgeRoutingNodeAlert(id)
          : action === 'silence'
            ? await silenceRoutingNodeAlert(id, 3600)
            : await resolveRoutingNodeAlert(id)
      if (!result.success) {
        throw new Error(result.message || t('Failed to update alert'))
      }
      return action
    },
    onSuccess: async (action) => {
      await Promise.all([
        queryClient.invalidateQueries({
          queryKey: ['routing-node-alerts', 'active'],
        }),
        queryClient.invalidateQueries({ queryKey: ['routing-nodes'] }),
      ])
      toast.success(
        action === 'acknowledge'
          ? t('Alert acknowledged')
          : action === 'silence'
            ? t('Alert silenced for one hour')
            : t('Alert resolved')
      )
    },
    onError: (error: Error) => toast.error(error.message),
  })

  const handleMarkRead = async () => {
    try {
      await markRead()
      toast.success(t('Node alerts marked as read'))
    } catch {
      toast.error(t('Failed to mark node alerts as read'))
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t('Node alerts')}</CardTitle>
        <CardDescription>
          {t(
            'Server-side rules continuously evaluate node heartbeats and resource pressure.'
          )}
        </CardDescription>
        {unreadCount > 0 && (
          <CardAction>
            <Button
              size='sm'
              variant='outline'
              disabled={isMarkingRead}
              onClick={handleMarkRead}
            >
              {isMarkingRead && <Spinner data-icon='inline-start' />}
              {t('Mark alerts as read')}
            </Button>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className='space-y-4'>
        {alertsQuery.isLoading ? (
          <Alert>
            <Spinner />
            <AlertTitle>{t('Loading...')}</AlertTitle>
          </Alert>
        ) : alertsQuery.isError ? (
          <Alert variant='destructive'>
            <AlertTitle>{t('Failed to load node alerts')}</AlertTitle>
            <AlertDescription>{t('Please try again later')}</AlertDescription>
          </Alert>
        ) : alerts.length === 0 ? (
          <Alert>
            <AlertTitle>{t('All monitored nodes are healthy')}</AlertTitle>
            <AlertDescription>
              {t('No active warning or critical alerts.')}
            </AlertDescription>
          </Alert>
        ) : (
          <Alert variant={criticalCount > 0 ? 'destructive' : 'default'}>
            <AlertTitle>{t('Active node alerts')}</AlertTitle>
            <AlertDescription>
              {t('Critical: {{critical}}, Warning: {{warning}}', {
                critical: criticalCount,
                warning: warningCount,
              })}
            </AlertDescription>
          </Alert>
        )}

        {alerts.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Severity')}</TableHead>
                <TableHead>{t('Node')}</TableHead>
                <TableHead>{t('Rule')}</TableHead>
                <TableHead>{t('Current value')}</TableHead>
                <TableHead>{t('Triggered at')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {alerts.map((alert) => {
                const pending =
                  actionMutation.isPending &&
                  actionMutation.variables?.id === alert.id
                return (
                  <TableRow key={alert.id}>
                    <TableCell>
                      <Badge
                        variant={
                          alert.severity === 'critical'
                            ? 'destructive'
                            : 'warning'
                        }
                      >
                        {alert.severity === 'critical'
                          ? t('Critical')
                          : t('Warning')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className='font-medium'>{alert.node_name}</div>
                      <div className='text-muted-foreground font-mono text-xs'>
                        {alert.node_key}
                      </div>
                    </TableCell>
                    <TableCell>{alertRuleLabel(alert.rule_key, t)}</TableCell>
                    <TableCell className='font-mono'>
                      {formatAlertValue(alert)}
                    </TableCell>
                    <TableCell>{formatAlertTime(alert.triggered_at)}</TableCell>
                    <TableCell>
                      <div className='flex justify-end gap-2'>
                        {pending ? (
                          <Spinner />
                        ) : (
                          <>
                            <Button
                              size='sm'
                              variant='outline'
                              disabled={alert.status === 'acknowledged'}
                              onClick={() =>
                                actionMutation.mutate({
                                  id: alert.id,
                                  action: 'acknowledge',
                                })
                              }
                            >
                              {t('Acknowledge')}
                            </Button>
                            <Button
                              size='sm'
                              variant='outline'
                              onClick={() =>
                                actionMutation.mutate({
                                  id: alert.id,
                                  action: 'silence',
                                })
                              }
                            >
                              {t('Silence 1 hour')}
                            </Button>
                            <Button
                              size='sm'
                              variant='secondary'
                              onClick={() =>
                                actionMutation.mutate({
                                  id: alert.id,
                                  action: 'resolve',
                                })
                              }
                            >
                              {t('Resolve')}
                            </Button>
                          </>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  )
}
