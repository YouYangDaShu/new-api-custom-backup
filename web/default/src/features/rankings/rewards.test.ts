// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { describe, expect, test } from 'vitest'

import {
  MAX_REWARD_RANKS,
  MAX_REWARD_VALUE,
  parseRewardInputs,
  rewardInputsFromApi,
} from './rewards'

describe('rewardInputsFromApi', () => {
  test('preserves every reward tier returned by the API', () => {
    expect(rewardInputsFromApi([12.5, 8, 3.25, 1])).toEqual([
      '12.5',
      '8',
      '3.25',
      '1',
    ])
  })

  test('provides three editable tiers when the API returns none', () => {
    expect(rewardInputsFromApi([])).toEqual(['0', '0', '0'])
  })
})

describe('parseRewardInputs', () => {
  test('converts each tier independently and treats an empty input as zero', () => {
    expect(parseRewardInputs(['12.5', '', '3'])).toEqual([12.5, 0, 3])
  })

  test('rejects non-numeric, negative, and oversized values', () => {
    expect(parseRewardInputs(['not-a-number'])).toBeNull()
    expect(parseRewardInputs(['-1'])).toBeNull()
    expect(parseRewardInputs([String(MAX_REWARD_VALUE + 1)])).toBeNull()
  })

  test('accepts the maximum value and rejects more than 100 tiers', () => {
    expect(parseRewardInputs([String(MAX_REWARD_VALUE)])).toEqual([
      MAX_REWARD_VALUE,
    ])
    expect(parseRewardInputs(Array(MAX_REWARD_RANKS + 1).fill('0'))).toBeNull()
  })
})
