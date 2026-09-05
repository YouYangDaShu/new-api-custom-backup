// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { describe, expect, test } from 'vitest'

import {
  encodeTokenGroupValue,
  parseTokenGroupValue,
} from './token-group-encoding'

describe('token group smart mode encoding', () => {
  test('round-trips success-rate mode', () => {
    const value = encodeTokenGroupValue({
      mode: 'smart',
      groups: [],
      smartMode: 'success_rate',
    })
    expect(value).toBe('@smart:success_rate')
    expect(parseTokenGroupValue(value).smartMode).toBe('success_rate')
  })
})
