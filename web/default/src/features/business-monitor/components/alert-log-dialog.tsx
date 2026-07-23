/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { formatTimeStr } from '@/lib/format'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Spinner } from '@/components/ui/spinner'
import type { UsageLog } from '@/features/usage-logs/data/schema'
import { parseLogOther } from '@/features/usage-logs/lib/format'
import { getBusinessMonitorAlertLog, type ErrorAlert } from '../api'

interface AlertLogDialogProps {
  alert: ErrorAlert
  children: ReactNode
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return (
    <div className='grid min-w-0 grid-cols-[7rem_minmax(0,1fr)] gap-3 text-sm'>
      <span className='text-muted-foreground text-xs'>{label}</span>
      <span className='min-w-0 break-all'>{value || '-'}</span>
    </div>
  )
}

function getNodeName(log: UsageLog) {
  const other = parseLogOther(log.other)
  return other?.admin_info?.node_name || ''
}

export function AlertLogDialog({ alert, children }: AlertLogDialogProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const alertID = alert.id
  const logID = alert.last_log_id
  const logQuery = useQuery({
    queryKey: ['business-monitor-alert-log', alertID, logID],
    queryFn: async () => {
      const response = await getBusinessMonitorAlertLog(alertID)
      if (!response.success || !response.data) {
        throw new Error(response.message)
      }
      return response.data
    },
    enabled: open,
    retry: false,
  })

  const log = logQuery.data
  const logOther = log ? parseLogOther(log.other) : undefined

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger
        render={
          <button
            type='button'
            className='focus-visible:ring-ring/50 hover:bg-muted/50 flex min-w-0 flex-1 cursor-pointer items-start gap-2 rounded-md px-1 py-1 text-left outline-none focus-visible:ring-[3px]'
            title={`${t('Click to view full details')}: ${t('Log Details')} #${logID}`}
            aria-label={`${t('Click to view full details')}: ${t('Log Details')} #${logID}`}
          />
        }
      >
        {children}
      </DialogTrigger>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>
            {t('Log Details')} #{logID}
          </DialogTitle>
          <DialogDescription>
            {t('View the complete details for this log entry')}
          </DialogDescription>
        </DialogHeader>

        {logQuery.isPending && (
          <div className='flex min-h-28 items-center justify-center gap-2'>
            <Spinner />
            <span className='text-muted-foreground text-sm'>
              {t('Loading')}
            </span>
          </div>
        )}

        {!logQuery.isPending && logQuery.error && (
          <Alert variant='destructive'>
            <AlertTitle>{t('Failed to load logs')}</AlertTitle>
            <AlertDescription>
              {logQuery.error instanceof Error
                ? logQuery.error.message || t('Failed to load logs')
                : t('Failed to load logs')}
            </AlertDescription>
            <Button
              type='button'
              variant='outline'
              size='sm'
              className='mt-2'
              onClick={() => void logQuery.refetch()}
            >
              {t('Retry')}
            </Button>
          </Alert>
        )}

        {log && (
          <ScrollArea className='max-h-[70vh] pr-3'>
            <div className='flex flex-col gap-4'>
              <div className='flex flex-col gap-1.5'>
                <span className='text-muted-foreground text-xs font-medium'>
                  {t('Error Message')}
                </span>
                <div className='rounded-md border border-red-200 bg-red-50/60 p-3 text-sm break-all whitespace-pre-wrap text-red-700 dark:border-red-900/60 dark:bg-red-950/20 dark:text-red-300'>
                  {log.content || '-'}
                </div>
              </div>

              <div className='flex flex-col gap-2'>
                <InfoRow label={t('Username')} value={log.username} />
                <InfoRow
                  label={t('Created At')}
                  value={formatTimeStr(new Date(log.created_at * 1000))}
                />
                <InfoRow
                  label={t('Channel')}
                  value={log.channel_name || `#${log.channel}`}
                />
                <InfoRow label={t('Model')} value={log.model_name} />
                <InfoRow label={t('Node Name')} value={getNodeName(log)} />
                <InfoRow label={t('Request ID')} value={log.request_id} />
                <InfoRow
                  label={t('Upstream Request ID')}
                  value={log.upstream_request_id}
                />
                <InfoRow
                  label={t('Token')}
                  value={log.token_name || logOther?.group || ''}
                />
              </div>
            </div>
          </ScrollArea>
        )}
      </DialogContent>
    </Dialog>
  )
}
