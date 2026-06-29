import { api } from '@/lib/api'

export type AffiliateInvitee = {
  user_id: number
  username: string
  email: string
  created_at: string
  total_rebate_usd: string
}

export type AffiliateDetail = {
  aff_code: string
  inviter_id?: number | null
  aff_count: number
  aff_quota_usd: string
  aff_frozen_quota_usd: string
  aff_history_quota_usd: string
  effective_rebate_rate_percent: number
  affiliate_enabled: boolean
  invitees: AffiliateInvitee[]
  invitees_total?: number
  invitees_page?: number
  invitees_page_size?: number
}

export type AffiliateDetailParams = {
  page?: number
  pageSize?: number
}

export async function getAffiliateDetail(params?: AffiliateDetailParams) {
  return api.get<{ success: boolean; data: AffiliateDetail }>('/api/user/affiliate', {
    params: {
      p: params?.page,
      page_size: params?.pageSize,
    },
  })
}

export async function transferAffiliateUSD() {
  return api.post<{
    success: boolean
    data: { quota_added: number; transferred_usd: string }
  }>('/api/user/affiliate/transfer')
}

export function generateAffiliateLink(affCode: string): string {
  if (typeof window === 'undefined') return ''
  return `${window.location.origin}/sign-up?aff=${affCode}`
}
