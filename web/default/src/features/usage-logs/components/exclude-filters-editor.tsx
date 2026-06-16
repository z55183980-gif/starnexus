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
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { LogExcludeField, LogExcludeFilter } from '../types'

const DEFAULT_EXCLUDE_FIELD: LogExcludeField = 'model_name'

type ExcludeFiltersEditorProps = {
  filters: LogExcludeFilter[]
  onChange: (filters: LogExcludeFilter[]) => void
  isAdmin: boolean
  inputClassName?: string
  sensitiveType?: 'text' | 'password'
  onKeyDown?: (e: React.KeyboardEvent) => void
}

function createEmptyExcludeFilter(): LogExcludeFilter {
  return { field: DEFAULT_EXCLUDE_FIELD, value: '' }
}

export function ExcludeFiltersEditor({
  filters,
  onChange,
  isAdmin,
  inputClassName = 'w-full sm:w-[140px] lg:w-[160px]',
  sensitiveType = 'text',
  onKeyDown,
}: ExcludeFiltersEditorProps) {
  const { t } = useTranslation()

  const fieldOptions: Array<{ value: LogExcludeField; label: string }> = [
    { value: 'model_name', label: t('Model Name') },
    { value: 'token_name', label: t('Token Name') },
    { value: 'group', label: t('Group') },
    ...(isAdmin
      ? [{ value: 'username' as const, label: t('Username (exact match)') }]
      : []),
  ]

  const handleAdd = () => {
    onChange([...filters, createEmptyExcludeFilter()])
  }

  const handleRemove = (index: number) => {
    onChange(filters.filter((_, i) => i !== index))
  }

  const handleFieldChange = (index: number, field: LogExcludeField) => {
    onChange(
      filters.map((item, i) => (i === index ? { ...item, field } : item))
    )
  }

  const handleValueChange = (index: number, value: string) => {
    onChange(
      filters.map((item, i) => (i === index ? { ...item, value } : item))
    )
  }

  return (
    <>
      {filters.map((filter, index) => (
        <div
          key={`exclude-filter-${index}`}
          className='border-border/60 bg-muted/20 flex flex-wrap items-center gap-2 rounded-md border px-2 py-1.5'
        >
          <Select
            items={fieldOptions.map((option) => ({
              value: option.value,
              label: option.label,
            }))}
            value={filter.field}
            onValueChange={(value) => {
              if (value !== null) {
                handleFieldChange(index, value as LogExcludeField)
              }
            }}
          >
            <SelectTrigger className={inputClassName}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {fieldOptions.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <span className='text-muted-foreground shrink-0 text-xs sm:text-sm'>
            {t('Does not contain')}
          </span>
          <Input
            placeholder={
              filter.field === 'username'
                ? t('Username (exact match)')
                : t('Filter...')
            }
            type={
              filter.field === 'group' || filter.field === 'token_name'
                ? sensitiveType
                : 'text'
            }
            value={filter.value}
            onChange={(e) => handleValueChange(index, e.target.value)}
            onKeyDown={onKeyDown}
            className={inputClassName}
          />
          <Button
            type='button'
            variant='ghost'
            size='icon'
            onClick={() => handleRemove(index)}
            aria-label={t('Remove exclude condition')}
            className='text-muted-foreground hover:text-foreground size-8 shrink-0'
          >
            <Trash2 className='size-4' />
          </Button>
        </div>
      ))}
      <Button
        type='button'
        variant='outline'
        size='sm'
        onClick={handleAdd}
        className='h-9'
      >
        <Plus className='size-4' />
        {t('Add exclude condition')}
      </Button>
    </>
  )
}
