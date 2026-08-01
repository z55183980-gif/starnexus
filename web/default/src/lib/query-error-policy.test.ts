/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  isHttpStatusHandledLocally,
  localHttpErrorMeta,
} from './query-error-policy'

describe('query error policy', () => {
  test('handles only explicitly configured HTTP statuses locally', () => {
    const meta = localHttpErrorMeta([500])

    assert.equal(isHttpStatusHandledLocally(meta, 500), true)
    assert.equal(isHttpStatusHandledLocally(meta, 401), false)
  })

  test('uses global handling for missing or malformed metadata', () => {
    assert.equal(isHttpStatusHandledLocally(undefined, 500), false)
    assert.equal(
      isHttpStatusHandledLocally({ locallyHandledHttpStatuses: '500' }, 500),
      false
    )
  })
})
