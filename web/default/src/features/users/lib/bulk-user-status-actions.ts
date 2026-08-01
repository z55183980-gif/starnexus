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
import { USER_ROLE, USER_STATUS } from '../constants'
import { type User } from '../types'

export interface BulkUserStatusTargets {
  enable: User[]
  disable: User[]
}

/**
 * Mirrors the single-user status actions:
 * - only disabled users need enabling;
 * - only enabled, non-Root users may be disabled.
 */
export function getBulkUserStatusTargets(
  users: readonly User[]
): BulkUserStatusTargets {
  return {
    enable: users.filter((user) => user.status === USER_STATUS.DISABLED),
    disable: users.filter(
      (user) =>
        user.status === USER_STATUS.ENABLED && user.role !== USER_ROLE.ROOT
    ),
  }
}
