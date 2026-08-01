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
import { USER_ROLE, USER_STATUS } from '../constants'
import { type User } from '../types'
import { getBulkUserStatusTargets } from './bulk-user-status-actions'

function makeUser(
  id: number,
  status: number,
  role: number = USER_ROLE.USER
): User {
  return {
    id,
    username: `user-${id}`,
    display_name: '',
    quota: 0,
    used_quota: 0,
    request_count: 0,
    group: 'default',
    status,
    role,
  }
}

describe('getBulkUserStatusTargets', () => {
  test('enables only currently disabled users', () => {
    const enabled = makeUser(1, USER_STATUS.ENABLED)
    const disabled = makeUser(2, USER_STATUS.DISABLED)

    assert.deepEqual(getBulkUserStatusTargets([enabled, disabled]).enable, [
      disabled,
    ])
  })

  test('disables only currently enabled non-Root users', () => {
    const enabled = makeUser(1, USER_STATUS.ENABLED)
    const disabled = makeUser(2, USER_STATUS.DISABLED)
    const root = makeUser(3, USER_STATUS.ENABLED, USER_ROLE.ROOT)

    assert.deepEqual(
      getBulkUserStatusTargets([enabled, disabled, root]).disable,
      [enabled]
    )
  })

  test('ignores unsupported status values for both actions', () => {
    const unknown = makeUser(1, 3)

    assert.deepEqual(getBulkUserStatusTargets([unknown]), {
      enable: [],
      disable: [],
    })
  })

  test('allows a disabled Root user to be re-enabled', () => {
    const root = makeUser(1, USER_STATUS.DISABLED, USER_ROLE.ROOT)

    assert.deepEqual(getBulkUserStatusTargets([root]), {
      enable: [root],
      disable: [],
    })
  })
})
