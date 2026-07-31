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
  buildHeaderOverridesObject,
  buildValidatedModelMapping,
  credentialBackedSettingsChanged,
  editableAccountStatuses,
  parseIntegerAtLeast,
  splitHeaderOverridesObject,
  validateHeaderOverrideRows,
  type CredentialBackedSettings,
} from './account-dialog-validation'

describe('buildValidatedModelMapping', () => {
  test('accepts exact and suffix wildcard sources', () => {
    assert.deepEqual(
      buildValidatedModelMapping([
        { from: 'gpt-5.4', to: 'gpt-5.4' },
        { from: 'gpt-5.*', to: 'gpt-5.4-compact' },
      ]),
      {
        mapping: {
          'gpt-5.4': 'gpt-5.4',
          'gpt-5.*': 'gpt-5.4-compact',
        },
        error: null,
      }
    )
  })

  test('rejects invalid source and target wildcards', () => {
    assert.equal(
      buildValidatedModelMapping([{ from: 'gpt-*legacy', to: 'gpt-5.4' }])
        .error,
      'invalidSourceWildcard'
    )
    assert.equal(
      buildValidatedModelMapping([{ from: 'gpt-5.*', to: 'gpt-*' }]).error,
      'wildcardTarget'
    )
  })

  test('rejects duplicate compact mapping sources', () => {
    assert.equal(
      buildValidatedModelMapping(
        [
          { from: 'gpt-5.*', to: 'gpt-5.4' },
          { from: 'gpt-5.*', to: 'gpt-5.4-mini' },
        ],
        true
      ).error,
      'duplicateSource'
    )
  })
})

describe('header overrides', () => {
  test('validates names, duplicates, values and limits', () => {
    assert.equal(
      validateHeaderOverrideRows([{ name: 'Authorization', value: 'x' }]),
      'blockedName'
    )
    assert.equal(
      validateHeaderOverrideRows([
        { name: 'X-App', value: 'a' },
        { name: 'x-app', value: 'b' },
      ]),
      'duplicateName'
    )
    assert.equal(
      validateHeaderOverrideRows([{ name: 'bad name', value: 'x' }]),
      'invalidName'
    )
    assert.equal(
      validateHeaderOverrideRows([{ name: 'x-app', value: 'bad\nvalue' }]),
      'invalidValue'
    )
  })

  test('round trips normalized rows', () => {
    const rows = [
      { name: 'User-Agent', value: 'starnexus' },
      { name: 'X-App', value: 'codex' },
    ]
    assert.deepEqual(
      splitHeaderOverridesObject(buildHeaderOverridesObject(rows)),
      [
        { name: 'user-agent', value: 'starnexus' },
        { name: 'x-app', value: 'codex' },
      ]
    )
  })
})

test('parseIntegerAtLeast rejects decimals and out-of-range values', () => {
  assert.equal(parseIntegerAtLeast('2', 1), 2)
  assert.equal(parseIntegerAtLeast('1.5', 1), null)
  assert.equal(parseIntegerAtLeast('-1', 0), null)
  assert.equal(parseIntegerAtLeast('not-a-number', 1), null)
})

test('editableAccountStatuses only preserves an existing runtime status', () => {
  assert.deepEqual(editableAccountStatuses('active'), ['active', 'inactive'])
  assert.deepEqual(editableAccountStatuses('error'), [
    'active',
    'inactive',
    'error',
  ])
  assert.deepEqual(editableAccountStatuses('expired'), [
    'active',
    'inactive',
    'expired',
  ])
})

test('credentialBackedSettingsChanged detects a hidden-setting edit', () => {
  const initial: CredentialBackedSettings = {
    baseUrl: '',
    bedrockAuthMode: 'sigv4',
    bedrockRegion: 'us-east-1',
    vertexLocation: 'global',
    interceptWarmupRequests: false,
    compactModelMappings: [],
    openaiEndpointCapabilities: ['chat_completions', 'embeddings'],
    allowedModels: [],
    modelMappings: [],
    headerOverrideEnabled: false,
    headerOverrideRows: [],
  }
  assert.equal(credentialBackedSettingsChanged(initial, initial), false)
  assert.equal(
    credentialBackedSettingsChanged(initial, {
      ...initial,
      interceptWarmupRequests: true,
    }),
    true
  )
})
