import { Trophy } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'

import type { WeeklyUserRankingRow, WeeklyUserRankingSnapshot } from '../types'

function formatTokens(value: number) {
  return Number(value || 0).toLocaleString()
}

function formatDate(timestamp: number, timezone: string) {
  return new Date(timestamp * 1000).toLocaleDateString(undefined, { timeZone: timezone })
}

function rewardText(row: WeeklyUserRankingRow, t: (key: string, options?: { amount: string }) => string) {
  if (!row.reward_eligible || !row.reward_amount) return null
  return t('Reward: {{amount}}', { amount: row.reward_amount.toFixed(2) })
}

export function WeeklyUserLeaderboard(props: {
  snapshot: WeeklyUserRankingSnapshot
  period: 'day' | 'week'
}) {
  const { t } = useTranslation()
  const timezone = props.snapshot.timezone || 'Asia/Shanghai'
  const currentUser = props.snapshot.current_user
  const rows = props.snapshot.rows || []
  const currentUserOutsideRows =
    currentUser && !rows.some((row) => row.is_current_user)
      ? currentUser
      : null
  const label = props.period === 'day' ? t('Today') : t('This week')

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-base'>
          <Trophy className='text-amber-500 size-4' aria-hidden='true' />
          {t('User ranking')} · {label}
        </CardTitle>
        <p className='text-muted-foreground text-xs'>
          {formatDate(props.snapshot.week_start, timezone)} – {formatDate(props.snapshot.week_end - 1, timezone)} · {t('Successful requests only')}
        </p>
        {currentUser ? (
          <p className='border-foreground/30 text-sm border-l-2 pl-3'>
            {t('Your rank')}: <span className='font-semibold'>{currentUser.rank}</span>
            {rewardText(currentUser, t) ? <span className='text-muted-foreground ml-2'>{rewardText(currentUser, t)}</span> : null}
          </p>
        ) : (
          <p className='text-muted-foreground rounded-lg border px-3 py-2 text-sm'>{t('No ranking data')}</p>
        )}
      </CardHeader>
      <CardContent>
        <div className='overflow-x-auto'>
          <div className='min-w-[680px]'>
            <div className='text-muted-foreground grid grid-cols-[56px_minmax(180px,1fr)_90px_repeat(4,110px)] gap-2 border-b px-3 py-2 text-xs'>
              <span>{t('Rank')}</span><span>{t('User')}</span><span className='text-right'>{t('Requests')}</span><span className='text-right'>{t('Input')}</span><span className='text-right'>{t('Output')}</span><span className='text-right'>{t('Cache')}</span><span className='text-right'>{t('Total tokens')}</span>
            </div>
            {rows.length === 0 ? <p className='text-muted-foreground py-8 text-center text-sm'>{t('No ranking data')}</p> : rows.map((row) => (
              <div key={`${row.rank}-${row.display_name}`} className={cn('grid grid-cols-[56px_minmax(180px,1fr)_90px_repeat(4,110px)] items-center gap-2 border-b px-3 py-3 text-sm last:border-0', row.is_current_user && 'bg-muted/60 font-medium')}>
                <span className='font-mono font-semibold'>{row.rank}</span>
                <span className='truncate'>{row.display_name}{row.is_current_user ? <span className='bg-foreground text-background ml-2 rounded px-1.5 py-0.5 text-[10px]'>{t('Me')}</span> : null}</span>
                <span className='text-right'>{formatTokens(row.request_count)}</span>
                <span className='text-right font-mono'>{formatTokens(row.input_tokens)}</span>
                <span className='text-right font-mono'>{formatTokens(row.output_tokens)}</span>
                <span className='text-right font-mono'>{formatTokens(row.cache_tokens)}</span>
                <span className='text-right font-mono font-semibold'>{formatTokens(row.total_tokens)}</span>
              </div>
            ))}
            {currentUserOutsideRows ? (
              <>
                <div className='text-muted-foreground border-b px-3 py-1 text-center text-xs'>···</div>
                <div className='bg-muted/60 grid grid-cols-[56px_minmax(180px,1fr)_90px_repeat(4,110px)] items-center gap-2 px-3 py-3 text-sm font-medium'>
                  <span className='font-mono font-semibold'>{currentUserOutsideRows.rank}</span>
                  <span className='truncate'>{currentUserOutsideRows.display_name}<span className='bg-foreground text-background ml-2 rounded px-1.5 py-0.5 text-[10px]'>{t('Me')}</span></span>
                  <span className='text-right'>{formatTokens(currentUserOutsideRows.request_count)}</span>
                  <span className='text-right font-mono'>{formatTokens(currentUserOutsideRows.input_tokens)}</span>
                  <span className='text-right font-mono'>{formatTokens(currentUserOutsideRows.output_tokens)}</span>
                  <span className='text-right font-mono'>{formatTokens(currentUserOutsideRows.cache_tokens)}</span>
                  <span className='text-right font-mono font-semibold'>{formatTokens(currentUserOutsideRows.total_tokens)}</span>
                </div>
              </>
            ) : null}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
