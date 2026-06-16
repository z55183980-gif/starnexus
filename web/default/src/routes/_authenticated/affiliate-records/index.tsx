import { createFileRoute } from '@tanstack/react-router'
import { AdminAffiliateRecords } from '@/features/admin-affiliate'

export const Route = createFileRoute('/_authenticated/affiliate-records/')({
  component: AdminAffiliateRecords,
})
