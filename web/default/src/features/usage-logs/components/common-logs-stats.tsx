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
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { useIsAdmin } from '@/hooks/use-admin'
import {
  formatCompactNumber,
  formatLogQuota,
  formatNumber,
  formatPercent,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import { getLogStats, getUserLogStats } from '../api'
import { DEFAULT_LOG_STATS } from '../constants'
import { buildApiParams } from '../lib/utils'
import { useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')

function StatBadge(props: {
  label: string
  value: string | number
  accent: string
  sub?: string
}) {
  return (
    <span className='border-border/60 bg-muted/25 inline-flex min-h-7 items-center gap-2 rounded-md border px-2.5 py-1 text-xs shadow-xs'>
      <span className={cn('h-3.5 w-0.5 shrink-0 rounded-full', props.accent)} />
      <span className='text-muted-foreground shrink-0'>{props.label}</span>
      <span className='text-foreground/85 font-mono font-semibold tabular-nums'>
        {props.value}
      </span>
      {props.sub ? (
        <span className='text-muted-foreground/80 font-mono text-[10px] tabular-nums'>
          {props.sub}
        </span>
      ) : null}
    </span>
  )
}

function fmtTokens(n: number | undefined | null): string {
  return formatCompactNumber(n ?? 0)
}

export function CommonLogsStats() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const searchParams = route.useSearch()
  const { sensitiveVisible, autoRefresh } = useUsageLogsContext()

  const { data: stats, isLoading } = useQuery({
    queryKey: ['usage-logs-stats', isAdmin, searchParams],
    queryFn: async () => {
      const params = buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams,
        columnFilters: [],
        isAdmin,
      })

      const result = isAdmin
        ? await getLogStats(params)
        : await getUserLogStats(params)

      return result.success
        ? { ...DEFAULT_LOG_STATS, ...(result.data || {}) }
        : DEFAULT_LOG_STATS
    },
    placeholderData: (previousData) => previousData,
    // Follow the same opt-in switch as the log table; no silent 30s polling.
    refetchInterval: autoRefresh ? 30_000 : false,
  })

  if (isLoading) {
    return (
      <div className='flex flex-wrap items-center gap-2'>
        <Skeleton className='h-7 w-[150px] rounded-md' />
        <Skeleton className='h-7 w-[100px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
        <Skeleton className='h-7 w-[140px] rounded-md' />
        <Skeleton className='h-7 w-[120px] rounded-md' />
      </div>
    )
  }

  const s = stats || DEFAULT_LOG_STATS
  const cacheRate = formatPercent(s.cache_rate ?? 0)

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <StatBadge
        label={t('Usage')}
        value={sensitiveVisible ? formatLogQuota(s.quota || 0) : '••••'}
        accent='bg-sky-500/70'
        sub={
          sensitiveVisible
            ? `${t('Total')} ${formatNumber(s.request_count || 0)}`
            : undefined
        }
      />
      <StatBadge
        label={t('RPM')}
        value={formatNumber(s.rpm || 0)}
        accent='bg-rose-500/65'
        sub={`${t('Total')} ${formatNumber(s.request_count || 0)}`}
      />
      <StatBadge
        label={t('TPM')}
        value={fmtTokens(s.tpm)}
        accent='bg-slate-400/70'
        sub={`${t('Total')} ${fmtTokens(s.total_tokens)}`}
      />
      <StatBadge
        label={t('Cache Read TPM')}
        value={fmtTokens(s.cache_read_tpm)}
        accent='bg-emerald-500/65'
        sub={`${t('Total')} ${fmtTokens(s.cache_read_tokens)}`}
      />
      <StatBadge
        label={t('Cache Create TPM')}
        value={fmtTokens(s.cache_create_tpm)}
        accent='bg-amber-500/65'
        sub={`${t('Total')} ${fmtTokens(s.cache_create_tokens)}`}
      />
      <StatBadge
        label={t('Output TPM')}
        value={fmtTokens(s.completion_tpm)}
        accent='bg-violet-500/65'
        sub={`${t('Total')} ${fmtTokens(s.completion_tokens)}`}
      />
      <StatBadge
        label={t('Cache Rate')}
        value={cacheRate}
        accent='bg-cyan-500/65'
      />
    </div>
  )
}
