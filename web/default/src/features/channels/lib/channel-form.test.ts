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
  CHANNEL_FORM_DEFAULT_VALUES,
  getChannelCreateDefaultValues,
  requiresChannelKeyForCreate,
  transformFormDataToCreatePayload,
  transformFormDataToUpdatePayload,
} from './channel-form'

function buildSettings(
  type: number,
  enabled: boolean,
  settings = '{}',
  replayEnabled = false
) {
  const payload = transformFormDataToUpdatePayload(
    {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'test channel',
      type,
      models: 'gpt-5.6-sol',
      group: ['test3'],
      settings,
      responses_websocket_v2_enabled: enabled,
      responses_websocket_v2_replay_enabled: replayEnabled,
    },
    44
  )

  return JSON.parse(payload.settings || '{}') as Record<string, unknown>
}

describe('Responses WebSocket v2 channel settings', () => {
  test('persists the enabled flag for Sub2API channels', () => {
    const settings = buildSettings(59, true)

    assert.equal(settings.responses_websocket_v2_enabled, true)
  })

  test('persists the disabled flag for Sub2API channels', () => {
    const settings = buildSettings(59, false)

    assert.equal(settings.responses_websocket_v2_enabled, false)
  })

  test('persists replay only when Responses WebSocket v2 is enabled', () => {
    const enabled = buildSettings(59, true, '{}', true)
    const disabled = buildSettings(59, false, '{}', true)

    assert.equal(enabled.responses_websocket_v2_replay_enabled, true)
    assert.equal(disabled.responses_websocket_v2_replay_enabled, false)
  })

  test('removes the flag from unsupported channel types', () => {
    const settings = buildSettings(
      14,
      true,
      JSON.stringify({ responses_websocket_v2_enabled: true })
    )

    assert.equal('responses_websocket_v2_enabled' in settings, false)
    assert.equal('responses_websocket_v2_replay_enabled' in settings, false)
  })

  test('forces upstream websocket off for local account pools', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'local pool channel',
        type: 1,
        models: 'gpt-5.6-sol',
        group: ['test3'],
        credential_source: 'local_account_pool',
        upstream_account_pool_id: 9,
        responses_websocket_v2_enabled: true,
        responses_websocket_v2_replay_enabled: true,
      },
      44
    )
    const settings = JSON.parse(payload.settings || '{}') as Record<
      string,
      unknown
    >

    assert.equal(settings.responses_websocket_v2_enabled, false)
    assert.equal(settings.responses_websocket_v2_replay_enabled, false)
    assert.equal(payload.credential_source, 'local_account_pool')
    assert.equal(payload.upstream_account_pool_id, 9)
  })
})

describe('Channel creation modes', () => {
  test('uses channel credentials for upstream channels', () => {
    const defaults = getChannelCreateDefaultValues('upstream')

    assert.equal(defaults.credential_source, 'channel_key')
    assert.equal(defaults.upstream_account_pool_id, null)
    assert.equal(requiresChannelKeyForCreate(defaults), true)
  })

  test('uses a local account pool without requiring an API key', () => {
    const defaults = getChannelCreateDefaultValues('local')
    const payload = transformFormDataToCreatePayload({
      ...defaults,
      name: 'local openai pool',
      upstream_account_pool_id: 9,
      models: 'gpt-5.6-sol',
      group: ['test3'],
    })

    assert.equal(defaults.type, 1)
    assert.equal(defaults.credential_source, 'local_account_pool')
    assert.equal(requiresChannelKeyForCreate(defaults), false)
    assert.equal(payload.mode, 'single')
    assert.equal(payload.channel.key, null)
    assert.equal(payload.channel.credential_source, 'local_account_pool')
    assert.equal(payload.channel.upstream_account_pool_id, 9)
  })

  test('keeps account-owned protocol settings out of local channels', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'local openai pool',
      type: 1,
      credential_source: 'local_account_pool',
      upstream_account_pool_id: 9,
      models: 'public-model',
      group: ['test3'],
      key: 'legacy-key',
      base_url: 'https://legacy.example.com',
      openai_organization: 'legacy-org',
      model_mapping: '{"public-model":"upstream-model"}',
      header_override: '{"x-legacy":"value"}',
      proxy: 'socks5://127.0.0.1:1080',
      pass_through_body_enabled: true,
      alpha_search_enabled: true,
      settings: JSON.stringify({
        alpha_search_enabled: true,
        upstream_model_update_check_enabled: true,
        upstream_model_update_auto_sync_enabled: true,
      }),
      system_prompt: 'keep-channel-policy',
      allow_service_tier: true,
    })
    const setting = JSON.parse(payload.channel.setting || '{}') as Record<
      string,
      unknown
    >
    const settings = JSON.parse(payload.channel.settings || '{}') as Record<
      string,
      unknown
    >

    assert.equal(payload.channel.key, null)
    assert.equal(payload.channel.base_url, null)
    assert.equal(payload.channel.openai_organization, null)
    assert.equal(payload.channel.model_mapping, null)
    assert.equal(payload.channel.header_override, null)
    assert.equal(setting.proxy, '')
    assert.equal(setting.pass_through_body_enabled, false)
    assert.equal(setting.system_prompt, 'keep-channel-policy')
    assert.equal(settings.allow_service_tier, true)
    assert.equal('alpha_search_enabled' in settings, false)
    assert.equal('upstream_model_update_check_enabled' in settings, false)
    assert.equal('upstream_model_update_auto_sync_enabled' in settings, false)
  })

  test('prefills a published local channel from its account pool', () => {
    const defaults = getChannelCreateDefaultValues('local', {
      id: 10,
      name: 'Claude primary pool',
      platform: 'anthropic',
    })

    assert.equal(defaults.name, 'Claude primary pool')
    assert.equal(defaults.type, 14)
    assert.equal(defaults.credential_source, 'local_account_pool')
    assert.equal(defaults.upstream_account_pool_id, 10)
  })

  test('normalizes an edited legacy local Codex channel to OpenAI', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'legacy local pool',
        type: 57,
        credential_source: 'local_account_pool',
        upstream_account_pool_id: 9,
        models: 'gpt-5.6-sol',
        group: ['test3'],
      },
      44
    )

    assert.equal(payload.type, 1)
  })

  test('keeps Anthropic type for local Anthropic account pools', () => {
    const payload = transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'local anthropic pool',
      type: 14,
      credential_source: 'local_account_pool',
      upstream_account_pool_id: 10,
      models: 'claude-sonnet-4-5',
      group: ['test3'],
    })

    assert.equal(payload.channel.type, 14)
    assert.equal(payload.channel.credential_source, 'local_account_pool')
  })
})

describe('Alpha Search channel capability', () => {
  test('persists the capability for OpenAI channels', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'test channel',
        type: 1,
        models: 'gpt-5.6-sol',
        group: ['test3'],
        alpha_search_enabled: true,
      },
      44
    )
    const settings = JSON.parse(payload.settings || '{}') as Record<
      string,
      unknown
    >

    assert.equal(settings.alpha_search_enabled, true)
  })

  test('removes the capability from non-OpenAI channels', () => {
    const payload = transformFormDataToUpdatePayload(
      {
        ...CHANNEL_FORM_DEFAULT_VALUES,
        name: 'test channel',
        type: 14,
        models: 'gpt-5.6-sol',
        group: ['test3'],
        settings: JSON.stringify({ alpha_search_enabled: true }),
        alpha_search_enabled: true,
      },
      44
    )
    const settings = JSON.parse(payload.settings || '{}') as Record<
      string,
      unknown
    >

    assert.equal('alpha_search_enabled' in settings, false)
  })
})
