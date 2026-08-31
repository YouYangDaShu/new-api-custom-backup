export const LOTTERY_ADMIN_PAGE_SIZES = [10, 20, 50, 100] as const

export type LotteryResetAction = 'all' | 'user'

export function lotteryResetActionVariant(
  action: LotteryResetAction
): 'destructive' | 'outline' {
  return action === 'all' ? 'destructive' : 'outline'
}

export function lotteryDrawDate(createdAt: number): Date {
  return new Date(createdAt * 1000)
}
