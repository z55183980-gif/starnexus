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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildCCSwitchImportUrl,
  ensureApiKeyPrefix,
} from './cc-switch-import.ts'

function paramsFromUrl(url: string): URLSearchParams {
  return new URL(url).searchParams
}

describe('CC Switch import URL', () => {
  test('builds a Claude import with selected model mappings', () => {
    const params = paramsFromUrl(
      buildCCSwitchImportUrl({
        app: 'claude',
        name: ' 星域互联 ',
        models: {
          model: ' claude-sonnet-4-5 ',
          haikuModel: '',
          sonnetModel: 'claude-sonnet-4-5',
        },
        apiKey: 'token-value',
        apiBaseUrl: 'https://api.example.com/',
        homepage: 'https://example.com/',
      })
    )

    assert.equal(params.get('resource'), 'provider')
    assert.equal(params.get('app'), 'claude')
    assert.equal(params.get('name'), '星域互联')
    assert.equal(params.get('endpoint'), 'https://api.example.com')
    assert.equal(params.get('apiKey'), 'sk-token-value')
    assert.equal(params.get('model'), 'claude-sonnet-4-5')
    assert.equal(params.has('haikuModel'), false)
    assert.equal(params.get('sonnetModel'), 'claude-sonnet-4-5')
    assert.equal(params.get('homepage'), 'https://example.com')
    assert.equal(params.get('enabled'), 'true')
  })

  test('adds exactly one /v1 suffix to Codex endpoints', () => {
    for (const apiBaseUrl of [
      'https://api.example.com',
      'https://api.example.com/',
      'https://api.example.com/v1',
      'https://api.example.com/v1/',
    ]) {
      const params = paramsFromUrl(
        buildCCSwitchImportUrl({
          app: 'codex',
          name: 'Codex',
          models: { model: 'gpt-5.6-sol' },
          apiKey: 'sk-token-value',
          apiBaseUrl,
          homepage: 'https://example.com',
        })
      )

      assert.equal(params.get('endpoint'), 'https://api.example.com/v1')
    }
  })

  test('does not duplicate an existing API key prefix', () => {
    assert.equal(ensureApiKeyPrefix('sk-token-value'), 'sk-token-value')
    assert.equal(ensureApiKeyPrefix(' token-value '), 'sk-token-value')
  })
})
