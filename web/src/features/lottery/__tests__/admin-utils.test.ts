import { describe, expect, it } from 'vitest'

import {
  LOTTERY_ADMIN_PAGE_SIZES,
  lotteryDrawDate,
  lotteryResetActionVariant,
} from '../admin-utils'

describe('lottery admin display helpers', () => {
  it('offers the supported user-progress page sizes', () => {
    expect(LOTTERY_ADMIN_PAGE_SIZES).toEqual([10, 20, 50, 100])
  })

  it('converts draw timestamps from Unix seconds', () => {
    expect(lotteryDrawDate(1_725_033_600).toISOString()).toBe(
      '2024-08-30T16:00:00.000Z'
    )
  })

  it('marks the all-user reset as destructive and targeted reset as neutral', () => {
    expect(lotteryResetActionVariant('all')).toBe('destructive')
    expect(lotteryResetActionVariant('user')).toBe('outline')
  })
})
