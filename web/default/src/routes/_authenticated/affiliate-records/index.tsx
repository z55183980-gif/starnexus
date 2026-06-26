import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { AdminAffiliateRecords } from '@/features/admin-affiliate'

export const Route = createFileRoute('/_authenticated/affiliate-records/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    if (!auth.user || auth.user.role < ROLE.AGENT) {
      throw redirect({
        to: '/403',
      })
    }
  },
  component: AdminAffiliateRecords,
})