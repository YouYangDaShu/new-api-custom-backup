import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Crown, Loader2, Medal, Trophy, Waves } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import {
  getLotteryLeaderboards,
  type LotteryLeaderboardBoard,
  type LotteryLeaderboardData,
  type LotteryLeaderboardPeriod,
  type LotteryLeaderboardRow,
} from './api'

type BoardKey = 'lucky' | 'draw_count' | 'hard_pity'

function rankMedal(rank: number) {
  if (rank === 1) {
    return <span aria-label='1st'>🥇</span>
  }
  if (rank === 2) {
    return <span aria-label='2nd'>🥈</span>
  }
  if (rank === 3) {
    return <span aria-label='3rd'>🥉</span>
  }
  return (
    <span className='text-muted-foreground text-xs tabular-nums'>{rank}</span>
  )
}

function boardTitle(t: (key: string) => string, key: BoardKey) {
  if (key === 'lucky') {
    return t('Lucky leaderboard')
  }
  if (key === 'draw_count') {
    return t('Draw count leaderboard')
  }
  return t('Hard pity leaderboard')
}

function boardDescription(t: (key: string) => string, key: BoardKey) {
  if (key === 'lucky') {
    return t('Natural first prizes in draws 1 to 10')
  }
  if (key === 'draw_count') {
    return t('Real lottery draws in the selected period')
  }
  return t('First prizes won at or after the hard pity threshold')
}

function rowValue(
  t: (key: string, options?: Record<string, unknown>) => string,
  key: BoardKey,
  row: LotteryLeaderboardRow
) {
  if (key === 'lucky') {
    return `${row.pity_step ?? 0} ${t('draws')}`
  }
  if (key === 'hard_pity') {
    return `${row.value} ${t('hard pity hits')}`
  }
  return `${row.value} ${t('draws')}`
}

function boardIcon(key: BoardKey) {
  if (key === 'lucky') {
    return <Crown className='size-4' aria-hidden='true' />
  }
  if (key === 'draw_count') {
    return <Trophy className='size-4' aria-hidden='true' />
  }
  return <Waves className='size-4' aria-hidden='true' />
}

function BoardCard({
  board,
  keyName,
}: {
  board: LotteryLeaderboardBoard
  keyName: BoardKey
}) {
  const { t } = useTranslation()
  const isEmpty = board.rows.length === 0
  return (
    <section className='bg-card rounded-2xl border p-5 shadow-sm sm:p-6'>
      <div className='flex items-start gap-3'>
        <div className='bg-primary/10 text-primary flex size-9 shrink-0 items-center justify-center rounded-lg'>
          {boardIcon(keyName)}
        </div>
        <div className='min-w-0'>
          <h3 className='text-sm font-semibold'>{boardTitle(t, keyName)}</h3>
          <p className='text-muted-foreground mt-1 text-xs'>
            {boardDescription(t, keyName)}
          </p>
        </div>
      </div>
      <div className='mt-4 space-y-2'>
        {isEmpty ? (
          <p className='text-muted-foreground rounded-xl border border-dashed px-3 py-5 text-center text-sm'>
            {t('No leaderboard records yet')}
          </p>
        ) : (
          board.rows.map((row) => (
            <div
              className='flex items-center gap-3 rounded-xl border px-3 py-2.5'
              key={`${row.rank}-${row.display_name}-${row.value}`}
            >
              <span className='flex size-6 shrink-0 items-center justify-center text-sm'>
                {rankMedal(row.rank)}
              </span>
              <span className='min-w-0 flex-1 truncate text-sm'>
                {row.display_name}
              </span>
              <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
                {rowValue(t, keyName, row)}
              </span>
            </div>
          ))
        )}
      </div>
      <div className='border-primary/20 bg-primary/5 mt-4 flex items-center gap-3 rounded-xl border px-3 py-2.5'>
        <Medal className='text-primary size-4 shrink-0' aria-hidden='true' />
        <span className='min-w-0 flex-1 truncate text-xs'>
          {t('Your best record')}
        </span>
        {board.current_user ? (
          <>
            <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
              {rowValue(t, keyName, board.current_user)}
            </span>
            <span className='text-primary shrink-0 text-xs font-semibold tabular-nums'>
              #{board.current_user.rank}
            </span>
          </>
        ) : (
          <span className='text-muted-foreground shrink-0 text-xs'>
            {t('Not ranked yet')}
          </span>
        )}
      </div>
    </section>
  )
}

function periodLabel(
  t: (key: string) => string,
  period: LotteryLeaderboardPeriod
) {
  if (period === 'day') {
    return t('Today')
  }
  if (period === 'week') {
    return t('This week')
  }
  if (period === 'month') {
    return t('This month')
  }
  return t('All time')
}

export function LotteryLeaderboards() {
  const { t } = useTranslation()
  const [period, setPeriod] = useState<LotteryLeaderboardPeriod>('all')
  const query = useQuery({
    queryKey: ['lottery-leaderboards', period],
    queryFn: () => getLotteryLeaderboards(period),
  })
  const data: LotteryLeaderboardData | undefined = query.data?.data
  let leaderboardContent
  if (query.isLoading) {
    leaderboardContent = (
      <div className='bg-card flex min-h-40 items-center justify-center rounded-2xl border'>
        <Loader2
          className='text-muted-foreground size-5 animate-spin'
          aria-hidden='true'
        />
        <span className='text-muted-foreground ml-2 text-sm'>
          {t('Loading')}
        </span>
      </div>
    )
  } else if (query.isError || !data) {
    leaderboardContent = (
      <div className='bg-card flex min-h-40 items-center justify-center gap-2 rounded-2xl border px-6 text-sm'>
        <CircleAlert className='text-destructive size-4' aria-hidden='true' />
        {t('Unable to load lottery rankings')}
      </div>
    )
  } else {
    leaderboardContent = (
      <>
        <p className='text-muted-foreground text-xs'>
          {t('Draw count period')}: {periodLabel(t, period)} · {data.timezone}
        </p>
        <div className='grid gap-4 lg:grid-cols-3'>
          <BoardCard board={data.lucky} keyName='lucky' />
          <BoardCard board={data.draw_count} keyName='draw_count' />
          <BoardCard board={data.hard_pity} keyName='hard_pity' />
        </div>
      </>
    )
  }

  return (
    <section className='space-y-4' aria-labelledby='lottery-leaderboards-title'>
      <div className='flex flex-col justify-between gap-3 sm:flex-row sm:items-end'>
        <div>
          <p className='text-primary text-[11px] font-semibold tracking-[0.2em] uppercase'>
            {t('Lottery rankings')}
          </p>
          <h2
            id='lottery-leaderboards-title'
            className='mt-2 text-2xl font-semibold tracking-tight'
          >
            {t('Three leaderboards')}
          </h2>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Top 10 with privacy-safe identities')}
          </p>
        </div>
        <div
          className='flex flex-wrap gap-2'
          role='group'
          aria-label={t('Draw count period')}
        >
          {(['all', 'day', 'week', 'month'] as const).map((item) => (
            <Button
              key={item}
              type='button'
              size='sm'
              variant={period === item ? 'default' : 'outline'}
              onClick={() => setPeriod(item)}
            >
              {periodLabel(t, item)}
            </Button>
          ))}
        </div>
      </div>
      {leaderboardContent}
    </section>
  )
}
