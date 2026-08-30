// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { describe, expect, test } from 'vitest'

import {
  formatHistoryRecipient,
  formatTokenUsageInBillions,
  type HistoryRow,
} from './history'

describe('formatHistoryRecipient', () => {
  test('shows both the recipient user ID and display name', () => {
    const row = {
      user_id: 101,
      display_name: 'A***',
    } satisfies Pick<HistoryRow, 'user_id' | 'display_name'>

    expect(formatHistoryRecipient(row, 'User ID')).toBe('User ID: 101 | A***')
  })

  test('keeps the user ID visible when the recipient no longer exists', () => {
    expect(
      formatHistoryRecipient({ user_id: 999, display_name: '' }, 'User ID')
    ).toBe('User ID: 999 | -')
  })
})

describe('formatTokenUsageInBillions', () => {
  test.each([
    [4_145_042_879, '4.15 B'],
    [500_000_000, '0.50 B'],
    [80_000_000, '0.08 B'],
  ])('formats %i tokens as %s', (totalTokens: number, expected: string) => {
    expect(formatTokenUsageInBillions(totalTokens)).toBe(expected)
  })
})
