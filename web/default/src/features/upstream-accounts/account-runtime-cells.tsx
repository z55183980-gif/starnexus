import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  Alert02Icon,
  InformationCircleIcon,
  Key01Icon,
  Link01Icon,
  Loading03Icon,
  RefreshIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  getUpstreamAccountQuota,
  getUpstreamAccountUsage,
  resetUpstreamAccountQuota,
} from './api'
import {
  upstreamOAuthRefreshBlocksScheduling,
  type UpstreamAccount,
  type UpstreamAccountQuotaUsage,
  type UpstreamAccountRateLimitWindow,
  type UpstreamAccountUsage,
} from './types'

type Translate = (key: string, options?: Record<string, unknown>) => string

const accountUsageCache = new Map<
  number,
  {
    data: UpstreamAccountQuotaUsage
    timestamp: number
    includesCredits: boolean
    credentialVersion: number
  }
>()
const accountUsageInflight = new Map<
  string,
  Promise<UpstreamAccountQuotaUsage>
>()
const accountUsageCacheTTL = 5 * 60 * 1000
const accountUsageQueue: Array<() => void> = []
const accountUsageConcurrency = 4
let activeAccountUsageRequests = 0

function drainAccountUsageQueue() {
  while (
    activeAccountUsageRequests < accountUsageConcurrency &&
    accountUsageQueue.length > 0
  ) {
    activeAccountUsageRequests += 1
    accountUsageQueue.shift()?.()
  }
}

function enqueueAccountUsageRequest<T>(request: () => Promise<T>) {
  return new Promise<T>((resolve, reject) => {
    accountUsageQueue.push(() => {
      void request()
        .then(resolve, reject)
        .finally(() => {
          activeAccountUsageRequests -= 1
          drainAccountUsageQueue()
        })
    })
    drainAccountUsageQueue()
  })
}

async function loadAccountUsage(
  accountId: number,
  credentialVersion: number,
  options: { force: boolean; includeCredits: boolean }
) {
  const cached = accountUsageCache.get(accountId)
  if (
    !options.force &&
    cached &&
    cached.credentialVersion === credentialVersion &&
    Date.now() - cached.timestamp < accountUsageCacheTTL &&
    (!options.includeCredits || cached.includesCredits)
  ) {
    return cached.data
  }

  const requestKey = `${accountId}:${credentialVersion}:${options.includeCredits}`
  const pending = accountUsageInflight.get(requestKey)
  if (pending && !options.force) return pending

  const request = enqueueAccountUsageRequest(() =>
    getUpstreamAccountQuota(accountId, options).then((response) => {
      if (!response.success || !response.data) {
        throw new Error(response.message || 'Failed to fetch usage')
      }
      accountUsageCache.set(accountId, {
        data: response.data,
        timestamp: Date.now(),
        includesCredits: options.includeCredits,
        credentialVersion,
      })
      return response.data
    })
  )
  accountUsageInflight.set(requestKey, request)
  try {
    return await request
  } finally {
    if (accountUsageInflight.get(requestKey) === request) {
      accountUsageInflight.delete(requestKey)
    }
  }
}

const anthropicUsageCache = new Map<
  number,
  { data: UpstreamAccountUsage; timestamp: number; credentialVersion: number }
>()

async function loadAnthropicUsage(
  accountId: number,
  credentialVersion: number,
  force: boolean
) {
  const cached = anthropicUsageCache.get(accountId)
  if (
    !force &&
    cached?.credentialVersion === credentialVersion &&
    Date.now() - cached.timestamp < accountUsageCacheTTL
  ) {
    return cached.data
  }
  const response = await enqueueAccountUsageRequest(() =>
    getUpstreamAccountUsage(accountId, { force })
  )
  if (!response.success || !response.data) {
    throw new Error(response.message || 'Failed to fetch usage')
  }
  anthropicUsageCache.set(accountId, {
    data: response.data,
    timestamp: Date.now(),
    credentialVersion,
  })
  return response.data
}

function compactModeLabel(account: UpstreamAccount, t: Translate) {
  if (!account.metadata.compact_supported) return ''
  if (account.metadata.compact_mode === 'force_on') return t('Compact On')
  if (account.metadata.compact_mode === 'force_off') return t('Compact Off')
  return t('Compact Auto')
}

function expiryLabel(value?: number | null) {
  if (!value) return ''
  return new Date(value * 1000).toLocaleDateString()
}

export function AccountIdentityCell({ account }: { account: UpstreamAccount }) {
  return (
    <div className='min-w-24 space-y-1'>
      <div className='text-muted-foreground font-mono text-xs'>
        #{account.id}
      </div>
      <div
        className='max-w-36 truncate text-xs font-medium'
        title={account.name}
      >
        {account.name}
      </div>
    </div>
  )
}

export function AccountPlatformCell({ account }: { account: UpstreamAccount }) {
  const { t } = useTranslation()
  const compact = compactModeLabel(account, t)
  const expiry = expiryLabel(account.expires_at)
  const isPrivate = account.metadata.privacy_mode === 'training_off'
  const typeLabel =
    account.type === 'oauth'
      ? 'OAuth'
      : account.type === 'apikey'
        ? t('Key')
        : account.type === 'setup_token'
          ? t('Setup Token')
          : account.type === 'bedrock'
            ? 'Bedrock'
            : 'Vertex AI'

  return (
    <div className='min-w-44 space-y-1.5'>
      <div className='flex flex-wrap gap-1'>
        <Badge variant='success' className='rounded-md'>
          {account.platform === 'openai' ? 'OpenAI' : 'Anthropic'}
        </Badge>
        <Badge variant='success' className='rounded-md'>
          <HugeiconsIcon
            icon={account.type === 'oauth' ? Link01Icon : Key01Icon}
            strokeWidth={2}
          />
          {typeLabel}
        </Badge>
      </div>
      {(account.metadata.plan_type || isPrivate) && (
        <div className='flex flex-wrap gap-1'>
          {account.metadata.plan_type && (
            <Badge variant='success' className='rounded-md capitalize'>
              {account.metadata.plan_type}
            </Badge>
          )}
          {isPrivate && (
            <Badge variant='success' className='rounded-md'>
              {t('Private')}
            </Badge>
          )}
        </div>
      )}
      {expiry && (
        <div className='text-muted-foreground text-[11px]'>
          {t('Expires {{date}}', { date: expiry })}
        </div>
      )}
      {compact && (
        <div className='text-muted-foreground flex items-center gap-1 text-[11px]'>
          <span className='size-1.5 rounded-full bg-slate-400' />
          {compact}
        </div>
      )}
    </div>
  )
}

export function AccountCapacityCell({ account }: { account: UpstreamAccount }) {
  const full = account.current_concurrency >= account.concurrency
  const active = account.current_concurrency > 0
  return (
    <Badge
      variant={full ? 'destructive' : active ? 'warning' : 'secondary'}
      className='rounded-md font-mono text-[11px]'
    >
      {account.current_concurrency} / {account.concurrency}
    </Badge>
  )
}

function accountStatusCountdown(
  timestamp: number | null | undefined,
  now: number
) {
  if (!timestamp) return ''
  const seconds = Math.max(0, timestamp - Math.floor(now / 1000))
  if (seconds <= 0) return ''
  const days = Math.floor(seconds / 86_400)
  const hours = Math.floor((seconds % 86_400) / 3_600)
  const minutes = Math.floor((seconds % 3_600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${Math.max(1, minutes)}m`
}

function accountStatusDateTime(timestamp: number | null | undefined) {
  if (!timestamp) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

function accountExpiredForScheduling(account: UpstreamAccount, now: number) {
  return Boolean(
    account.auto_pause_on_expired &&
    account.expires_at &&
    account.expires_at * 1000 <= now
  )
}

function StatusDetailTooltip({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <TooltipProvider delay={150}>
      <Tooltip>
        <TooltipTrigger
          render={
            <button
              type='button'
              className='inline-flex cursor-help items-center'
              aria-label={label}
            />
          }
        >
          {children}
        </TooltipTrigger>
        <TooltipContent
          side='bottom'
          align='start'
          className='max-w-72 whitespace-pre-wrap'
        >
          {label}
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

export function AccountStatusCell({ account }: { account: UpstreamAccount }) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  const rateLimited = Boolean(
    account.rate_limit_reset_at && account.rate_limit_reset_at * 1000 > now
  )
  const overloaded = Boolean(
    account.overload_until && account.overload_until * 1000 > now
  )
  const temporaryWindowActive = Boolean(
    account.temp_unschedulable_until &&
    account.temp_unschedulable_until * 1000 > now
  )
  const refreshBlocked = upstreamOAuthRefreshBlocksScheduling(
    account.temp_unschedulable_reason
  )
  const tracksExpiration = Boolean(
    account.auto_pause_on_expired && account.expires_at
  )
  const expiredForScheduling = accountExpiredForScheduling(account, now)

  useEffect(() => {
    if (
      !rateLimited &&
      !overloaded &&
      !temporaryWindowActive &&
      !tracksExpiration
    )
      return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [overloaded, rateLimited, temporaryWindowActive, tracksExpiration])

  if (rateLimited) {
    const countdown = accountStatusCountdown(account.rate_limit_reset_at, now)
    return (
      <div className='flex items-center gap-2'>
        <div className='flex flex-col items-center gap-1'>
          <Badge variant='warning' className='rounded-md'>
            {t('Rate limited status')}
          </Badge>
          <span className='text-muted-foreground text-[11px]'>
            {t('{{time}} auto recovery', { time: countdown })}
          </span>
        </div>
        <StatusDetailTooltip
          label={t('Rate limited until {{time}}', {
            time: accountStatusDateTime(account.rate_limit_reset_at),
          })}
        >
          <Badge variant='warning' className='rounded-md'>
            <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} />
            429
          </Badge>
        </StatusDetailTooltip>
      </div>
    )
  }

  if (overloaded) {
    const countdown = accountStatusCountdown(account.overload_until, now)
    return (
      <div className='flex items-center gap-2'>
        <div className='flex flex-col items-center gap-1'>
          <Badge variant='destructive' className='rounded-md'>
            {t('Overloaded status')}
          </Badge>
          <span className='text-muted-foreground text-[11px]'>
            {t('{{time}} auto recovery', { time: countdown })}
          </span>
        </div>
        <StatusDetailTooltip
          label={t('Overloaded until {{time}}', {
            time: accountStatusDateTime(account.overload_until),
          })}
        >
          <Badge variant='destructive' className='rounded-md'>
            <HugeiconsIcon icon={Alert02Icon} strokeWidth={2} />
            529
          </Badge>
        </StatusDetailTooltip>
      </div>
    )
  }

  if (account.temp_unschedulable_reason === 'oauth_refresh_permanent') {
    return (
      <StatusDetailTooltip
        label={t(
          'OAuth credentials cannot be refreshed automatically. Reauthorize the account to restore scheduling.'
        )}
      >
        <Badge variant='destructive' className='rounded-md'>
          {t('Reauthorization required')}
        </Badge>
      </StatusDetailTooltip>
    )
  }

  if (account.status === 'error') {
    return (
      <div className='flex items-center gap-1'>
        <Badge variant='destructive' className='rounded-md'>
          {t('Error')}
        </Badge>
        {account.error_message && (
          <StatusDetailTooltip label={account.error_message}>
            <HugeiconsIcon
              icon={InformationCircleIcon}
              className='text-destructive size-4'
              strokeWidth={2}
            />
          </StatusDetailTooltip>
        )}
      </div>
    )
  }

  if (refreshBlocked) {
    const refreshFailed =
      account.temp_unschedulable_reason === 'oauth_refresh_failed'
    const countdown = accountStatusCountdown(
      account.temp_unschedulable_until,
      now
    )
    const detail = [
      t('Scheduling stays paused until credential refresh succeeds.'),
      refreshFailed && countdown
        ? t('Retry after {{time}}', { time: countdown })
        : '',
    ]
      .filter(Boolean)
      .join('\n')
    return (
      <StatusDetailTooltip label={detail}>
        <div className='flex flex-col items-center gap-1'>
          <Badge variant='warning' className='rounded-md'>
            {refreshFailed
              ? t('Credential refresh failed')
              : t('Credential refresh pending')}
          </Badge>
          {refreshFailed && countdown && (
            <span className='text-muted-foreground text-[11px]'>
              {t('Retry after {{time}}', { time: countdown })}
            </span>
          )}
        </div>
      </StatusDetailTooltip>
    )
  }

  if (temporaryWindowActive) {
    const countdown = accountStatusCountdown(
      account.temp_unschedulable_until,
      now
    )
    const detail = account.temp_unschedulable_reason
      ? `${account.temp_unschedulable_reason}\n${t('{{time}} auto recovery', { time: countdown })}`
      : t('{{time}} auto recovery', { time: countdown })
    return (
      <StatusDetailTooltip label={detail}>
        <div className='flex flex-col items-center gap-1'>
          <Badge variant='warning' className='rounded-md'>
            {t('Temporarily unavailable')}
          </Badge>
          <span className='text-muted-foreground text-[11px]'>
            {t('{{time}} auto recovery', { time: countdown })}
          </span>
        </div>
      </StatusDetailTooltip>
    )
  }

  if (account.status === 'inactive') {
    return (
      <Badge variant='secondary' className='rounded-md'>
        {t('Inactive status')}
      </Badge>
    )
  }

  if (account.status === 'expired' || expiredForScheduling) {
    return (
      <Badge variant='secondary' className='rounded-md'>
        {t('Expired status')}
      </Badge>
    )
  }

  if (!account.schedulable) {
    return (
      <Badge variant='secondary' className='rounded-md'>
        {t('Account status paused')}
      </Badge>
    )
  }

  return (
    <Badge variant='success' className='rounded-md'>
      {t('Available for scheduling')}
    </Badge>
  )
}

export function AccountSchedulingCell({
  account,
  busy,
  onChange,
}: {
  account: UpstreamAccount
  busy: boolean
  onChange: (checked: boolean) => void
}) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  const tracksExpiration = Boolean(
    account.expires_at ||
    account.rate_limit_reset_at ||
    account.overload_until ||
    account.temp_unschedulable_until
  )
  useEffect(() => {
    if (!tracksExpiration) return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [tracksExpiration])
  const expiredForScheduling = accountExpiredForScheduling(account, now)
  const refreshBlocked = upstreamOAuthRefreshBlocksScheduling(
    account.temp_unschedulable_reason
  )
  return (
    <Switch
      checked={account.schedulable}
      disabled={
        busy ||
        account.status === 'expired' ||
        expiredForScheduling ||
        refreshBlocked
      }
      size='lg'
      variant='success'
      aria-label={t('Toggle scheduling for {{name}}', { name: account.name })}
      onCheckedChange={onChange}
    />
  )
}

export function AccountPoolsCell({ poolNames }: { poolNames: string[] }) {
  const visible = poolNames.slice(0, 2)
  const hidden = poolNames.slice(2)
  if (poolNames.length === 0)
    return <span className='text-muted-foreground'>-</span>
  return (
    <div className='flex max-w-40 flex-wrap gap-1'>
      {visible.map((name) => (
        <Badge key={name} variant='success' className='rounded-md'>
          {name}
        </Badge>
      ))}
      {hidden.length > 0 && (
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant='ghost'
                size='xs'
                className='h-5 px-1.5 text-[11px]'
              />
            }
          >
            +{hidden.length}
          </PopoverTrigger>
          <PopoverContent align='start' className='w-52'>
            <div className='flex flex-wrap gap-1'>
              {hidden.map((name) => (
                <Badge key={name} variant='secondary' className='rounded-md'>
                  {name}
                </Badge>
              ))}
            </div>
          </PopoverContent>
        </Popover>
      )}
    </div>
  )
}

function classifyWindows(usage: UpstreamAccountQuotaUsage | null) {
  const primary = usage?.rate_limit?.primary_window ?? null
  const secondary = usage?.rate_limit?.secondary_window ?? null
  const windows = [primary, secondary].filter(
    Boolean
  ) as UpstreamAccountRateLimitWindow[]
  let fiveHour: UpstreamAccountRateLimitWindow | null = null
  let weekly: UpstreamAccountRateLimitWindow | null = null
  for (const window of windows) {
    if (window.limit_window_seconds >= 24 * 60 * 60) weekly ??= window
    else fiveHour ??= window
  }
  if (!fiveHour && !weekly) {
    fiveHour = primary
    weekly = secondary
  }
  return { fiveHour, weekly }
}

function resetCountdown(
  window: UpstreamAccountRateLimitWindow | null,
  now: number,
  t: Translate
) {
  if (!window) return '-'
  const seconds = Math.max(
    0,
    window.reset_at > 0
      ? window.reset_at - Math.floor(now / 1000)
      : window.reset_after_seconds
  )
  if (seconds <= 0)
    return window.used_percent > 0 ? t('Pending refresh') : t('Now')
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (hours >= 24) return `${Math.floor(hours / 24)}d ${hours % 24}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${Math.max(1, minutes)}m`
}

function UsageWindow({
  label,
  window,
  now,
}: {
  label: string
  window: UpstreamAccountRateLimitWindow | null
  now: number
}) {
  const { t } = useTranslation()
  const percent = Math.max(0, Math.min(100, Number(window?.used_percent) || 0))
  return (
    <div className='flex items-center gap-1.5'>
      <span className='w-7 rounded-sm bg-indigo-100 px-1 py-0.5 text-center text-[10px] font-medium text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300'>
        {label}
      </span>
      <Progress value={percent} className='w-9 gap-0' />
      <span className='w-8 text-right text-[10px] tabular-nums'>
        {window ? `${Math.round(percent)}%` : '-'}
      </span>
      <span className='text-muted-foreground min-w-12 text-[10px]'>
        {resetCountdown(window, now, t)}
      </span>
    </div>
  )
}

function resetCreditExpiryTimestamp(value: string) {
  const timestamp = new Date(value).getTime()
  return Number.isNaN(timestamp) ? Number.POSITIVE_INFINITY : timestamp
}

function formatResetCreditExpiry(value: string, includeYear = false) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    ...(includeYear ? { year: 'numeric' } : {}),
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function AccountUsageCell({ account }: { account: UpstreamAccount }) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const rootRef = useRef<HTMLDivElement>(null)
  const [usage, setUsage] = useState<UpstreamAccountQuotaUsage | null>(null)
  const [anthropicUsage, setAnthropicUsage] =
    useState<UpstreamAccountUsage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [resetMessage, setResetMessage] = useState('')
  const [resetting, setResetting] = useState(false)
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false)
  const [showCreditDetails, setShowCreditDetails] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const [visible, setVisible] = useState(
    () =>
      typeof window === 'undefined' ||
      !window.matchMedia('(max-width: 767px)').matches
  )
  const isOpenAI = account.platform === 'openai' && account.type === 'oauth'
  const isAnthropic =
    account.platform === 'anthropic' &&
    (account.type === 'oauth' || account.type === 'setup_token')
  const supported = isOpenAI || isAnthropic
  const windows = useMemo(() => classifyWindows(usage), [usage])
  const displayedWindows = isAnthropic
    ? ([
        ['5h', anthropicUsage?.five_hour ?? null],
        ['7d', anthropicUsage?.seven_day ?? null],
        ['7d S', anthropicUsage?.seven_day_sonnet ?? null],
        ['7d F', anthropicUsage?.seven_day_fable ?? null],
      ] as const)
    : ([
        ['5h', windows.fiveHour],
        ['7d', windows.weekly],
      ] as const)
  const creditsLoaded = usage?.rate_limit_reset_credits != null
  const credits = usage?.rate_limit_reset_credits?.available_count ?? 0
  const creditExpirations = useMemo(
    () =>
      (usage?.rate_limit_reset_credits?.credits ?? [])
        .map((credit) => credit.expires_at?.trim() ?? '')
        .filter(Boolean)
        .sort((left, right) => {
          const difference =
            resetCreditExpiryTimestamp(left) - resetCreditExpiryTimestamp(right)
          return difference || left.localeCompare(right)
        }),
    [usage]
  )
  const primaryCreditExpiration = creditExpirations[0] ?? ''
  const hiddenCreditExpirationCount = Math.max(creditExpirations.length - 1, 0)
  const refreshSchedulingState = useCallback(() => {
    void Promise.all([
      queryClient.invalidateQueries({ queryKey: ['upstream-accounts'] }),
      queryClient.invalidateQueries({ queryKey: ['upstream-account-pools'] }),
    ])
  }, [queryClient])

  useEffect(() => {
    if (visible || !supported || !rootRef.current) return
    if (typeof IntersectionObserver === 'undefined') {
      setVisible(true)
      return
    }
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setVisible(true)
          observer.disconnect()
        }
      },
      { rootMargin: '160px' }
    )
    observer.observe(rootRef.current)
    return () => observer.disconnect()
  }, [supported, visible])

  useEffect(() => {
    if (!usage && !anthropicUsage) return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [anthropicUsage, usage])

  const query = useCallback(
    async (force = true) => {
      setLoading(true)
      setError('')
      setResetMessage('')
      setShowCreditDetails(false)
      try {
        if (isOpenAI) {
          const data = await loadAccountUsage(
            account.id,
            account.credential_version,
            { force, includeCredits: force }
          )
          setUsage(data)
        } else {
          const data = await loadAnthropicUsage(
            account.id,
            account.credential_version,
            force
          )
          setAnthropicUsage(data)
        }
        setNow(Date.now())
      } catch (queryError) {
        const message =
          queryError instanceof Error
            ? queryError.message
            : t('Failed to fetch usage')
        setError(message)
        refreshSchedulingState()
      } finally {
        setLoading(false)
      }
    },
    [
      account.credential_version,
      account.id,
      isOpenAI,
      refreshSchedulingState,
      t,
    ]
  )

  useEffect(() => {
    if (!supported || !visible) return
    let active = true
    setUsage(null)
    setAnthropicUsage(null)
    setLoading(true)
    setError('')
    setResetMessage('')
    setShowCreditDetails(false)
    const request = isOpenAI
      ? loadAccountUsage(account.id, account.credential_version, {
          force: false,
          includeCredits: false,
        })
      : loadAnthropicUsage(account.id, account.credential_version, false)
    void request
      .then((data) => {
        if (!active) return
        if (isOpenAI) setUsage(data as UpstreamAccountQuotaUsage)
        else setAnthropicUsage(data as UpstreamAccountUsage)
        setNow(Date.now())
      })
      .catch((queryError) => {
        if (!active) return
        setError(
          queryError instanceof Error
            ? queryError.message
            : t('Failed to fetch usage')
        )
        refreshSchedulingState()
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [
    account.credential_version,
    account.id,
    isOpenAI,
    refreshSchedulingState,
    supported,
    t,
    visible,
  ])

  const reset = async () => {
    setResetting(true)
    setError('')
    setResetMessage('')
    try {
      const response = await resetUpstreamAccountQuota(account.id)
      if (!response.success)
        throw new Error(response.message || t('Reset failed'))
      setResetConfirmOpen(false)
      await query(true)
      setResetMessage(
        t('Reset {{count}} usage windows successfully', {
          count: response.data?.windows_reset ?? 0,
        })
      )
    } catch (error) {
      setError(error instanceof Error ? error.message : t('Reset failed'))
      refreshSchedulingState()
    } finally {
      setResetting(false)
    }
  }

  if (!supported) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  return (
    <>
      <div ref={rootRef} className='flex min-w-48 flex-col gap-1.5'>
        {loading && !usage && !anthropicUsage ? (
          <div className='flex flex-col gap-1.5' aria-label={t('Loading...')}>
            <Skeleton className='h-4 w-36' />
            <Skeleton className='h-4 w-36' />
          </div>
        ) : (
          displayedWindows.map(([label, value]) =>
            value || label === '5h' || label === '7d' ? (
              <UsageWindow key={label} label={label} window={value} now={now} />
            ) : null
          )
        )}
        {anthropicUsage?.source === 'estimated' && (
          <span className='text-muted-foreground text-[10px]'>
            {t('Estimated from the setup-token session window')}
          </span>
        )}
        {error && (
          <StatusDetailTooltip
            label={`${t(
              'Usage query errors are shown separately. Credential refresh failures may pause scheduling.'
            )}\n${error}`}
          >
            <span className='inline-flex items-center gap-1'>
              <Badge variant='warning' className='rounded-md'>
                {t('Usage unavailable')}
              </Badge>
              <HugeiconsIcon
                icon={InformationCircleIcon}
                className='size-3.5 text-amber-600'
                strokeWidth={2}
              />
            </span>
          </StatusDetailTooltip>
        )}
        {!error && resetMessage && (
          <span className='text-success block max-w-44 truncate text-[10px]'>
            {resetMessage}
          </span>
        )}
        <div className='flex flex-wrap items-center gap-1 text-[11px]'>
          <ActionTooltip
            label={t(
              usage || anthropicUsage
                ? 'Query upstream usage again'
                : isOpenAI
                  ? 'Query upstream usage and reset credits'
                  : 'Query upstream usage'
            )}
          >
            <Button
              variant='link'
              size='xs'
              disabled={loading || resetting}
              onClick={() => void query()}
            >
              {loading ? (
                <HugeiconsIcon
                  icon={Loading03Icon}
                  data-icon='inline-start'
                  className='animate-spin'
                />
              ) : (
                <HugeiconsIcon icon={RefreshIcon} data-icon='inline-start' />
              )}
              {t('Query')}
            </Button>
          </ActionTooltip>
          {isOpenAI && (
            <>
              <span className='text-muted-foreground'>
                {creditsLoaded
                  ? t('{{count}} credits', { count: credits })
                  : t('Credits')}
              </span>
              <ActionTooltip
                label={t(
                  !creditsLoaded
                    ? 'Query reset credits before resetting quota'
                    : credits <= 0
                      ? 'No reset credits are available'
                      : 'Consume one reset credit'
                )}
              >
                <Button
                  variant='destructive'
                  size='xs'
                  disabled={
                    !creditsLoaded || credits <= 0 || loading || resetting
                  }
                  onClick={() => setResetConfirmOpen(true)}
                >
                  {resetting ? t('Resetting...') : t('Reset')}
                </Button>
              </ActionTooltip>
            </>
          )}
        </div>
        {primaryCreditExpiration && (
          <div className='flex flex-col gap-1'>
            <div className='flex flex-wrap items-center gap-1'>
              <Badge
                variant='secondary'
                className='rounded-md text-[10px] tabular-nums'
                title={t('Reset credit expires at {{time}}', {
                  time: formatResetCreditExpiry(primaryCreditExpiration, true),
                })}
              >
                {t('Expires {{time}}', {
                  time: formatResetCreditExpiry(primaryCreditExpiration),
                })}
              </Badge>
              {hiddenCreditExpirationCount > 0 && (
                <Collapsible
                  open={showCreditDetails}
                  onOpenChange={setShowCreditDetails}
                >
                  <CollapsibleTrigger
                    render={
                      <Button
                        variant='ghost'
                        size='xs'
                        aria-label={t(
                          showCreditDetails
                            ? 'Collapse reset credit expirations'
                            : 'Expand {{count}} more reset credit expirations',
                          { count: hiddenCreditExpirationCount }
                        )}
                      />
                    }
                  >
                    +{hiddenCreditExpirationCount}
                  </CollapsibleTrigger>
                  <CollapsibleContent className='border-border bg-muted/40 mt-1 flex flex-col gap-0.5 rounded-md border px-2 py-1'>
                    {creditExpirations.slice(1).map((expiresAt, index) => (
                      <span
                        key={`${expiresAt}-${index}`}
                        className='text-muted-foreground text-[10px] tabular-nums'
                      >
                        {formatResetCreditExpiry(expiresAt, true)}
                      </span>
                    ))}
                  </CollapsibleContent>
                </Collapsible>
              )}
            </div>
          </div>
        )}
      </div>
      <AlertDialog open={resetConfirmOpen} onOpenChange={setResetConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Reset quota?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This consumes one of {{count}} available reset credits and resets the usage windows for {{name}}.',
                { count: credits, name: account.name }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetting}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={resetting}
              onClick={(event) => {
                event.preventDefault()
                void reset()
              }}
            >
              {resetting ? t('Resetting...') : t('Reset')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}

function ActionTooltip({
  label,
  children,
}: {
  label: string
  children: ReactNode
}) {
  return (
    <TooltipProvider delay={150}>
      <Tooltip>
        <TooltipTrigger render={<span className='inline-flex' />}>
          {children}
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
