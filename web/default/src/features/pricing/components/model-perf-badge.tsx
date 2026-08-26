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
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import { getSuccessRateDotClass } from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = {
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_rates?: number[]
  /** Aligned with recent_success_rates: request count per sample bucket. */
  recent_request_counts?: number[]
}

export interface ModelPerfBadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  perf: ModelPerfBadgeData | undefined
}

function formatCompactNumber(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  return value > 1 ? String(Math.round(value)) : value.toFixed(1)
}

function formatCompactLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  if (ms >= 1_000) return `${formatCompactNumber(ms / 1_000)}s`
  return `${formatCompactNumber(ms)}ms`
}

function formatCompactThroughput(tps: number): string {
  if (!Number.isFinite(tps) || tps <= 0) return '—'
  if (tps >= 1_000) return `${formatCompactNumber(tps / 1_000)}Kt`
  return `${formatCompactNumber(tps)}t`
}

/** Height classes for request-volume bars — moderate range only (not extreme). */
const HEIGHT_BY_LEVEL = [
  'h-2', // low volume
  'h-2.5', // medium
  'h-3', // high
] as const

function heightClassForCount(count: number | null, maxCount: number): string {
  if (count == null || count <= 0 || maxCount <= 0) return HEIGHT_BY_LEVEL[0]
  const ratio = count / maxCount
  // Keep differences mild: only 3 steps between shortest and tallest
  if (ratio >= 0.66) return HEIGHT_BY_LEVEL[2]
  if (ratio >= 0.33) return HEIGHT_BY_LEVEL[1]
  return HEIGHT_BY_LEVEL[0]
}

export const ModelPerfBadge = memo(function ModelPerfBadge(
  props: ModelPerfBadgeProps
) {
  const { t } = useTranslation()

  if (!props.perf) {
    return null
  }

  const { avg_latency_ms, avg_tps, success_rate } = props.perf

  // Up to 10 recent samples. Color = success/fail; height = relative request volume.
  const STATUS_BAR_COUNT = 10
  const recentRates =
    props.perf.recent_success_rates?.filter((rate) => Number.isFinite(rate)) ??
    []
  const recentCountsRaw = props.perf.recent_request_counts ?? []

  const statusRates =
    recentRates.length > 0
      ? recentRates.slice(-STATUS_BAR_COUNT)
      : Number.isFinite(success_rate)
        ? [success_rate]
        : []

  // Align counts with rates (same window from the end). Missing counts → treat as equal.
  const alignedCounts =
    statusRates.length === 0
      ? []
      : (() => {
          const sliced = recentCountsRaw.slice(-statusRates.length)
          if (sliced.length === statusRates.length) return sliced
          // Old backend without counts: equal height for all filled bars
          return statusRates.map(() => 1)
        })()

  const statusBars = [
    ...Array(Math.max(0, STATUS_BAR_COUNT - statusRates.length)).fill(null),
    ...statusRates,
  ].slice(-STATUS_BAR_COUNT) as Array<number | null>

  const statusCounts = [
    ...Array(Math.max(0, STATUS_BAR_COUNT - alignedCounts.length)).fill(null),
    ...alignedCounts,
  ].slice(-STATUS_BAR_COUNT) as Array<number | null>

  const maxCount = Math.max(
    1,
    ...statusCounts.map((c) => (c != null && c > 0 ? c : 0))
  )

  return (
    <div
      className={cn(
        'hidden w-[168px] grid-cols-[38px_48px_66px] gap-x-2 text-right tabular-nums min-[460px]:grid',
        props.className
      )}
    >
      <div title={t('Average latency')} className='min-w-0'>
        <div className='text-muted-foreground/55 text-[10px] leading-4'>
          {t('Latency short')}
        </div>
        <div className='text-muted-foreground/80 font-mono text-xs leading-4 whitespace-nowrap'>
          {formatCompactLatency(avg_latency_ms)}
        </div>
      </div>
      <div title={t('Throughput')} className='min-w-0'>
        <div className='text-muted-foreground/55 truncate text-[10px] leading-4'>
          {t('Throughput short')}
        </div>
        <div className='text-muted-foreground/80 font-mono text-xs leading-4 whitespace-nowrap'>
          {formatCompactThroughput(avg_tps)}
        </div>
      </div>
      <div
        title={`${t('Success rate')}: ${success_rate.toFixed(1)}%`}
        className='min-w-0'
      >
        <div className='text-muted-foreground/55 truncate text-[10px] leading-4'>
          {t('Status short')}
        </div>
        <div className='flex h-4 items-end justify-end gap-px'>
          {statusBars.map((rate, index) => {
            const count = statusCounts[index]
            return (
              <span
                key={`${index}-${rate ?? 'empty'}-${count ?? 0}`}
                title={
                  rate == null
                    ? undefined
                    : `${rate.toFixed(1)}% · ${count ?? 0} req`
                }
                className={cn(
                  'w-[3px] rounded-full',
                  rate == null
                    ? 'h-2 bg-muted-foreground/10'
                    : cn(
                        heightClassForCount(count, maxCount),
                        getSuccessRateDotClass(rate)
                      )
                )}
              />
            )
          })}
        </div>
      </div>
    </div>
  )
})
