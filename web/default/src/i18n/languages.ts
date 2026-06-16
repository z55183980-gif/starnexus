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

export const INTERFACE_LANGUAGE_OPTIONS = [
  { code: 'zh', label: '简体中文' },
  { code: 'en', label: 'English' },
  { code: 'fr', label: 'Français' },
  { code: 'ru', label: 'Русский' },
  { code: 'ja', label: '日本語' },
  { code: 'vi', label: 'Tiếng Việt' },
] as const

export type InterfaceLanguageCode =
  (typeof INTERFACE_LANGUAGE_OPTIONS)[number]['code']

/** Compact label on the header language trigger (fixed width). */
export const INTERFACE_LANGUAGE_SHORT_LABELS: Record<
  InterfaceLanguageCode,
  string
> = {
  zh: 'ZH',
  en: 'EN',
  fr: 'FR',
  ru: 'RU',
  ja: 'JA',
  vi: 'VI',
}

export function normalizeInterfaceLanguage(value?: string | null): string {
  if (!value) return 'en'

  const normalized = value.trim().replace(/_/g, '-').toLowerCase()
  if (normalized.startsWith('zh')) return 'zh'

  return INTERFACE_LANGUAGE_OPTIONS.some((lang) => lang.code === normalized)
    ? normalized
    : 'en'
}

export function getInterfaceLanguageShortLabel(
  code?: string | null
): string {
  const normalized = normalizeInterfaceLanguage(code)
  return (
    INTERFACE_LANGUAGE_SHORT_LABELS[
      normalized as InterfaceLanguageCode
    ] ?? INTERFACE_LANGUAGE_SHORT_LABELS.en
  )
}
