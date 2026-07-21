import { api } from '@/lib/api'

export async function getBusinessMonitorConcurrency(): Promise<{
  success: boolean
  data?: { concurrency: number; available?: boolean }
}> {
  const res = await api.get('/api/business-monitor/concurrency')
  return res.data
}

export type ErrorAlertSeverity = 'critical' | 'warning' | 'info'
export type ErrorAlertStatus = 'unhandled' | 'acknowledged' | 'resolved'

export interface ErrorAlert {
  id: number
  fingerprint: string
  title: string
  severity: ErrorAlertSeverity
  status: ErrorAlertStatus
  channel_id: number
  channel_name: string
  node_name: string
  occurrence_count: number
  first_seen_at: number
  last_seen_at: number
  last_log_id: number
  acknowledged_by?: number
  acknowledged_at?: number
  resolved_at?: number
}

export async function getBusinessMonitorAlerts(): Promise<{
  success: boolean
  data?: ErrorAlert[]
}> {
  const res = await api.get('/api/business-monitor/alerts?limit=50')
  return res.data
}

export async function acknowledgeBusinessMonitorAlert(id: number) {
  const res = await api.post(`/api/business-monitor/alerts/${id}/acknowledge`)
  return res.data
}

export async function resolveBusinessMonitorAlert(id: number) {
  const res = await api.post(`/api/business-monitor/alerts/${id}/resolve`)
  return res.data
}
