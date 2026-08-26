import { Trophy } from 'lucide-react'

import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import { formatTokens } from '@/lib/format'

import type { WeeklyUserRankingRow } from '../types'

function formatRequests(value: number) {
  if (value >= 10_000) return `${(value / 10_000).toFixed(2).replace(/\.00$/, '')}万`
  return value.toLocaleString('zh-CN')
}

export function WeeklyTopThree(props: {
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
        <h2 className='text-lg font-semibold'>本周前三名</h2>
        <p className='text-muted-foreground mt-1 text-sm'>
          周排行前三名送额度：第一名 ¥{props.rewardAmounts[0] ?? 0}，第二名 ¥
          {props.rewardAmounts[1] ?? 0}，第三名 ¥{props.rewardAmounts[2] ?? 0}
        </p>
        <p className='text-muted-foreground text-xs'>
          每周一 00:00 至周日 24:00，额度每周一 00:05 自动发放；管理员参与排名但不参与奖励，奖励顺延给普通用户
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
                  'flex h-9 w-9 shrink-0 items-center justify-center rounded-full font-semibold',
                  index === 0 && 'bg-amber-100 text-amber-700 dark:bg-amber-950',
                  index === 1 && 'bg-slate-200 text-slate-700 dark:bg-slate-800',
                  index === 2 && 'bg-orange-100 text-orange-700 dark:bg-orange-950'
                )}
              >
                {index + 1}
              </span>
              <div className='min-w-0 flex-1'>
                <p className='truncate text-sm font-medium'>{row.display_name}</p>
                <p className='font-mono text-lg font-semibold'>{formatTokens(row.total_tokens)}</p>
                <p className='text-muted-foreground text-xs'>
                  {formatRequests(row.request_count)} 次请求
                </p>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  )
}

export function WeeklyUserLeaderboard(props: {
  rows: WeeklyUserRankingRow[]
  currentUser: WeeklyUserRankingRow | null
  period: 'day' | 'week'
  start: number
  end: number
}) {
  const currentUser = props.currentUser
  const start = new Date(props.start * 1000).toLocaleDateString('zh-CN', {
    timeZone: 'Asia/Shanghai',
  })
  const end = new Date((props.end - 1) * 1000).toLocaleDateString('zh-CN', {
    timeZone: 'Asia/Shanghai',
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle className='flex items-center gap-2 text-base'>
          <Trophy className='h-4 w-4 text-amber-500' aria-hidden='true' />
          {props.period === 'day' ? '日榜' : '周榜'}
        </CardTitle>
        <p className='text-muted-foreground text-xs'>
          {start} 至 {end}，仅统计成功请求
        </p>
        {currentUser ? (
          <div className='mt-2 flex items-center gap-2 border-l-2 border-foreground/30 pl-3 text-sm'>
            <span className='text-muted-foreground'>我的{props.period === 'day' ? '今日' : '本周'}排名</span>
            <span className='font-semibold'>第 {currentUser.rank} 名</span>
          </div>
        ) : (
          <div className='text-muted-foreground mt-2 rounded-lg border px-3 py-2 text-sm'>
            本期暂无 Token 使用记录
          </div>
        )}
      </CardHeader>
      <CardContent>
        <div className='overflow-x-auto'>
          <div className='min-w-[760px]'>
            <div className='text-muted-foreground grid grid-cols-[56px_minmax(180px,1fr)_100px_repeat(4,110px)] gap-2 border-b px-3 py-2 text-xs'>
              <span>排名</span><span>用户</span><span className='text-right'>请求</span><span className='text-right'>输入</span><span className='text-right'>输出</span><span className='text-right'>缓存</span><span className='text-right'>总 Token</span>
            </div>
            {props.rows.length === 0 ? (
              <p className='text-muted-foreground py-8 text-center text-sm'>暂无排行数据</p>
            ) : (
              props.rows.map((row) => (
                <div
                  key={`${row.rank}-${row.display_name}`}
                  className={cn(
                    'grid grid-cols-[56px_minmax(180px,1fr)_100px_repeat(4,110px)] items-center gap-2 border-b px-3 py-3 text-sm last:border-0',
                    row.is_current_user && 'bg-muted/60 font-medium'
                  )}
                >
                  <span className='font-mono font-semibold'>{row.rank}</span>
                  <span className='flex min-w-0 items-center gap-2 truncate'>
                    {row.display_name}
                    {row.is_current_user ? <span className='bg-foreground text-background rounded px-1.5 py-0.5 text-[10px] font-semibold'>我</span> : null}
                  </span>
                  <span className='text-right'>{formatRequests(row.request_count)}</span>
                  <span className='text-right font-mono'>{formatTokens(row.input_tokens)}</span>
                  <span className='text-right font-mono'>{formatTokens(row.output_tokens)}</span>
                  <span className='text-right font-mono'>{formatTokens(row.cache_tokens)}</span>
                  <span className='text-right font-mono font-semibold'>{formatTokens(row.total_tokens)}</span>
                </div>
              ))
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
