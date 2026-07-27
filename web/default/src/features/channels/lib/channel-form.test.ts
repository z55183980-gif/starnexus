import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
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
