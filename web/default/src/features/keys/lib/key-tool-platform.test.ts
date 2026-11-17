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
import { test } from 'node:test'
import { getKeyToolPlatform } from './key-tool-platform'

test('recommends desktop installers without misidentifying mobile devices', () => {
  const browser = { userAgent: '', platform: '', maxTouchPoints: 0 }
  assert.equal(getKeyToolPlatform({ ...browser, platform: 'Win32' }), 'windows')
  assert.equal(
    getKeyToolPlatform({ ...browser, platform: 'MacIntel' }),
    'macos'
  )
  assert.equal(
    getKeyToolPlatform({ ...browser, platform: 'Linux x86_64' }),
    null
  )
  assert.equal(getKeyToolPlatform(browser), null)
  assert.equal(
    getKeyToolPlatform({ ...browser, platform: 'MacIntel', maxTouchPoints: 5 }),
    null
  )
  assert.equal(
    getKeyToolPlatform({
      ...browser,
      platform: 'MacIntel',
      userAgent: 'iPhone Mobile',
    }),
    null
  )
  assert.equal(
    getKeyToolPlatform({
      ...browser,
      userAgentData: { platform: 'Android', mobile: true },
    }),
    null
  )
  assert.equal(
    getKeyToolPlatform({ ...browser, userAgentData: { platform: 'Windows' } }),
    'windows'
  )
  assert.equal(
    getKeyToolPlatform({ ...browser, userAgentData: { platform: 'macOS' } }),
    'macos'
  )
})
