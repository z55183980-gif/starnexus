export type ImportCollectionKey = 'accounts' | 'proxies'

export type UpstreamDataImportPayload = {
  type: 'sub2api-data'
  version: 1
  exported_at: string
  proxies: unknown[]
  accounts: unknown[]
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value)
}

function hasCredentialValue(value: unknown) {
  return value !== null && value !== undefined && String(value).trim() !== ''
}

export function normalizeAccountImportItem(item: unknown) {
  if (!isRecord(item)) return item

  const normalized = { ...item }
  if (
    normalized.extra !== null &&
    normalized.extra !== undefined &&
    typeof normalized.extra !== 'string'
  ) {
    normalized.extra = JSON.stringify(normalized.extra)
  }

  if (isRecord(normalized.credentials)) {
    const credentials = { ...normalized.credentials }
    const platform = String(normalized.platform ?? '').trim().toLowerCase()
    const accountType = String(normalized.type ?? '').trim().toLowerCase()
    if (
      platform === 'openai' &&
      accountType === 'oauth' &&
      !hasCredentialValue(credentials.account_id) &&
      hasCredentialValue(credentials.chatgpt_account_id)
    ) {
      credentials.account_id = credentials.chatgpt_account_id
    }
    normalized.credentials = credentials
  }

  return normalized
}

export function extractImportItems(
  parsed: unknown,
  collectionKey: ImportCollectionKey
) {
  if (Array.isArray(parsed)) {
    return collectionKey === 'accounts'
      ? parsed.map(normalizeAccountImportItem)
      : parsed
  }
  if (!parsed || typeof parsed !== 'object') return null

  const collection = (parsed as Record<string, unknown>)[collectionKey]
  if (!Array.isArray(collection)) return null

  return collectionKey === 'accounts'
    ? collection.map(normalizeAccountImportItem)
    : collection
}

export function mergeAccountImportDocuments(
  documents: unknown[]
): UpstreamDataImportPayload {
  const accounts: unknown[] = []
  const proxies: unknown[] = []

  for (const document of documents) {
    if (Array.isArray(document)) {
      accounts.push(...document.map(normalizeAccountImportItem))
      continue
    }
    if (!isRecord(document)) continue
    if (Array.isArray(document.accounts)) {
      accounts.push(...document.accounts.map(normalizeAccountImportItem))
    }
    if (Array.isArray(document.proxies)) {
      proxies.push(...document.proxies)
    }
  }

  return {
    type: 'sub2api-data',
    version: 1,
    exported_at: new Date().toISOString(),
    proxies,
    accounts,
  }
}
