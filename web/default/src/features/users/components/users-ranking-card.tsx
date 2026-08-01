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
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { type TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { formatQuota, formatTimestamp } from '@/lib/format'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemFooter,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from '@/components/ui/item'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { getUserStatisticsRanking } from '../api'
import { USER_ROLES, USER_STATUSES } from '../constants'
import {
  type UserStatisticsRankingType,
  type UserStatisticsRankingUser,
} from '../types'

const INITIAL_RANKING_LIMIT = 10 as const
const EXPANDED_RANKING_LIMIT = 20 as const
const RANKING_TYPES: UserStatisticsRankingType[] = [
  'recent',
  'usage',
  'balance',
]

type RankingLimit = typeof INITIAL_RANKING_LIMIT | typeof EXPANDED_RANKING_LIMIT

function getInitials(username: string): string {
  return username.trim().slice(0, 2).toUpperCase() || 'U'
}

function getRankingValue(
  user: UserStatisticsRankingUser,
  rankingType: UserStatisticsRankingType
): string {
  if (rankingType === 'recent') return formatTimestamp(user.created_at)
  if (rankingType === 'usage') return formatQuota(user.used_quota)
  return formatQuota(user.quota)
}

function RankingUsers(props: {
  users: UserStatisticsRankingUser[]
  rankingType: UserStatisticsRankingType
  metricLabel: string
  t: TFunction
}) {
  return (
    <>
      <ItemGroup className='md:hidden'>
        {props.users.map((user) => {
          const role = USER_ROLES[user.role as keyof typeof USER_ROLES]
          const status =
            USER_STATUSES[user.status as keyof typeof USER_STATUSES]
          return (
            <Item key={user.id} variant='outline' size='sm'>
              <ItemMedia>
                <Avatar className='size-8'>
                  <AvatarFallback>{getInitials(user.username)}</AvatarFallback>
                </Avatar>
              </ItemMedia>
              <ItemContent>
                <ItemTitle>{user.username}</ItemTitle>
                {user.display_name && user.display_name !== user.username && (
                  <ItemDescription>{user.display_name}</ItemDescription>
                )}
              </ItemContent>
              <ItemActions>
                {status && (
                  <StatusBadge
                    label={props.t(status.labelKey)}
                    variant={status.variant}
                    showDot={status.showDot}
                    copyable={false}
                  />
                )}
              </ItemActions>
              <ItemFooter>
                <div className='text-muted-foreground flex min-w-0 items-center gap-2 text-xs'>
                  <span>
                    {role ? props.t(role.labelKey) : props.t('Other')}
                  </span>
                  <span>·</span>
                  <span className='truncate'>
                    {props.metricLabel}:{' '}
                    {getRankingValue(user, props.rankingType)}
                  </span>
                </div>
                <GroupBadge group={user.group} />
              </ItemFooter>
            </Item>
          )
        })}
      </ItemGroup>

      <div className='hidden rounded-lg border md:block'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{props.t('User')}</TableHead>
              <TableHead>{props.t('Status')}</TableHead>
              <TableHead>{props.t('Role')}</TableHead>
              <TableHead>{props.t('Group')}</TableHead>
              <TableHead>{props.metricLabel}</TableHead>
              <TableHead>{props.t('Last Login')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {props.users.map((user) => {
              const role = USER_ROLES[user.role as keyof typeof USER_ROLES]
              const status =
                USER_STATUSES[user.status as keyof typeof USER_STATUSES]
              return (
                <TableRow key={user.id}>
                  <TableCell>
                    <div className='flex min-w-44 items-center gap-2.5'>
                      <Avatar className='size-8'>
                        <AvatarFallback>
                          {getInitials(user.username)}
                        </AvatarFallback>
                      </Avatar>
                      <div className='min-w-0'>
                        <div className='truncate font-medium'>
                          {user.username}
                        </div>
                        {user.display_name &&
                          user.display_name !== user.username && (
                            <div className='text-muted-foreground truncate text-xs'>
                              {user.display_name}
                            </div>
                          )}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell>
                    {status ? (
                      <StatusBadge
                        label={props.t(status.labelKey)}
                        variant={status.variant}
                        showDot={status.showDot}
                        copyable={false}
                      />
                    ) : (
                      '-'
                    )}
                  </TableCell>
                  <TableCell>
                    {role ? props.t(role.labelKey) : props.t('Other')}
                  </TableCell>
                  <TableCell>
                    <GroupBadge group={user.group} />
                  </TableCell>
                  <TableCell className='font-mono tabular-nums'>
                    {getRankingValue(user, props.rankingType)}
                  </TableCell>
                  <TableCell className='text-muted-foreground whitespace-nowrap'>
                    {formatTimestamp(user.last_login_at)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

export function UsersRankingCard(props: { onViewAll: () => void }) {
  const { t } = useTranslation()
  const [rankingType, setRankingType] =
    useState<UserStatisticsRankingType>('recent')
  const [limits, setLimits] = useState<
    Record<UserStatisticsRankingType, RankingLimit>
  >({
    recent: INITIAL_RANKING_LIMIT,
    usage: INITIAL_RANKING_LIMIT,
    balance: INITIAL_RANKING_LIMIT,
  })
  const activeLimit = limits[rankingType]

  const rankingConfig: Record<
    UserStatisticsRankingType,
    { label: string; description: string; metricLabel: string }
  > = {
    recent: {
      label: t('Recent Registrations'),
      description: t('Latest registered user accounts'),
      metricLabel: t('Registered At'),
    },
    usage: {
      label: t('Highest Spending'),
      description: t('Users with the highest total consumption'),
      metricLabel: t('Total Spending'),
    },
    balance: {
      label: t('Highest Balance'),
      description: t('Users with the highest remaining balance'),
      metricLabel: t('Current Balance'),
    },
  }
  const activeConfig = rankingConfig[rankingType]

  const rankingQuery = useQuery({
    queryKey: ['users', 'statistics', 'ranking', rankingType, activeLimit],
    queryFn: async () => {
      const response = await getUserStatisticsRanking({
        type: rankingType,
        limit: activeLimit,
      })
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Failed to load user rankings')
      }
      return response.data
    },
    staleTime: 60_000,
    refetchOnMount: 'always',
    retry: 1,
  })

  const users = rankingQuery.data?.items ?? []
  const canExpand =
    activeLimit === INITIAL_RANKING_LIMIT &&
    rankingQuery.data?.has_more === true

  return (
    <Card>
      <Tabs
        value={rankingType}
        onValueChange={(value) => {
          if (RANKING_TYPES.includes(value as UserStatisticsRankingType)) {
            setRankingType(value as UserStatisticsRankingType)
          }
        }}
      >
        <CardHeader>
          <CardTitle>{t('User Rankings')}</CardTitle>
          <CardDescription>{activeConfig.description}</CardDescription>
          <CardAction>
            <Button variant='outline' size='sm' onClick={props.onViewAll}>
              {t('View all users')}
            </Button>
          </CardAction>
          <TabsList
            className='col-span-full mt-2 w-full sm:w-fit'
            aria-label={t('User Rankings')}
          >
            {RANKING_TYPES.map((type) => (
              <TabsTrigger key={type} value={type}>
                {rankingConfig[type].label}
              </TabsTrigger>
            ))}
          </TabsList>
        </CardHeader>

        <CardContent>
          {RANKING_TYPES.map((type) => (
            <TabsContent key={type} value={type}>
              {rankingType === type && (
                <>
                  {rankingQuery.isPending ? (
                    <Skeleton className='h-80 rounded-lg' />
                  ) : rankingQuery.isError ? (
                    <ErrorState
                      description={t('Failed to load user rankings')}
                      onRetry={() => rankingQuery.refetch()}
                    />
                  ) : users.length === 0 ? (
                    <EmptyState
                      className='min-h-48'
                      title={t('No ranked users')}
                      icon={undefined}
                    />
                  ) : (
                    <RankingUsers
                      users={users}
                      rankingType={rankingType}
                      metricLabel={activeConfig.metricLabel}
                      t={t}
                    />
                  )}
                </>
              )}
            </TabsContent>
          ))}
        </CardContent>

        {!rankingQuery.isError && users.length > 0 && (
          <CardFooter className='justify-center'>
            {canExpand ? (
              <Button
                variant='ghost'
                size='sm'
                onClick={() =>
                  setLimits((current) => ({
                    ...current,
                    [rankingType]: EXPANDED_RANKING_LIMIT,
                  }))
                }
              >
                {t('Show more (top 20)')}
              </Button>
            ) : rankingQuery.isFetching ? (
              <Button variant='ghost' size='sm' disabled>
                <Spinner data-icon='inline-start' />
                {t('Loading more...')}
              </Button>
            ) : (
              <span className='text-muted-foreground text-xs'>
                {t('Showing top {{count}} users', { count: users.length })}
              </span>
            )}
          </CardFooter>
        )}
      </Tabs>
    </Card>
  )
}
