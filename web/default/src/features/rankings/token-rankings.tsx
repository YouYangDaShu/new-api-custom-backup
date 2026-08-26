import { useState } from 'react'

import { PageTransition } from '@/components/page-transition'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

import { WeeklyTopThree, WeeklyUserLeaderboard } from './components'
import { useWeeklyUserRanking } from './hooks/use-rankings'

export function TokenRankings() {
  const [period, setPeriod] = useState<'day' | 'week'>('day')
  const query = useWeeklyUserRanking(period)
  const weeklyQuery = useWeeklyUserRanking('week')

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
            rewardAmounts={weeklyQuery.data.data.reward_amounts}
            weekStart={weeklyQuery.data.data.week_start}
            weekEnd={weeklyQuery.data.data.week_end}
          />
        ) : null}

        {query.isLoading ? (
          <Skeleton className='h-[520px] w-full rounded-xl' />
        ) : query.isError ? (
          <div className='rounded-xl border border-dashed px-6 py-12 text-center'>
            <h2 className='text-base font-semibold'>Token 排行榜加载失败</h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              {query.error instanceof Error ? query.error.message : '请稍后重试'}
            </p>
          </div>
        ) : (
          <WeeklyUserLeaderboard
            rows={query.data?.data.rows ?? []}
            currentUser={query.data?.data.current_user ?? null}
            period={period}
            start={query.data?.data.week_start ?? 0}
            end={query.data?.data.week_end ?? 0}
          />
        )}
      </div>
    </PageTransition>
  )
}
