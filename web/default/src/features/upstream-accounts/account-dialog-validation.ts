/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/

export type ModelMappingRow = { from: string; to: string }

export type ModelMappingValidationError =
  | 'missingPair'
  | 'invalidSourceWildcard'
  | 'wildcardTarget'
  | 'duplicateSource'

export type ModelMappingBuildResult =
  | { mapping: Record<string, string> | null; error: null }
  | { mapping: null; error: ModelMappingValidationError }

export function buildValidatedModelMapping(
  rows: ModelMappingRow[],
  requireUniqueSources = false
): ModelMappingBuildResult {
  const mapping: Record<string, string> = {}
  const seen = new Set<string>()
  for (const row of rows) {
    const from = row.from.trim()
    const to = row.to.trim()
    if (!from && !to) continue
    if (!from || !to) return { mapping: null, error: 'missingPair' }
    const wildcardIndex = from.indexOf('*')
    if (
      wildcardIndex >= 0 &&
      (wildcardIndex !== from.length - 1 ||
        from.lastIndexOf('*') !== wildcardIndex)
    ) {
      return { mapping: null, error: 'invalidSourceWildcard' }
    }
    if (to.includes('*')) return { mapping: null, error: 'wildcardTarget' }
    if (requireUniqueSources && seen.has(from)) {
      return { mapping: null, error: 'duplicateSource' }
    }
    seen.add(from)
    mapping[from] = to
  }
  return {
    mapping: Object.keys(mapping).length > 0 ? mapping : null,
    error: null,
  }
}

export interface HeaderOverrideRow {
  name: string
  value: string
}

export type HeaderOverrideValidationError =
  | 'invalidName'
  | 'blockedName'
  | 'duplicateName'
  | 'invalidValue'
  | 'tooManyEntries'

const blockedHeaderOverrideNames = new Set([
  'host',
  'content-length',
  'content-type',
  'transfer-encoding',
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'proxy-connection',
  'te',
  'trailer',
  'upgrade',
  'authorization',
  'x-api-key',
  'x-goog-api-key',
  'cookie',
  'accept-encoding',
  'sec-websocket-key',
  'sec-websocket-version',
  'sec-websocket-extensions',
  'sec-websocket-protocol',
  'sec-websocket-accept',
  'session_id',
  'conversation_id',
  'x-codex-turn-state',
  'x-codex-turn-metadata',
  'chatgpt-account-id',
  'x-claude-code-session-id',
  'x-client-request-id',
])

const headerNamePattern = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/
// eslint-disable-next-line no-control-regex
const invalidHeaderValuePattern = /[\x00-\x08\x0a-\x1f\x7f]/
const textEncoder = new TextEncoder()

export function validateHeaderOverrideRows(
  rows: HeaderOverrideRow[]
): HeaderOverrideValidationError | null {
  const seen = new Set<string>()
  for (const row of rows) {
    const name = row.name.trim()
    const value = row.value.trim()
    if (!name) {
      if (value) return 'invalidName'
      continue
    }
    if (!headerNamePattern.test(name) || name.length > 200) {
      return 'invalidName'
    }
    const normalizedName = name.toLowerCase()
    if (blockedHeaderOverrideNames.has(normalizedName)) return 'blockedName'
    if (seen.has(normalizedName)) return 'duplicateName'
    if (
      invalidHeaderValuePattern.test(value) ||
      textEncoder.encode(value).length > 8192
    ) {
      return 'invalidValue'
    }
    seen.add(normalizedName)
  }
  return seen.size > 64 ? 'tooManyEntries' : null
}

export function buildHeaderOverridesObject(
  rows: HeaderOverrideRow[]
): Record<string, string> {
  const overrides: Record<string, string> = {}
  for (const row of rows) {
    const name = row.name.trim().toLowerCase()
    if (name) overrides[name] = row.value.trim()
  }
  return overrides
}

export function splitHeaderOverridesObject(
  record: unknown
): HeaderOverrideRow[] {
  if (!record || typeof record !== 'object' || Array.isArray(record)) return []
  return Object.entries(record as Record<string, unknown>)
    .filter(([, value]) => typeof value === 'string')
    .map(([name, value]) => ({ name, value: value as string }))
    .sort((left, right) => left.name.localeCompare(right.name))
}

export function parseIntegerAtLeast(raw: string, minimum: number) {
  if (!raw.trim()) return null
  const value = Number(raw)
  return Number.isInteger(value) && value >= minimum ? value : null
}

export type EditableAccountStatus = 'active' | 'inactive' | 'error' | 'expired'

export function editableAccountStatuses(
  current?: EditableAccountStatus
): EditableAccountStatus[] {
  const statuses: EditableAccountStatus[] = ['active', 'inactive']
  if (current === 'error' || current === 'expired') statuses.push(current)
  return statuses
}

export type CredentialBackedSettings = {
  baseUrl: string
  bedrockAuthMode: string
  bedrockRegion: string
  vertexLocation: string
  interceptWarmupRequests: boolean
  tempUnschedEnabled: boolean
  tempUnschedRules: Array<{
    error_code: string
    keywords: string
    duration_seconds: string
    description: string
  }>
  compactModelMappings: ModelMappingRow[]
  openaiEndpointCapabilities: string[]
  allowedModels: string[]
  modelMappings: ModelMappingRow[]
  headerOverrideEnabled: boolean
  headerOverrideRows: HeaderOverrideRow[]
}

export function credentialBackedSettingsChanged(
  initial: CredentialBackedSettings,
  current: CredentialBackedSettings
) {
  return JSON.stringify(initial) !== JSON.stringify(current)
}
