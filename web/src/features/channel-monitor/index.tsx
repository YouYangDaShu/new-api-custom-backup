import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { Activity, AlertCircle, RefreshCw } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

import {
  getChannelMonitors,
  type MonitorStat,
  type PublicChannelMonitor,
} from './api'
import {
  buildTimelineSlots,
  AVAILABILITY_WINDOWS,
  DEFAULT_AVAILABILITY_WINDOW,
  getAvailabilityValue,
  getMonitorRefreshInterval,
  HISTORY_SLOTS,
  timelineTickClass,
  type AvailabilityWindow,
  type MonitorHistoryPoint,
} from './timeline'

function isAvailabilityWindow(value: string): value is AvailabilityWindow {
  return AVAILABILITY_WINDOWS.some(({ key }) => key === value)
}

const emptyRows: PublicChannelMonitor[] = []

type MonitorHealth = 'healthy' | 'partial' | 'pending'

function getMonitorHealth(models: Record<string, MonitorStat>): MonitorHealth {
  const stats = Object.values(models)
  if (stats.every((stat) => !stat.status || stat.status === 'unknown')) {
    return 'pending'
  }
  if (stats.every((stat) => stat.status === 'success')) return 'healthy'
  return 'partial'
}

function statusLabel(status: string, latency: number, t: TFunction) {
  if (status === 'success' && latency >= 6000) return t('Slow response')
  if (status === 'success') return t('Available')
  if (status === 'failed') return t('Failed')
  if (status === 'timeout') return t('Timeout')
  return t('Unknown')
}

function monitorHealthLabel(health: MonitorHealth, t: TFunction) {
  if (health === 'healthy') return t('Running normally')
  if (health === 'partial') return t('Partially unavailable')
  return t('Pending detection')
}

function formatAvailability(value: number | undefined) {
  return value === undefined ? '\u2014' : `${value.toFixed(1)}%`
}

function availabilityClass(value: number | undefined) {
  if (value === undefined) return 'text-muted-foreground'
  if (value >= 99) return 'text-success'
  if (value >= 95) return 'text-warning'
  return 'text-destructive'
}

function Timeline(props: {
  history?: MonitorHistoryPoint[]
  refreshIntervalMs: number | false
}) {
  const { t } = useTranslation()
  const slots = buildTimelineSlots(props.history)
  const refreshSeconds =
    props.refreshIntervalMs === false
      ? undefined
      : Math.round(props.refreshIntervalMs / 1000)

  return (
    <div className='border-border/70 mt-4 border-t pt-3'>
      <div className='text-muted-foreground mb-2 flex justify-between gap-3 text-[10px] font-semibold uppercase'>
        <span>{t('Recent {{count}} records', { count: HISTORY_SLOTS })}</span>
        {refreshSeconds === undefined ? null : (
          <span>
            {t('Auto-refreshing every {{seconds}}s', {
              seconds: refreshSeconds,
            })}
          </span>
        )}
      </div>
      <TooltipProvider delay={100}>
        <div
          className='flex h-5 w-full min-w-0 items-end gap-px'
          aria-label={t('Recent status history')}
        >
          {slots.map((point, position) => {
            let tooltip: ReactNode = t('No samples yet')
            let ariaLabel = t('No samples yet')
            if (point) {
              const checkedAt = new Date(
                point.checked_at * 1000
              ).toLocaleString()
              const latency =
                point.latency_ms > 0 ? `${point.latency_ms} ms` : '\u2014'
              const label = statusLabel(point.status, point.latency_ms, t)
              tooltip = (
                <div className='flex flex-col gap-0.5'>
                  <span>{label}</span>
                  <span>{latency}</span>
                  <span>{checkedAt}</span>
                </div>
              )
              ariaLabel = `${label}, ${latency}, ${checkedAt}`
            }

            return (
              // Slot position is the stable identity for this fixed timeline.
              // eslint-disable-next-line react/no-array-index-key
              <Tooltip key={`timeline-slot-${position}`}>
                <TooltipTrigger
                  render={
                    <span
                      aria-label={ariaLabel}
                      className={cn(
                        'block min-w-0 flex-1 rounded-sm transition-opacity hover:opacity-75',
                        timelineTickClass(point)
                      )}
                    />
                  }
                />
                <TooltipContent>{tooltip}</TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
      <div className='text-muted-foreground mt-1 flex justify-between text-[9px] font-semibold uppercase'>
        <span>{t('Past')}</span>
        <span>{t('Now')}</span>
      </div>
    </div>
  )
}

function ModelMonitor(props: {
  model: string
  stat: MonitorStat
  availabilityWindow: AvailabilityWindow
  refreshIntervalMs: number | false
}) {
  const { t } = useTranslation()
  const status = props.stat.status || 'unknown'
  const latency = props.stat.latency_ms
  const unavailable = status === 'failed' || status === 'timeout'
  const availability = AVAILABILITY_WINDOWS.find(
    ({ key }) => key === props.availabilityWindow
  )
  const availabilityValue = getAvailabilityValue(
    props.stat.availability,
    props.availabilityWindow
  )

  return (
    <section className='border-border/70 border-t pt-4 first:border-t-0 first:pt-0'>
      <div className='flex min-w-0 items-center justify-between gap-3'>
        <h3
          className='truncate font-mono text-sm font-semibold'
          title={props.model}
        >
          {props.model}
        </h3>
        <Badge
          variant={unavailable ? 'destructive' : 'secondary'}
          className={cn(
            status === 'success' && 'bg-success/10 text-success',
            status === 'success' &&
              latency >= 6000 &&
              'bg-warning/10 text-warning',
            !unavailable &&
              status !== 'success' &&
              'bg-muted text-muted-foreground'
          )}
        >
          {statusLabel(status, latency, t)}
        </Badge>
      </div>

      <div className='mt-3 grid grid-cols-2 gap-2'>
        <div className='bg-muted/50 rounded-xl border p-3'>
          <span className='text-muted-foreground block text-[11px] font-medium'>
            {t('Latest latency')}
          </span>
          <strong className='mt-1 block text-lg font-semibold tabular-nums'>
            {latency > 0 ? `${latency} ms` : '\u2014'}
          </strong>
        </div>
        <div className='bg-muted/50 rounded-xl border p-3'>
          <span className='text-muted-foreground block text-[11px] font-medium'>
            {t(availability?.availabilityLabelKey ?? '24-hour availability')}
          </span>
          <strong
            className={cn(
              'mt-1 block text-lg font-bold tabular-nums',
              availabilityClass(availabilityValue)
            )}
          >
            {formatAvailability(availabilityValue)}
          </strong>
        </div>
      </div>

      <Timeline
        history={props.stat.history}
        refreshIntervalMs={props.refreshIntervalMs}
      />
    </section>
  )
}

function MonitorCard(props: {
  row: PublicChannelMonitor
  refreshIntervalMs: number | false
}) {
  const { t } = useTranslation()
  const [availabilityWindow, setAvailabilityWindow] =
    useState<AvailabilityWindow>(DEFAULT_AVAILABILITY_WINDOW)
  const health = getMonitorHealth(props.row.models)
  const models = Object.entries(props.row.models).sort(([left], [right]) =>
    left.localeCompare(right)
  )
  const modelNames = models.map(([model]) => model).join(', ')

  return (
    <Card className='gap-5 rounded-2xl shadow-sm'>
      <CardHeader className='gap-2 px-5'>
        <div className='flex min-w-0 items-center gap-3'>
          <span className='bg-muted grid size-9 shrink-0 place-items-center rounded-xl border'>
            <Activity
              className='text-muted-foreground size-4'
              aria-hidden='true'
            />
          </span>
          <div className='min-w-0'>
            <CardTitle className='truncate text-base font-semibold'>
              {props.row.name}
            </CardTitle>
            <CardDescription
              className='mt-0.5 truncate text-xs'
              title={modelNames}
            >
              {props.row.group} ·{' '}
              {t('{{count}} monitored models', { count: models.length })}
              {modelNames ? ` · ${modelNames}` : ''}
            </CardDescription>
          </div>
        </div>
        <CardAction className='flex max-w-full flex-col items-end gap-2'>
          <Badge
            variant={health === 'partial' ? 'destructive' : 'secondary'}
            className={cn(
              health === 'healthy' && 'bg-success/10 text-success',
              health === 'pending' && 'bg-muted text-muted-foreground'
            )}
          >
            {monitorHealthLabel(health, t)}
          </Badge>
          <Tabs
            value={availabilityWindow}
            onValueChange={(value) => {
              if (isAvailabilityWindow(value)) setAvailabilityWindow(value)
            }}
            className='w-36 max-w-full'
          >
            <TabsList
              aria-label={t('Availability period')}
              className='grid w-full grid-cols-3'
            >
              {AVAILABILITY_WINDOWS.map(({ key, labelKey }) => (
                <TabsTrigger key={key} value={key} className='px-1 text-xs'>
                  {t(labelKey)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
        </CardAction>
      </CardHeader>
      <CardContent className='flex flex-col gap-5 px-5'>
        {models.length > 0 ? (
          models.map(([model, stat]) => (
            <ModelMonitor
              key={model}
              model={model}
              stat={stat}
              availabilityWindow={availabilityWindow}
              refreshIntervalMs={props.refreshIntervalMs}
            />
          ))
        ) : (
          <div className='bg-muted/40 text-muted-foreground rounded-xl border border-dashed p-5 text-center text-sm'>
            {t('No data available')}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function MonitorSkeleton() {
  return (
    <div className='grid items-start gap-4 lg:grid-cols-2' aria-hidden='true'>
      {Array.from({ length: 2 }, (_, index) => (
        <Card
          key={`monitor-skeleton-${index}`}
          className='gap-5 rounded-2xl p-5'
        >
          <div className='flex items-center gap-3'>
            <Skeleton className='size-9 rounded-xl' />
            <div className='flex flex-1 flex-col gap-2'>
              <Skeleton className='h-5 w-40' />
              <Skeleton className='h-3 w-56 max-w-full' />
            </div>
          </div>
          <div className='grid grid-cols-2 gap-2'>
            {Array.from({ length: 2 }, (_, metricIndex) => (
              <Skeleton
                key={`metric-skeleton-${metricIndex}`}
                className='h-16'
              />
            ))}
          </div>
          <Skeleton className='h-5 w-full' />
        </Card>
      ))}
    </div>
  )
}

export function ChannelMonitorPage() {
  const { t } = useTranslation()
  const query = useQuery({
    queryKey: ['channel-monitors'],
    queryFn: getChannelMonitors,
    refetchInterval: (currentQuery) =>
      getMonitorRefreshInterval(currentQuery.state.data?.data),
    refetchOnWindowFocus: false,
  })
  const rows = query.data?.data ?? emptyRows
  const refreshIntervalMs = getMonitorRefreshInterval(rows)

  let content: ReactNode
  if (query.isLoading) {
    content = <MonitorSkeleton />
  } else if (query.isError) {
    content = (
      <Alert
        variant='destructive'
        className='min-h-32 content-center px-5 py-4'
      >
        <AlertCircle aria-hidden='true' />
        <AlertTitle>{t('Unable to load channel monitors')}</AlertTitle>
        <AlertDescription>
          {t('Please refresh the page and try again')}
        </AlertDescription>
      </Alert>
    )
  } else if (rows.length === 0) {
    content = (
      <Empty className='bg-card min-h-72 border'>
        <EmptyHeader>
          <EmptyMedia variant='icon'>
            <Activity aria-hidden='true' />
          </EmptyMedia>
          <EmptyTitle>{t('No channel monitoring data')}</EmptyTitle>
          <EmptyDescription>
            {t('Monitoring results will appear here when available')}
          </EmptyDescription>
        </EmptyHeader>
      </Empty>
    )
  } else {
    content = (
      <div className='grid items-start gap-4 lg:grid-cols-2'>
        {rows.map((row) => (
          <MonitorCard
            key={`${row.name}-${row.group}`}
            row={row}
            refreshIntervalMs={refreshIntervalMs}
          />
        ))}
      </div>
    )
  }

  return (
    <main className='mx-auto flex w-full max-w-7xl flex-col gap-6 overflow-x-hidden p-4 sm:p-6'>
      <header className='flex flex-wrap items-end justify-between gap-4'>
        <div>
          <h1 className='text-2xl font-semibold'>{t('Channel monitoring')}</h1>
          <p className='text-muted-foreground mt-1 text-sm'>
            {t('View real-time model availability by group')}
          </p>
        </div>
        <Button
          variant='outline'
          onClick={() => void query.refetch()}
          disabled={query.isFetching}
        >
          <RefreshCw
            data-icon='inline-start'
            className={cn(query.isFetching && 'animate-spin')}
          />
          {t('Refresh')}
        </Button>
      </header>

      {content}
    </main>
  )
}
