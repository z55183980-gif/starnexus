import { useEffect, useMemo, useState } from 'react'
import {
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
import { Switch } from '@/components/ui/switch'
import { getUpstreamAccountQuota, resetUpstreamAccountQuota } from './api'
import type {
  UpstreamAccount,
  UpstreamAccountQuotaUsage,
  UpstreamAccountRateLimitWindow,
} from './types'

type Translate = (key: string, options?: Record<string, unknown>) => string

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
  const [resetting, setResetting] = useState(false)
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false)
  const [now, setNow] = useState(Date.now())
  const supported = account.platform === 'openai' && account.type === 'oauth'
  const windows = useMemo(() => classifyWindows(usage), [usage])
  const credits = usage?.rate_limit_reset_credits?.available_count ?? 0

  useEffect(() => {
    if (!usage) return
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [usage])

  const query = async () => {
    setLoading(true)
    try {
      const response = await getUpstreamAccountQuota(account.id)
      if (!response.success || !response.data) {
        throw new Error(response.message || t('Failed to fetch usage'))
      }
      setUsage(response.data)
      setNow(Date.now())
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to fetch usage')
      )
    } finally {
      setLoading(false)
    }
  }

  const reset = async () => {
    setResetting(true)
    try {
      const response = await resetUpstreamAccountQuota(account.id)
      if (!response.success)
        throw new Error(response.message || t('Reset failed'))
      toast.success(t('Quota reset successfully'))
      setResetConfirmOpen(false)
      await query()
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
        <UsageWindow label='5h' window={windows.fiveHour} now={now} />
        <UsageWindow label='7d' window={windows.weekly} now={now} />
        <div className='flex items-center gap-2 text-[11px]'>
          <button
            type='button'
            className='text-blue-600 hover:underline disabled:opacity-50'
            disabled={loading || resetting}
            onClick={query}
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
