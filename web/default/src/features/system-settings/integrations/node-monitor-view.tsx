import {
  Activity01Icon,
  CpuIcon,
  HardDriveIcon,
  RamMemoryIcon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from '@/components/ui/chart'
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemTitle,
} from '@/components/ui/item'
import { Progress } from '@/components/ui/progress'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import type { RoutingNode } from '@/features/node-routing/types'

const ONLINE_SECONDS = 30
const DELAYED_SECONDS = 90

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const index = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1
  )
  return `${(value / 1024 ** index).toFixed(index >= 3 ? 1 : 0)} ${units[index]}`
}

function formatDuration(value: number): string {
  const seconds = Math.max(0, Math.floor(value))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h`
  if (hours > 0) return `${hours}h ${minutes}m`
  return `${minutes}m`
}

function formatNetworkRate(value: number): string {
  return `${formatBytes(value)}/s`
}

function formatNetworkAxisRate(value: number): string {
  if (value >= 1024 ** 2) return `${Math.round(value / 1024 ** 2)} MB`
  if (value >= 1024) return `${Math.round(value / 1024)} KB`
  return `${Math.round(value)} B`
}

type MonitorState =
  | 'disabled'
  | 'unconfigured'
  | 'waiting'
  | 'online'
  | 'delayed'
  | 'offline'

function getRoutingNodeMonitorState(node: RoutingNode): MonitorState {
  if (!node.monitor_enabled) return 'disabled'
  if (!node.monitor_configured) return 'unconfigured'
  if (!node.monitor_status?.reported_at) return 'waiting'
  const age = Date.now() / 1000 - node.monitor_status.reported_at
  if (age <= ONLINE_SECONDS) return 'online'
  if (age <= DELAYED_SECONDS) return 'delayed'
  return 'offline'
}

export function RoutingNodeMonitorBadge({ node }: { node: RoutingNode }) {
  const { t } = useTranslation()
  const state = getRoutingNodeMonitorState(node)
  const config = {
    disabled: { label: t('Monitoring disabled'), variant: 'secondary' },
    unconfigured: { label: t('Not configured'), variant: 'outline' },
    waiting: { label: t('Waiting for report'), variant: 'secondary' },
    online: { label: t('Online'), variant: 'success' },
    delayed: { label: t('Report delayed'), variant: 'warning' },
    offline: { label: t('Offline'), variant: 'destructive' },
  }[state] as {
    label: string
    variant: 'secondary' | 'outline' | 'success' | 'warning' | 'destructive'
  }

  return <Badge variant={config.variant}>{config.label}</Badge>
}

function monitorHealthLabel(node: RoutingNode, t: (key: string) => string) {
  const status = node.monitor_status
  if (!status) return '-'
  const pressure = Math.max(
    status.load_percent,
    status.cpu_usage,
    status.memory_percent,
    status.disk_percent
  )
  if (pressure >= 90) return t('Critical')
  if (pressure >= 70) return t('Busy')
  return t('Smooth')
}

function ResourceUsageRing({
  percent,
  available,
}: {
  percent: number
  available: boolean
}) {
  const safePercent = Math.min(100, Math.max(0, percent))
  return (
    <div className='relative size-16 shrink-0'>
      <svg
        viewBox='0 0 64 64'
        className='size-full -rotate-90'
        aria-hidden='true'
      >
        <circle
          cx='32'
          cy='32'
          r='26'
          fill='none'
          stroke='var(--muted)'
          strokeWidth='6'
          pathLength='100'
        />
        <circle
          cx='32'
          cy='32'
          r='26'
          fill='none'
          stroke='var(--chart-2)'
          strokeWidth='6'
          strokeLinecap='round'
          strokeDasharray={`${available ? safePercent : 0} 100`}
          pathLength='100'
        />
      </svg>
      <div className='absolute inset-0 flex items-center justify-center font-semibold tabular-nums'>
        {available ? (
          <>
            <span className='text-lg'>{Math.round(safePercent)}</span>
            <span className='text-chart-2 text-[10px]'>%</span>
          </>
        ) : (
          <span className='text-muted-foreground'>-</span>
        )}
      </div>
    </div>
  )
}

export function RoutingNodeResourceOverview({ node }: { node: RoutingNode }) {
  const { t } = useTranslation()
  const status = node.monitor_status
  const resources = [
    {
      label: t('Load'),
      value: status ? monitorHealthLabel(node, t) : '-',
      percent: status?.load_percent ?? 0,
    },
    {
      label: t('CPU'),
      value: status ? t('{{count}} cores', { count: status.cpu_cores }) : '-',
      percent: status?.cpu_usage ?? 0,
    },
    {
      label: t('Memory'),
      value: status
        ? `${formatBytes(status.memory_used)} / ${formatBytes(status.memory_total)}`
        : '-',
      percent: status?.memory_percent ?? 0,
    },
    {
      label: t('Disk'),
      value: status
        ? `${formatBytes(status.disk_used)} / ${formatBytes(status.disk_total)}`
        : '-',
      percent: status?.disk_percent ?? 0,
    },
  ]

  return (
    <div className='grid grid-cols-2 gap-2'>
      {resources.map((resource) => (
        <Item
          key={resource.label}
          variant='muted'
          size='sm'
          className='bg-background/55 min-h-24 flex-nowrap justify-between'
        >
          <ItemContent className='min-w-0'>
            <ItemDescription>{resource.label}</ItemDescription>
            <ItemTitle className='w-full text-base tabular-nums'>
              {resource.value}
            </ItemTitle>
          </ItemContent>
          <ResourceUsageRing
            percent={resource.percent}
            available={Boolean(status)}
          />
        </Item>
      ))}
    </div>
  )
}

export function RoutingNodeNetworkTraffic({ node }: { node: RoutingNode }) {
  const { t } = useTranslation()
  const status = node.monitor_status
  const samples = status?.network_samples ?? []
  const chartConfig = {
    upload_bps: {
      label: t('Upload'),
      color: 'var(--chart-2)',
    },
    download_bps: {
      label: t('Download'),
      color: 'var(--chart-4)',
    },
  } satisfies ChartConfig

  const summary = [
    {
      label: t('Upload'),
      value: formatNetworkRate(status?.network_upload_bps ?? 0),
    },
    {
      label: t('Download'),
      value: formatNetworkRate(status?.network_download_bps ?? 0),
    },
    {
      label: t('Total sent'),
      value: formatBytes(status?.network_bytes_sent ?? 0),
    },
    {
      label: t('Total received'),
      value: formatBytes(status?.network_bytes_received ?? 0),
    },
  ]

  return (
    <div className='flex flex-col gap-3'>
      <div className='flex items-center justify-between gap-3'>
        <span className='font-medium'>{t('Traffic')}</span>
        <div className='text-muted-foreground flex items-center gap-3 text-xs'>
          <span className='flex items-center gap-1.5'>
            <span className='bg-chart-2 size-2 rounded-full' />
            {t('Upload')}
          </span>
          <span className='flex items-center gap-1.5'>
            <span className='bg-chart-4 size-2 rounded-full' />
            {t('Download')}
          </span>
        </div>
      </div>

      <div className='grid grid-cols-2 gap-2'>
        {summary.map((item) => (
          <Item key={item.label} variant='muted' size='xs'>
            <ItemContent>
              <ItemDescription>{item.label}</ItemDescription>
              <ItemTitle className='font-mono tabular-nums'>
                {item.value}
              </ItemTitle>
            </ItemContent>
          </Item>
        ))}
      </div>

      {samples.length > 1 ? (
        <ChartContainer
          config={chartConfig}
          className='aspect-auto h-36 w-full'
          initialDimension={{ width: 320, height: 144 }}
        >
          <AreaChart
            accessibilityLayer
            data={samples}
            margin={{ top: 8, right: 4, bottom: 0, left: 0 }}
          >
            <defs>
              <linearGradient
                id={`upload-${node.id}`}
                x1='0'
                y1='0'
                x2='0'
                y2='1'
              >
                <stop
                  offset='5%'
                  stopColor='var(--color-upload_bps)'
                  stopOpacity={0.3}
                />
                <stop
                  offset='95%'
                  stopColor='var(--color-upload_bps)'
                  stopOpacity={0.02}
                />
              </linearGradient>
              <linearGradient
                id={`download-${node.id}`}
                x1='0'
                y1='0'
                x2='0'
                y2='1'
              >
                <stop
                  offset='5%'
                  stopColor='var(--color-download_bps)'
                  stopOpacity={0.25}
                />
                <stop
                  offset='95%'
                  stopColor='var(--color-download_bps)'
                  stopOpacity={0.02}
                />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} strokeDasharray='4 4' />
            <XAxis
              dataKey='reported_at'
              axisLine={false}
              tickLine={false}
              minTickGap={32}
              tickFormatter={(value) =>
                new Date(Number(value) * 1000).toLocaleTimeString([], {
                  hour: '2-digit',
                  minute: '2-digit',
                })
              }
            />
            <YAxis
              axisLine={false}
              tickLine={false}
              width={42}
              tickFormatter={formatNetworkAxisRate}
            />
            <ChartTooltip
              cursor={{ strokeDasharray: '4 4' }}
              content={
                <ChartTooltipContent
                  labelFormatter={(value) =>
                    new Date(Number(value) * 1000).toLocaleTimeString()
                  }
                  formatter={(value, name) => (
                    <div className='flex w-full items-center gap-2'>
                      <span
                        className='size-2 rounded-full'
                        style={{
                          backgroundColor:
                            name === 'upload_bps'
                              ? 'var(--chart-2)'
                              : 'var(--chart-4)',
                        }}
                      />
                      <span className='text-muted-foreground'>
                        {chartConfig[name as keyof typeof chartConfig]?.label}
                      </span>
                      <span className='ml-auto font-mono font-medium tabular-nums'>
                        {formatNetworkRate(Number(value))}
                      </span>
                    </div>
                  )}
                />
              }
            />
            <Area
              dataKey='download_bps'
              type='monotone'
              stroke='var(--color-download_bps)'
              fill={`url(#download-${node.id})`}
              strokeWidth={2}
              isAnimationActive={false}
            />
            <Area
              dataKey='upload_bps'
              type='monotone'
              stroke='var(--color-upload_bps)'
              fill={`url(#upload-${node.id})`}
              strokeWidth={2}
              isAnimationActive={false}
            />
          </AreaChart>
        </ChartContainer>
      ) : (
        <div className='text-muted-foreground flex h-36 items-center justify-center rounded-lg border border-dashed text-xs'>
          {t('Collecting traffic data')}
        </div>
      )}
    </div>
  )
}

function MonitorMetricCard({
  title,
  description,
  percent,
  icon,
}: {
  title: string
  description: string
  percent: number
  icon: typeof Activity01Icon
}) {
  return (
    <Card size='sm'>
      <CardHeader>
        <CardTitle className='flex items-center gap-2'>
          <HugeiconsIcon icon={icon} strokeWidth={2} />
          {title}
        </CardTitle>
        <CardDescription>{description}</CardDescription>
      </CardHeader>
      <CardContent className='flex flex-col gap-3'>
        <div className='text-3xl font-semibold tabular-nums'>
          {Math.round(percent)}%
        </div>
        <Progress value={Math.min(100, Math.max(0, percent))} />
      </CardContent>
    </Card>
  )
}

export function RoutingNodeMonitorSheet({
  node,
  open,
  onOpenChange,
}: {
  node: RoutingNode | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const status = node?.monitor_status

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='w-full gap-0 p-0 sm:max-w-2xl'>
        <SheetHeader className='p-5'>
          <SheetTitle>{t('Node monitoring')}</SheetTitle>
          <SheetDescription>
            {node ? `${node.name} · ${node.origin}` : ''}
          </SheetDescription>
        </SheetHeader>
        <Separator />
        <ScrollArea className='min-h-0 flex-1'>
          <div className='flex flex-col gap-5 p-5'>
            {node && (
              <div className='flex flex-wrap items-center justify-between gap-3'>
                <RoutingNodeMonitorBadge node={node} />
                {status?.reported_at ? (
                  <span className='text-muted-foreground text-sm'>
                    {t('Last report: {{time}}', {
                      time: new Date(
                        status.reported_at * 1000
                      ).toLocaleString(),
                    })}
                  </span>
                ) : null}
              </div>
            )}

            {status ? (
              <div className='grid gap-3 sm:grid-cols-2'>
                <MonitorMetricCard
                  title={t('System load')}
                  description={t('1-minute load: {{value}}', {
                    value: status.load_one.toFixed(2),
                  })}
                  percent={status.load_percent}
                  icon={Activity01Icon}
                />
                <MonitorMetricCard
                  title={t('CPU')}
                  description={t('{{count}} cores', {
                    count: status.cpu_cores,
                  })}
                  percent={status.cpu_usage}
                  icon={CpuIcon}
                />
                <MonitorMetricCard
                  title={t('Memory')}
                  description={`${formatBytes(status.memory_used)} / ${formatBytes(status.memory_total)}`}
                  percent={status.memory_percent}
                  icon={RamMemoryIcon}
                />
                <MonitorMetricCard
                  title={t('Disk')}
                  description={`${formatBytes(status.disk_used)} / ${formatBytes(status.disk_total)}`}
                  percent={status.disk_percent}
                  icon={HardDriveIcon}
                />
              </div>
            ) : (
              <div className='text-muted-foreground py-12 text-center text-sm'>
                {t('No monitoring data has been reported yet')}
              </div>
            )}

            {status && (
              <dl className='grid gap-3 text-sm sm:grid-cols-2'>
                <div className='bg-muted/50 flex justify-between gap-4 rounded-md p-3'>
                  <dt className='text-muted-foreground'>{t('Host name')}</dt>
                  <dd className='font-mono'>{status.node_name || '-'}</dd>
                </div>
                <div className='bg-muted/50 flex justify-between gap-4 rounded-md p-3'>
                  <dt className='text-muted-foreground'>{t('Uptime')}</dt>
                  <dd>{formatDuration(status.uptime_seconds)}</dd>
                </div>
                <div className='bg-muted/50 flex justify-between gap-4 rounded-md p-3'>
                  <dt className='text-muted-foreground'>
                    {t('Application version')}
                  </dt>
                  <dd className='font-mono'>{status.app_version || '-'}</dd>
                </div>
                <div className='bg-muted/50 flex justify-between gap-4 rounded-md p-3'>
                  <dt className='text-muted-foreground'>{t('Node key')}</dt>
                  <dd className='font-mono'>{node?.key}</dd>
                </div>
              </dl>
            )}
          </div>
        </ScrollArea>
      </SheetContent>
    </Sheet>
  )
}
