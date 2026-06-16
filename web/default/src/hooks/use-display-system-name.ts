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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  DEFAULT_SYSTEM_NAME_I18N_KEY,
  formatHeaderBrandName,
} from '@/lib/system-name'

/**
 * Localized brand name for all UI surfaces.
 * Always uses i18n (zh: 星域互联, other languages: StarNexus).
 * Admin SystemName is kept for backend/email only.
 */
export function useDisplaySystemName() {
  const { t, i18n } = useTranslation()

  return useMemo(
    () => t(DEFAULT_SYSTEM_NAME_I18N_KEY),
    [t, i18n.language]
  )
}

/** Localized brand name for the top menu bar only. */
export function useHeaderBrandName() {
  const name = useDisplaySystemName()

  return useMemo(() => formatHeaderBrandName(name), [name])
}
