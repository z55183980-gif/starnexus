/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

export type CCSwitchApp = 'claude' | 'codex' | 'gemini'

type BuildCCSwitchImportUrlInput = {
  app: CCSwitchApp
  name: string
  models: Record<string, string>
  apiKey: string
  apiBaseUrl: string
  homepage: string
}

export function normalizeCCSwitchBaseUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

export function ensureApiKeyPrefix(apiKey: string): string {
  const normalizedKey = apiKey.trim()
  return normalizedKey.startsWith('sk-') ? normalizedKey : `sk-${normalizedKey}`
}

function resolveEndpoint(app: CCSwitchApp, apiBaseUrl: string): string {
  const normalizedBaseUrl = normalizeCCSwitchBaseUrl(apiBaseUrl)
  if (app !== 'codex' || normalizedBaseUrl.endsWith('/v1')) {
    return normalizedBaseUrl
  }
  return `${normalizedBaseUrl}/v1`
}

export function buildCCSwitchImportUrl({
  app,
  name,
  models,
  apiKey,
  apiBaseUrl,
  homepage,
}: BuildCCSwitchImportUrlInput): string {
  const params = new URLSearchParams()
  params.set('resource', 'provider')
  params.set('app', app)
  params.set('name', name.trim())
  params.set('endpoint', resolveEndpoint(app, apiBaseUrl))
  params.set('apiKey', ensureApiKeyPrefix(apiKey))

  for (const [key, value] of Object.entries(models)) {
    const normalizedValue = value.trim()
    if (normalizedValue) params.set(key, normalizedValue)
  }

  params.set('homepage', normalizeCCSwitchBaseUrl(homepage))
  params.set('enabled', 'true')
  return `ccswitch://v1/import?${params.toString()}`
}
