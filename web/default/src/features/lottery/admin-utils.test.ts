import assert from 'node:assert/strict'
import { describe, it } from 'node:test'

import {
  LOTTERY_ADMIN_PAGE_SIZES,
  lotteryDrawDate,
  lotteryResetActionVariant,
} from './admin-utils.ts'

describe('lottery admin display helpers', () => {
  it('offers the required user-progress page sizes', () => {
    assert.deepEqual(LOTTERY_ADMIN_PAGE_SIZES, [10, 20, 50, 100])
  })

  it('converts draw timestamps from Unix seconds', () => {
    assert.equal(
      lotteryDrawDate(1_725_033_600).toISOString(),
      '2024-08-30T16:00:00.000Z'
    )
  })

  it('marks the all-user reset as destructive and targeted reset as neutral', () => {
    assert.equal(lotteryResetActionVariant('all'), 'destructive')
    assert.equal(lotteryResetActionVariant('user'), 'outline')
  })
})
