import { describe, expect, test } from 'vitest'

import { getTieredInputTokenThreshold } from './context-warning'

describe('tiered context warning threshold', () => {
  test('reads the input boundary from the billing expression', () => {
    expect(
      getTieredInputTokenThreshold(
        'len <= 200000 ? tier("short", p * 2 + c * 4) : tier("long", p * 4 + c * 8)'
      )
    ).toBe(200000)
  })

  test('does not warn for malformed expressions or expressions without tiers', () => {
    expect(getTieredInputTokenThreshold('not a billing expression')).toBeNull()
    expect(getTieredInputTokenThreshold('tier("base", p * 2 + c * 4)')).toBeNull()
  })
})