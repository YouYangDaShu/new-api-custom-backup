import { Trophy } from 'lucide-react'
import { useState, type ReactNode } from 'react'

import { PageTransition } from '@/components/page-transition'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { cn } from '@/lib/utils'

import { formatTotalTokens } from './lib/format'
import { WeeklyUserLeaderboard } from './components'
import { useWeeklyUserRanking } from './hooks/use-rankings'
import type { WeeklyUserRankingRow } from './types'

function WeeklyTopThree(props: {
  rows: WeeklyUserRankingRow[]
  rewardAmounts: number[]
  weekStart: number
  weekEnd: number
}) {
  const start = new Date(props.weekStart * 1000).toLocaleDateString('zh-CN', {
    timeZone: 'Asia/Shanghai',
  })
  const end = new Date((props.weekEnd - 1) * 1000).toLocaleDateString('zh-CN', {
    timeZone: 'Asia/Shanghai',
  })

  return (
    <section className='space-y-3'>
      <div>
        <h2 className='flex items-center gap-2 text-lg font-semibold'>
          <Trophy className='size-5 text-amber-500' aria-hidden='true' />
          本周前三名
        </h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          周排行前三名送额度：第一名 ¥{props.rewardAmounts[0] ?? 0}，第二名 ¥
          {props.rewardAmounts[1] ?? 0}，第三名 ¥{props.rewardAmounts[2] ?? 0}
        </p>
        <p className='text-muted-foreground text-xs'>
          每周一 00:00 至周日 24:00，额度每周一 00:05
          自动发放；管理员参与排名但不参与奖励，奖励顺延给普通用户
        </p>
        <p className='text-muted-foreground mt-1 text-xs'>
          {start} 至 {end}
        </p>
      </div>
      <div className='grid gap-3 sm:grid-cols-3'>
        {props.rows.slice(0, 3).map((row, index) => (
          <Card key={`${row.rank}-${row.display_name}`}>
            <CardContent className='flex items-center gap-3 p-4'>
              <span
                className={cn(
                  'flex size-9 shrink-0 items-center justify-center rounded-full font-semibold',
                  index === 0 &&
                    'bg-amber-100 text-amber-700 dark:bg-amber-950',
                  index === 1 &&
                    'bg-slate-200 text-slate-700 dark:bg-slate-800',
                  index === 2 &&
                    'bg-orange-100 text-orange-700 dark:bg-orange-950'
                )}
              >
                {index + 1}
              </span>
              <div className='min-w-0 flex-1'>
                <p className='truncate text-sm font-medium'>
                  {row.display_name}
                </p>
                <p className='font-mono text-lg font-semibold'>
                  {formatTotalTokens(row.total_tokens)}
                </p>
                <p className='text-muted-foreground text-xs'>
                  {row.request_count.toLocaleString('zh-CN')} 次请求
                </p>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

export function TokenRankings() {
  const [period, setPeriod] = useState<'day' | 'week'>('day')
  const query = useWeeklyUserRanking(period)
  const weeklyQuery = useWeeklyUserRanking('week')
  let leaderboard: ReactNode = null
  if (query.isLoading) {
    leaderboard = <Skeleton className='h-[520px] w-full rounded-xl' />
  } else if (query.isError) {
    leaderboard = (
      <div className='rounded-xl border border-dashed px-6 py-12 text-center'>
        <h2 className='text-base font-semibold'>Token 排行榜加载失败</h2>
        <p className='text-muted-foreground mt-2 text-sm'>
          {query.error instanceof Error ? query.error.message : '请稍后重试'}
        </p>
      </div>
    )
  } else if (query.data?.data) {
    leaderboard = (
      <WeeklyUserLeaderboard snapshot={query.data.data} period={period} />
    )
  }

  return (
    <PageTransition className='mx-auto h-full w-full max-w-6xl overflow-y-auto p-4 sm:p-6'>
      <div className='space-y-6 pb-8'>
        <div className='flex flex-wrap items-start justify-between gap-4'>
          <div>
            <h1 className='text-2xl font-semibold tracking-tight'>用户排行</h1>
            <p className='text-muted-foreground mt-1 text-sm'>
              按总 Token 统计前 20 名，用户邮箱及长数字用户名已脱敏
            </p>
          </div>
          <Tabs
            value={period}
            onValueChange={(value) => setPeriod(value as 'day' | 'week')}
          >
            <TabsList>
              <TabsTrigger value='day'>日榜</TabsTrigger>
              <TabsTrigger value='week'>周榜</TabsTrigger>
            </TabsList>
          </Tabs>
        </div>

        {weeklyQuery.data?.data ? (
          <WeeklyTopThree
            rows={weeklyQuery.data.data.rows}
            rewardAmounts={weeklyQuery.data.data.reward_amounts ?? []}
            weekStart={weeklyQuery.data.data.week_start}
            weekEnd={weeklyQuery.data.data.week_end}
          />
        ) : null}

        {leaderboard}
      </div>
    </PageTransition>
  )
}
