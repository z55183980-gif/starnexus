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
import { useCallback, useMemo, useState } from 'react'
import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { StatusBadge } from '@/components/status-badge'

const sectionCardClassName =
  'relative shadow-sm ring-0 before:pointer-events-none before:absolute before:inset-0 before:rounded-xl before:border before:border-border/90'
const sectionHeaderClassName = 'border-b bg-muted/20'

type SpecialUsableMap = Record<string, Record<string, string>>

type GroupSpecialUsableRulesEditorProps = {
  value: string
  groupRatio: string
  userUsableGroups: string
  onChange: (value: string) => void
}

function safeParseRecord(str: string): Record<string, unknown> {
  if (!str || !str.trim()) return {}
  try {
    const parsed = JSON.parse(str)
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed)
      ? (parsed as Record<string, unknown>)
      : {}
  } catch {
    return {}
  }
}

function safeParseSpecialUsable(str: string): SpecialUsableMap {
  const parsed = safeParseRecord(str)
  const result: SpecialUsableMap = {}

  for (const [ownershipGroup, rules] of Object.entries(parsed)) {
    if (!rules || typeof rules !== 'object' || Array.isArray(rules)) continue
    const entries = Object.entries(rules as Record<string, unknown>)
      .filter(([rawKey]) => rawKey.trim())
      .map(([rawKey, desc]) => [rawKey, typeof desc === 'string' ? desc : ''])

    if (entries.length > 0) {
      result[ownershipGroup] = Object.fromEntries(entries)
    }
  }

  return result
}

function serializeSpecialUsable(map: SpecialUsableMap): string {
  const cleaned: SpecialUsableMap = {}

  for (const [ownershipGroup, rules] of Object.entries(map)) {
    const entries = Object.entries(rules).filter(
      ([rawKey]) => ownershipGroup && rawKey.trim()
    )
    if (entries.length > 0) cleaned[ownershipGroup] = Object.fromEntries(entries)
  }

  return Object.keys(cleaned).length === 0
    ? '{}'
    : JSON.stringify(cleaned, null, 2)
}

function getGroupNames(...jsonStrings: string[]): string[] {
  const names = new Set<string>()

  for (const jsonString of jsonStrings) {
    const parsed = safeParseRecord(jsonString)
    for (const key of Object.keys(parsed)) {
      if (key.trim()) names.add(key)
    }
  }

  return [...names].sort((a, b) => a.localeCompare(b))
}

function getDescriptionMap(
  groupRatio: string,
  userUsableGroups: string
): Record<string, string> {
  const descriptions: Record<string, string> = {}
  const ratioGroups = safeParseRecord(groupRatio)
  const usableGroups = safeParseRecord(userUsableGroups)

  for (const groupName of Object.keys(ratioGroups)) {
    descriptions[groupName] = groupName
  }
  for (const [groupName, desc] of Object.entries(usableGroups)) {
    descriptions[groupName] = typeof desc === 'string' ? desc : groupName
  }

  return descriptions
}

export function GroupSpecialUsableRulesEditor(
  props: GroupSpecialUsableRulesEditorProps
) {
  const { t } = useTranslation()
  const availableGroups = useMemo(
    () => getGroupNames(props.groupRatio, props.userUsableGroups),
    [props.groupRatio, props.userUsableGroups]
  )
  const descriptions = useMemo(
    () => getDescriptionMap(props.groupRatio, props.userUsableGroups),
    [props.groupRatio, props.userUsableGroups]
  )
  const rules = useMemo(() => safeParseSpecialUsable(props.value), [props.value])
  const ownershipGroups = useMemo(() => {
    const names = new Set([...availableGroups, ...Object.keys(rules)])
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [availableGroups, rules])

  const [selectedTargetGroups, setSelectedTargetGroups] = useState<
    Record<string, string>
  >({})

  const groupRows = useMemo(
    () =>
      ownershipGroups.map((ownershipGroup) => {
        const addedGroups = Object.entries(rules[ownershipGroup] ?? {})
          .filter(([rawKey]) => rawKey.startsWith('+:'))
          .map(([rawKey, desc]) => ({
            rawKey,
            groupName: rawKey.slice(2),
            description: desc,
          }))
          .sort((a, b) => a.groupName.localeCompare(b.groupName))
        const added = new Set(addedGroups.map((group) => group.groupName))
        const addableGroups = availableGroups.filter(
          (groupName) => groupName !== ownershipGroup && !added.has(groupName)
        )

        return {
          ownershipGroup,
          addedGroups,
          addableGroups,
        }
      }),
    [availableGroups, ownershipGroups, rules]
  )

  const emit = useCallback(
    (nextRules: SpecialUsableMap) => {
      props.onChange(serializeSpecialUsable(nextRules))
    },
    [props.onChange]
  )

  const addGroup = useCallback(
    (ownershipGroup: string, targetGroup: string) => {
      if (!ownershipGroup || !targetGroup) return

      const nextRules: SpecialUsableMap = {
        ...rules,
        [ownershipGroup]: {
          ...(rules[ownershipGroup] ?? {}),
          [`+:${targetGroup}`]: descriptions[targetGroup] || targetGroup,
        },
      }

      emit(nextRules)
      setSelectedTargetGroups((current) => ({
        ...current,
        [ownershipGroup]: '',
      }))
    },
    [descriptions, emit, rules]
  )

  const removeGroup = useCallback(
    (ownershipGroup: string, rawKey: string) => {
      if (!ownershipGroup) return
      const nextInner = { ...(rules[ownershipGroup] ?? {}) }
      delete nextInner[rawKey]

      const nextRules = { ...rules }
      if (Object.keys(nextInner).length === 0) {
        delete nextRules[ownershipGroup]
      } else {
        nextRules[ownershipGroup] = nextInner
      }

      emit(nextRules)
    },
    [emit, rules]
  )

  return (
    <Card className={sectionCardClassName}>
      <CardHeader className={sectionHeaderClassName}>
        <CardTitle>{t('Additional usable groups')}</CardTitle>
        <CardDescription>
          {t(
            'Choose an ownership group, then add channel/model groups it can also use.'
          )}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className='flex flex-col gap-4'>
          {ownershipGroups.length === 0 ? (
            <p className='text-muted-foreground py-4 text-center text-sm'>
              {t('Create at least one group before adding usable group rules.')}
            </p>
          ) : (
            <>
              <div className='overflow-hidden rounded-lg border'>
                {groupRows.map((row) => {
                  const selectedTarget =
                    selectedTargetGroups[row.ownershipGroup] &&
                    row.addableGroups.includes(
                      selectedTargetGroups[row.ownershipGroup]
                    )
                      ? selectedTargetGroups[row.ownershipGroup]
                      : row.addableGroups[0] ?? ''

                  return (
                    <div
                      key={row.ownershipGroup}
                      className='grid gap-3 border-b px-3 py-3 last:border-b-0 md:grid-cols-[220px_minmax(0,1fr)] md:items-center'
                    >
                      <div className='min-w-0'>
                        <div className='flex items-center gap-2'>
                          <div className='truncate font-medium'>
                            {row.ownershipGroup}
                          </div>
                          <StatusBadge variant='neutral' copyable={false}>
                            {row.addedGroups.length}
                          </StatusBadge>
                        </div>
                        <div className='text-muted-foreground mt-1 truncate text-sm'>
                          {descriptions[row.ownershipGroup] ||
                            row.ownershipGroup}
                        </div>
                      </div>

                      <div className='flex min-w-0 flex-col gap-2 lg:flex-row lg:items-center lg:justify-end'>
                        <div className='flex min-w-0 flex-1 flex-wrap gap-2'>
                          {row.addedGroups.length === 0 ? (
                            <span className='text-muted-foreground text-sm'>
                              {t('No additional usable groups yet.')}
                            </span>
                          ) : (
                            row.addedGroups.map((group) => (
                              <Badge
                                key={group.rawKey}
                                variant='secondary'
                                className='max-w-full gap-1 pr-1'
                                title={group.description || group.groupName}
                              >
                                <span className='truncate'>
                                  {group.groupName}
                                </span>
                                <button
                                  type='button'
                                  className='text-muted-foreground hover:text-destructive inline-flex rounded-sm'
                                  onClick={() =>
                                    removeGroup(
                                      row.ownershipGroup,
                                      group.rawKey
                                    )
                                  }
                                  aria-label={t('Remove')}
                                >
                                  <Trash2 className='h-3 w-3' />
                                </button>
                              </Badge>
                            ))
                          )}
                        </div>

                        <div className='grid shrink-0 gap-2 sm:grid-cols-[220px_auto]'>
                          <Select
                            items={row.addableGroups.map((groupName) => ({
                              value: groupName,
                              label: groupName,
                            }))}
                            value={selectedTarget}
                            onValueChange={(value) => {
                              if (value !== null) {
                                setSelectedTargetGroups((current) => ({
                                  ...current,
                                  [row.ownershipGroup]: value,
                                }))
                              }
                            }}
                            disabled={row.addableGroups.length === 0}
                          >
                            <SelectTrigger>
                              <SelectValue placeholder={t('Add usable group')} />
                            </SelectTrigger>
                            <SelectContent alignItemWithTrigger={false}>
                              <SelectGroup>
                                {row.addableGroups.map((groupName) => (
                                  <SelectItem key={groupName} value={groupName}>
                                    {groupName}
                                  </SelectItem>
                                ))}
                              </SelectGroup>
                            </SelectContent>
                          </Select>

                          <Button
                            type='button'
                            variant='outline'
                            onClick={() =>
                              addGroup(row.ownershipGroup, selectedTarget)
                            }
                            disabled={!selectedTarget}
                          >
                            <Plus className='mr-1 h-4 w-4' />
                            {t('Add')}
                          </Button>
                        </div>
                      </div>
                    </div>
                  )
                })}
              </div>
            </>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
