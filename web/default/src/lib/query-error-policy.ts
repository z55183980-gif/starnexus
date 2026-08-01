/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

const LOCALLY_HANDLED_HTTP_STATUSES_KEY = 'locallyHandledHttpStatuses'

export const localHttpErrorMeta = (statuses: readonly number[]) => ({
  [LOCALLY_HANDLED_HTTP_STATUSES_KEY]: statuses,
})

export function isHttpStatusHandledLocally(
  meta: unknown,
  status: number
): boolean {
  if (!meta || typeof meta !== 'object') return false

  const statuses = (meta as Record<string, unknown>)[
    LOCALLY_HANDLED_HTTP_STATUSES_KEY
  ]
  return Array.isArray(statuses) && statuses.includes(status)
}
