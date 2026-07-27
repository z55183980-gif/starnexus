import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToUpdatePayload,
} from './channel-form'

function buildSettings(type: number, enabled: boolean, settings = '{}') {
  const payload = transformFormDataToUpdatePayload(
    {
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'test channel',
      type,
      models: 'gpt-5.6-sol',
      group: ['test3'],
      settings,
      responses_websocket_v2_enabled: enabled,
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

  test('removes the flag from unsupported channel types', () => {
    const settings = buildSettings(
      14,
      true,
      JSON.stringify({ responses_websocket_v2_enabled: true })
    )

    assert.equal('responses_websocket_v2_enabled' in settings, false)
  })
})
