/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
import { AccountDialog } from './account-dialog'
import type {
  UpstreamAccount,
  UpstreamAccountPayload,
  UpstreamAccountPool,
  UpstreamProxy,
} from './types'

/**
 * Batch editing intentionally delegates all fields and layout to the normal
 * account editor. The editor tracks changed fields and sends only that patch,
 * so the first selected account is used as the form's initial value without
 * copying its unchanged settings to other accounts.
 */
export function AccountBatchUpdateDialog({
  open,
  onOpenChange,
  account,
  selectedIds,
  pools,
  proxies,
  onApply,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  account: UpstreamAccount | null
  selectedIds: number[]
  pools: UpstreamAccountPool[]
  proxies: UpstreamProxy[]
  onApply: (patch: Partial<UpstreamAccountPayload>) => Promise<boolean>
  onSaved: () => void
}) {
  if (!account || selectedIds.length === 0) return null

  return (
    <AccountDialog
      key={`batch-${account.id}-${selectedIds.join(',')}`}
      open={open}
      onOpenChange={onOpenChange}
      account={account}
      pools={pools}
      proxies={proxies}
      onSaved={onSaved}
      batchAccountIds={selectedIds}
      onBatchSaved={onApply}
    />
  )
}
