import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
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
import { toast } from 'sonner'
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
import { getUpstreamAccountQuota, resetUpstreamAccountQuota } from './api'
import type {
  UpstreamAccount,
  UpstreamAccountQuotaUsage,
  UpstreamAccountRateLimitWindow,
} from './types'

type Translate = (key: string, options?: Record<string, unknown>) => string

const accountUsageCache = new Map<
  number,
  {
    data: UpstreamAccountQuotaUsage
    timestamp: number
    includesCredits: boolean
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
  options: { force: boolean; includeCredits: boolean }
) {
  const cached = accountUsageCache.get(accountId)
  if (
    !options.force &&
    cached &&
    Date.now() - cached.timestamp < accountUsageCacheTTL &&
    (!options.includeCredits || cached.includesCredits)
  ) {
    return cached.data
  }

  const requestKey = `${accountId}:${options.includeCredits}`
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
  const temporarilyUnavailable = Boolean(
    account.temp_unschedulable_until &&
    account.temp_unschedulable_until * 1000 > now
  )

  useEffect(() => {
    if (!rateLimited && !overloaded && !temporarilyUnavailable) return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [overloaded, rateLimited, temporarilyUnavailable])

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

  if (temporarilyUnavailable) {
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

  if (account.status === 'expired') {
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
      {t('Normal status')}
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
  return (
    <Switch
      checked={account.schedulable}
      disabled={busy || account.status === 'expired'}
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
  now: number
) {
  if (!window) return '-'
  const seconds = Math.max(
    0,
    window.reset_at > 0
      ? window.reset_at - Math.floor(now / 1000)
      : window.reset_after_seconds
  )
  if (seconds <= 0) return '0m'
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
        {resetCountdown(window, now)}
      </span>
    </div>
  )
}

export function AccountUsageCell({ account }: { account: UpstreamAccount }) {
  const { t } = useTranslation()
  const [usage, setUsage] = useState<UpstreamAccountQuotaUsage | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [resetting, setResetting] = useState(false)
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false)
  const [now, setNow] = useState(() => Date.now())
  const supported = account.platform === 'openai' && account.type === 'oauth'
  const windows = useMemo(() => classifyWindows(usage), [usage])
  const credits = usage?.rate_limit_reset_credits?.available_count ?? 0

  useEffect(() => {
    if (!usage) return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [usage])

  const query = useCallback(
    async (force = true, notifyError = true) => {
      setLoading(true)
      setError('')
      try {
        const data = await loadAccountUsage(account.id, {
          force,
          includeCredits: force,
        })
        setUsage(data)
        setNow(Date.now())
      } catch (queryError) {
        const message =
          queryError instanceof Error
            ? queryError.message
            : t('Failed to fetch usage')
        setError(message)
        if (notifyError) toast.error(message)
      } finally {
        setLoading(false)
      }
    },
    [account.id, t]
  )

  useEffect(() => {
    if (!supported) return
    let active = true
    setLoading(true)
    setError('')
    void loadAccountUsage(account.id, {
      force: false,
      includeCredits: false,
    })
      .then((data) => {
        if (!active) return
        setUsage(data)
        setNow(Date.now())
      })
      .catch((queryError) => {
        if (!active) return
        setError(
          queryError instanceof Error
            ? queryError.message
            : t('Failed to fetch usage')
        )
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [account.id, supported, t])

  const reset = async () => {
    setResetting(true)
    try {
      const response = await resetUpstreamAccountQuota(account.id)
      if (!response.success)
        throw new Error(response.message || t('Reset failed'))
      toast.success(t('Quota reset successfully'))
      setResetConfirmOpen(false)
      await query(true, false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Reset failed'))
    } finally {
      setResetting(false)
    }
  }

  if (!supported) {
    return <span className='text-muted-foreground text-xs'>-</span>
  }

  return (
    <>
      <div className='min-w-48 space-y-1.5'>
        {loading && !usage ? (
          <div className='flex flex-col gap-1.5' aria-label={t('Loading...')}>
            <Skeleton className='h-4 w-36' />
            <Skeleton className='h-4 w-36' />
          </div>
        ) : (
          <>
            <UsageWindow label='5h' window={windows.fiveHour} now={now} />
            <UsageWindow label='7d' window={windows.weekly} now={now} />
          </>
        )}
        {error && !usage && (
          <span
            className='text-destructive block max-w-44 truncate text-[10px]'
            title={error}
          >
            {error}
          </span>
        )}
        <div className='flex items-center gap-2 text-[11px]'>
          <button
            type='button'
            className='text-blue-600 hover:underline disabled:opacity-50'
            disabled={loading || resetting}
            onClick={() => void query()}
          >
            {loading && (
              <HugeiconsIcon
                icon={Loading03Icon}
                className='mr-0.5 inline size-3 animate-spin'
              />
            )}
            {!loading && (
              <HugeiconsIcon
                icon={RefreshIcon}
                className='mr-0.5 inline size-3'
              />
            )}
            {t('Query')}
          </button>
          <span className='text-blue-600'>
            {t('{{count}} credits', { count: credits })}
          </span>
          <button
            type='button'
            className='text-amber-600 hover:underline disabled:cursor-not-allowed disabled:opacity-40'
            disabled={!usage || credits <= 0 || loading || resetting}
            onClick={() => setResetConfirmOpen(true)}
          >
            {resetting ? t('Resetting...') : t('Reset')}
          </button>
        </div>
      </div>
      <AlertDialog open={resetConfirmOpen} onOpenChange={setResetConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Reset quota?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This consumes one reset credit and resets the available usage windows for {{name}}.',
                { name: account.name }
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
