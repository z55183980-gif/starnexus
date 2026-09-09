import i18next from 'i18next'
import { toast } from 'sonner'
import { api } from './api'

type DashboardResponse = {
  success: boolean
  message?: string
  code?: string
}

export class DashboardRequestError extends Error {}

// Business failures must reject so React Query retains successful data.
// Retry only temporary capacity failures, and show one message after retries.
export async function getDashboardResponse<T extends DashboardResponse>(
  url: string,
  params?: object
): Promise<T> {
  for (let attempt = 0; ; attempt++) {
    const response = await api.get<T>(url, {
      params,
      ...{ skipBusinessError: true },
    })
    if (response.data.success) return response.data
    const busy =
      response.data.code === 'dashboard_busy' ||
      response.data.message?.includes('dashboard aggregate is busy') ||
      response.data.message === 'log list is busy'
    if (busy && attempt < 2) {
      await new Promise((resolve) =>
        setTimeout(resolve, 500 * 2 ** attempt + Math.random() * 300)
      )
      continue
    }
    const message = busy
      ? i18next.t('Statistics are busy. Please retry shortly.')
      : response.data.message || i18next.t('Request failed')
    toast.error(message)
    throw new DashboardRequestError(message)
  }
}
