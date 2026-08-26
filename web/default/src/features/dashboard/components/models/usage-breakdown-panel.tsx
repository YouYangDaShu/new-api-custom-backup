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
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { getUsageBreakdown, getUserQuotaDates } from '@/features/dashboard/api'
import {
  buildQueryParams,
  getDashboardChartColors,
  getDefaultDays,
} from '@/features/dashboard/lib'
import type {
  DashboardFilters,
  QuotaDataItem,
  UsageBreakdown,
  UsageModelStat,
} from '@/features/dashboard/types'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatCompactNumber,
  formatNumber,
  formatQuota,
} from '@/lib/format'
import { computeTimeRange } from '@/lib/time'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

interface UsageBreakdownPanelProps {
  filters?: DashboardFilters
  onDataUpdate?: (data: QuotaDataItem[], loading: boolean) => void
}

function emptyBreakdown(): UsageBreakdown {
  return {
    request_count: 0,
    quota: 0,
    prompt_tokens: 0,
    completion_tokens: 0,
    cache_read_tokens: 0,
    cache_create_tokens: 0,
    total_tokens: 0,
    cache_rate: 0,
    models: [],
  }
}

/** Fallback when new /api/data/usage is not deployed yet: use quota_data. */
function breakdownFromQuotaData(
  data: QuotaDataItem[],
  topN = 8
): UsageBreakdown {
  const byModel = new Map<
    string,
    { count: number; quota: number; tokens: number }
  >()
  let requestCount = 0
  let quota = 0
  let totalTokens = 0

  for (const item of data) {
    const count = Number(item.count) || 0
    const q = Number(item.quota) || 0
    const tokens = Number(item.token_used) || 0
    requestCount += count
    quota += q
    totalTokens += tokens
    const name = item.model_name || '(unknown)'
    const cur = byModel.get(name) || { count: 0, quota: 0, tokens: 0 }
    cur.count += count
    cur.quota += q
    cur.tokens += tokens
    byModel.set(name, cur)
  }

  const models: UsageModelStat[] = Array.from(byModel.entries())
    .map(([model_name, v]) => ({
      model_name,
      count: v.count,
      quota: v.quota,
      prompt_tokens: 0,
      completion_tokens: 0,
      cache_read_tokens: 0,
      cache_create_tokens: 0,
      total_tokens: v.tokens,
      cache_rate: 0,
    }))
    .sort((a, b) => b.count - a.count)
    .slice(0, topN)

  return {
    request_count: requestCount,
    quota,
    prompt_tokens: 0,
    completion_tokens: 0,
    cache_read_tokens: 0,
    cache_create_tokens: 0,
    total_tokens: totalTokens,
    cache_rate: 0,
    models,
  }
}

function formatTokenValue(value: number, locale: Intl.LocalesArgument) {
  if (!value) return '0'
  if (value >= 1000) return formatCompactNumber(value, locale)
  return formatNumber(value, locale)
}

function formatCacheRate(rate: number) {
  if (!rate || Number.isNaN(rate)) return '0%'
  return `${rate.toFixed(rate >= 10 ? 0 : 1)}%`
}

/**
 * Same ordinal color assignment as model trend/pie charts:
 * alphabetically sorted model names → getDashboardChartColors.
 * Must use the FULL model set from chart data (not only Top N),
 * otherwise color indices diverge from the waveform chart.
 */
function buildModelColorMap(modelNames: string[]): Map<string, string> {
  const unique = Array.from(new Set(modelNames.filter(Boolean))).sort()
  const colors = getDashboardChartColors(Math.max(unique.length, 1))
  const map = new Map<string, string>()
  unique.forEach((name, i) => {
    map.set(name, colors[i % colors.length] || colors[0] || '#3b82f6')
  })
  return map
}

export function UsageBreakdownPanel(props: UsageBreakdownPanelProps) {
  const { t, i18n } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const isAdmin = !!(user?.role && user.role >= 10)
  const [data, setData] = useState<UsageBreakdown | null>(null)
  const [chartModelNames, setChartModelNames] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)
  const [usingFallback, setUsingFallback] = useState(false)
  const { filters, onDataUpdate } = props

  useEffect(() => {
    const abortController = new AbortController()
    setLoading(true)
    setError(false)
    setUsingFallback(false)
    setChartModelNames([])
    onDataUpdate?.([], true)

    const timeRange = computeTimeRange(
      getDefaultDays(filters?.time_granularity),
      filters?.start_timestamp,
      filters?.end_timestamp
    )
    const params = {
      ...buildQueryParams(timeRange, filters),
      top_n: 8,
    }

    const load = async () => {
      // Always load time-series quota_data for the charts below.
      // Do NOT synthesize fake points from Top-N model rows (that would break the trend chart).
      let quotaRows: QuotaDataItem[] = []
      try {
        const quotaRes = await getUserQuotaDates(params, isAdmin)
        if (abortController.signal.aborted) return
        quotaRows = quotaRes?.data || []
        onDataUpdate?.(quotaRows, false)
        setChartModelNames(
          Array.from(
            new Set(
              quotaRows
                .map((r) => r.model_name || '')
                .filter((name) => name.length > 0)
            )
          )
        )
      } catch {
        if (abortController.signal.aborted) return
        onDataUpdate?.([], false)
      }

      // Prefer full usage breakdown (input/output/cache). Fall back to quota_data.
      try {
        const res = await getUsageBreakdown(params, isAdmin)
        if (abortController.signal.aborted) return
        if (res?.success && res.data) {
          setData(res.data)
          setUsingFallback(false)
          setError(false)
          return
        }
        throw new Error(res?.message || 'usage breakdown unavailable')
      } catch {
        if (abortController.signal.aborted) return
        try {
          setData(breakdownFromQuotaData(quotaRows, 8))
          setUsingFallback(true)
          setError(false)
        } catch {
          setData(null)
          setError(true)
        }
      } finally {
        if (!abortController.signal.aborted) setLoading(false)
      }
    }

    void load()
    return () => abortController.abort()
  }, [filters, isAdmin, onDataUpdate])

  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const summary = data || emptyBreakdown()

  const scopeHint = isAdmin
    ? filters?.username
      ? t('Filtered user usage')
      : t('Whole site usage')
    : t('Your usage')

  const cards = useMemo(
    () => [
      {
        key: 'requests',
        title: t('Request Count'),
        value: formatNumber(summary.request_count, locale),
        full: formatNumber(summary.request_count, locale),
      },
      {
        key: 'quota',
        title: t('Consumed Quota'),
        value: formatQuota(summary.quota),
        full: formatQuota(summary.quota),
      },
      {
        key: 'prompt',
        title: t('Input Tokens'),
        value: formatTokenValue(summary.prompt_tokens, locale),
        full: formatNumber(summary.prompt_tokens, locale),
      },
      {
        key: 'completion',
        title: t('Output Tokens'),
        value: formatTokenValue(summary.completion_tokens, locale),
        full: formatNumber(summary.completion_tokens, locale),
      },
      {
        key: 'cache_read',
        title: t('Cache Read Tokens'),
        value: formatTokenValue(summary.cache_read_tokens, locale),
        full: formatNumber(summary.cache_read_tokens, locale),
      },
      {
        key: 'cache_create',
        title: t('Cache Create Tokens'),
        value: formatTokenValue(summary.cache_create_tokens, locale),
        full: formatNumber(summary.cache_create_tokens, locale),
      },
      {
        key: 'cache_rate',
        title: t('Cache Rate'),
        value: formatCacheRate(summary.cache_rate),
        full: formatCacheRate(summary.cache_rate),
      },
    ],
    [summary, t, locale]
  )

  // Match chart colors: use full model set from quota_data when available.
  const modelColorMap = useMemo(() => {
    const names =
      chartModelNames.length > 0
        ? chartModelNames
        : summary.models.map((m) => m.model_name)
    return buildModelColorMap(names)
  }, [chartModelNames, summary.models])

  const maxModelCount = useMemo(
    () => Math.max(1, ...summary.models.map((m) => m.count || 0)),
    [summary.models]
  )

  return (
    // Mobile: stack. Desktop (lg+): summary left, model distribution right.
    <div className='grid grid-cols-1 gap-3 sm:gap-4 lg:grid-cols-2 lg:items-start xl:grid-cols-[minmax(300px,0.95fr)_minmax(0,1.15fr)]'>
      {/* Left: title + 7 independent metric cards; sticky so it stays while right scrolls */}
      <div className='min-w-0 space-y-2.5 sm:space-y-3 lg:sticky lg:top-3 lg:self-start'>
        <div className='flex items-center justify-between gap-2 px-0.5'>
          <div className='text-sm font-medium'>{t('Usage Summary')}</div>
          <div className='text-muted-foreground text-xs'>{scopeHint}</div>
        </div>
        <div className='grid grid-cols-2 gap-2 sm:gap-2.5'>
          {cards.map((card) => (
            <div
              key={card.key}
              className='bg-card min-w-0 overflow-hidden rounded-lg border px-3 py-2.5 sm:px-3.5 sm:py-3'
            >
              <div className='text-muted-foreground truncate text-[11px] font-medium tracking-wider uppercase'>
                {card.title}
              </div>
              {loading ? (
                <Skeleton className='mt-2 h-7 w-16' />
              ) : error ? (
                <div className='text-muted-foreground mt-1.5 font-mono text-lg font-bold'>
                  --
                </div>
              ) : (
                <div
                  className='text-foreground mt-1.5 max-w-full truncate font-mono text-lg font-bold tracking-tight tabular-nums sm:text-xl'
                  title={card.full}
                >
                  {card.value}
                </div>
              )}
            </div>
          ))}
        </div>
        {usingFallback && !loading && !error && (
          <div className='text-muted-foreground px-0.5 text-xs'>
            {t(
              'Input/output/cache details require the new backend. Showing request/quota/total tokens for now.'
            )}
          </div>
        )}
      </div>

      {/* Right: model distribution.
          Desktop: cap height + internal scroll.
          Mobile: no internal scroll so page can keep scrolling to charts below. */}
      <div className='flex min-h-0 min-w-0 flex-col overflow-hidden rounded-lg border lg:max-h-[min(22rem,calc(100vh-12rem))]'>
        <div className='shrink-0 border-b px-3 py-2 sm:px-4'>
          <div className='text-sm font-medium'>
            {t('Model Usage Distribution')}
          </div>
          <div className='text-muted-foreground mt-0.5 text-xs'>
            {t('Top active models in selected time range')}
          </div>
        </div>
        <div className='min-h-0 divide-y lg:overflow-y-auto lg:overscroll-contain'>
          {loading ? (
            Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className='space-y-2 px-3 py-2.5 sm:px-4'>
                <Skeleton className='h-4 w-56' />
                <Skeleton className='h-1.5 w-full' />
                <Skeleton className='h-3 w-full max-w-md' />
              </div>
            ))
          ) : error ? (
            <div className='text-muted-foreground px-3 py-6 text-center text-sm sm:px-4'>
              {t('Failed to load usage data')}
            </div>
          ) : summary.models.length === 0 ? (
            <div className='text-muted-foreground px-3 py-6 text-center text-sm sm:px-4'>
              {t('No usage data in selected time range')}
            </div>
          ) : (
            summary.models.map((m) => {
              const color =
                modelColorMap.get(m.model_name) || 'hsl(217, 91%, 60%)'
              const barPct = Math.max(
                2,
                Math.round(((m.count || 0) / maxModelCount) * 100)
              )
              return (
                <div
                  key={m.model_name}
                  className='flex flex-col gap-1 px-3 py-2.5 sm:flex-row sm:items-start sm:justify-between sm:gap-4 sm:px-4 sm:py-2.5'
                >
                  <div className='min-w-0 flex-1'>
                    <div className='flex flex-wrap items-center gap-2'>
                      <span
                        className='size-2.5 shrink-0 rounded-full'
                        style={{ backgroundColor: color }}
                        aria-hidden
                      />
                      <span className='truncate text-sm font-medium'>
                        {m.model_name}
                      </span>
                      <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[11px] tabular-nums'>
                        {m.count}
                      </span>
                    </div>
                    {/* Thin bar — same color as model trend/pie chart series */}
                    <div className='bg-muted mt-1.5 h-1.5 w-full overflow-hidden rounded-full'>
                      <div
                        className='h-full rounded-full transition-[width]'
                        style={{
                          width: `${barPct}%`,
                          backgroundColor: color,
                        }}
                      />
                    </div>
                    <div className='text-muted-foreground mt-1.5 flex flex-wrap gap-x-3 gap-y-0.5 text-xs'>
                      <span>
                        Token{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(m.total_tokens, locale)}
                        </span>
                      </span>
                      <span>
                        {t('Input')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(m.prompt_tokens, locale)}
                        </span>
                      </span>
                      <span>
                        {t('Output')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(m.completion_tokens, locale)}
                        </span>
                      </span>
                    </div>
                    <div className='text-muted-foreground mt-0.5 flex flex-wrap gap-x-3 gap-y-0.5 text-xs'>
                      <span>
                        {t('Cache Tokens')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(
                            m.cache_read_tokens + m.cache_create_tokens,
                            locale
                          )}
                        </span>
                      </span>
                      <span>
                        {t('Cache Read')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(m.cache_read_tokens, locale)}
                        </span>
                      </span>
                      <span>
                        {t('Cache Create')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatTokenValue(m.cache_create_tokens, locale)}
                        </span>
                      </span>
                      <span>
                        {t('Cache Rate')}{' '}
                        <span className='text-foreground font-mono tabular-nums'>
                          {formatCacheRate(m.cache_rate)}
                        </span>
                      </span>
                    </div>
                  </div>
                  <div
                    className={cn(
                      'text-muted-foreground shrink-0 font-mono text-xs tabular-nums sm:pt-0.5 sm:text-right sm:text-sm'
                    )}
                    title={formatQuota(m.quota)}
                  >
                    {formatQuota(m.quota)}
                  </div>
                </div>
              )
            })
          )}
        </div>
      </div>
    </div>
  )
}
