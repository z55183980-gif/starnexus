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
import i18next from 'i18next'

/** i18n key for the UI brand name (zh: 星域互联, other languages: StarNexus) */
export const DEFAULT_SYSTEM_NAME_I18N_KEY = 'StarNexus'

export const HEADER_BRAND_SUFFIX = '&DKBY'

export function getLocalizedDefaultSystemName(): string {
  return i18next.t(DEFAULT_SYSTEM_NAME_I18N_KEY)
}

/** Brand name shown in the top menu bar (e.g. 星域互联&DKBY). */
export function formatHeaderBrandName(name: string): string {
  return `${name}${HEADER_BRAND_SUFFIX}`
}

/** Applies the localized, search-friendly default title (ignores admin SystemName). */
export function applySystemDocumentTitle(): void {
  if (typeof document === 'undefined') return
  const title = i18next.t('StarNexus | Unified AI API Gateway')
  document.title = title
  const metaTitle = document.querySelector(
    'meta[name="title"]'
  ) as HTMLMetaElement | null
  if (metaTitle) metaTitle.setAttribute('content', title)
}
