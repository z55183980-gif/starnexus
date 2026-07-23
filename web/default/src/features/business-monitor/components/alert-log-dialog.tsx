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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Spinner } from '@/components/ui/spinner'
import { DetailsDialog } from '@/features/usage-logs/components/dialogs/details-dialog'
import { getBusinessMonitorAlertLog, type ErrorAlert } from '../api'

interface AlertLogDialogProps {
  alert: ErrorAlert | null
  onOpenChange: (open: boolean) => void
}

export function AlertLogDialog({ alert, onOpenChange }: AlertLogDialogProps) {
  const { t } = useTranslation()
  const alertID = alert?.id ?? 0
  const logID = alert?.last_log_id ?? 0
  const logQuery = useQuery({
    queryKey: ['business-monitor-alert-log', alertID, logID],
    queryFn: async () => {
      const response = await getBusinessMonitorAlertLog(alertID)
      if (!response.success || !response.data) {
        throw new Error(response.message)
      }
      return response.data
    },
    enabled: alert !== null,
    retry: false,
  })

  if (logQuery.data) {
    return (
      <DetailsDialog
        log={logQuery.data}
        isAdmin
        open={alert !== null}
        onOpenChange={onOpenChange}
      />
    )
  }

  return (
    <Dialog open={alert !== null} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-lg'>
        <DialogHeader>
          <DialogTitle>
            {t('Log Details')} #{logID}
          </DialogTitle>
          <DialogDescription>
            {t('View the complete details for this log entry')}
          </DialogDescription>
        </DialogHeader>

        {logQuery.isPending ? (
          <div className='flex min-h-28 items-center justify-center gap-2'>
            <Spinner />
            <span className='text-muted-foreground text-sm'>
              {t('Loading')}
            </span>
          </div>
        ) : (
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
      </DialogContent>
    </Dialog>
  )
}
