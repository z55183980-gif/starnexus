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
type PlatformNavigator = {
  userAgent: string
  platform: string
  maxTouchPoints: number
  userAgentData?: { platform: string; mobile?: boolean }
}

export function getKeyToolPlatform(
  browser: PlatformNavigator
): 'windows' | 'macos' | null {
  // iPadOS can advertise a desktop Mac identity. Never recommend a desktop
  // installer to a touch-enabled iPad or another mobile browser.
  if (
    browser.userAgentData?.mobile ||
    /Android|iPhone|iPad|iPod|Mobile/i.test(browser.userAgent) ||
    (/Mac/i.test(browser.platform) && browser.maxTouchPoints > 1)
  ) {
    return null
  }
  const platform = browser.userAgentData?.platform || browser.platform
  if (/^Win/i.test(platform)) return 'windows'
  if (/^Mac/i.test(platform)) return 'macos'
  return null
}
