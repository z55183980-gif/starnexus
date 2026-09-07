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
import { useTranslation } from 'react-i18next'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { TIME_RANGE_PRESETS } from '@/features/dashboard/constants'

interface DashboardTimeRangeToggleProps {
  value: number
  onValueChange: (days: number) => void
}

export function DashboardTimeRangeToggle({
  value,
  onValueChange,
}: DashboardTimeRangeToggleProps) {
  const { t } = useTranslation()

  return (
    <ToggleGroup
      value={[String(value)]}
      onValueChange={(values) => {
        const days = Number(values.at(-1))
        if (days) onValueChange(days)
      }}
      variant='segmented'
      size='sm'
      spacing={1}
      aria-label={t('Quick Range')}
    >
      {TIME_RANGE_PRESETS.map((preset) => (
        <ToggleGroupItem key={preset.days} value={String(preset.days)}>
          {t(preset.label)}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
