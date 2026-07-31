/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { z } from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { AccountManagement } from '@/features/upstream-accounts'

const upstreamAccountsSearchSchema = z.object({
  tab: z.enum(['accounts', 'pools']).optional(),
  edit_pool: z.coerce.number().int().positive().optional(),
})

export const Route = createFileRoute('/_authenticated/upstream-accounts/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || auth.user.role !== ROLE.SUPER_ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  validateSearch: upstreamAccountsSearchSchema,
  component: UpstreamAccountsRoute,
})

function UpstreamAccountsRoute() {
  const { tab, edit_pool } = Route.useSearch()

  return <AccountManagement initialTab={tab} editPoolId={edit_pool} />
}
