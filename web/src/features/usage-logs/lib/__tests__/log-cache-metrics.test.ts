/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import {
  formatLogCacheRate,
  getLogCacheMetrics,
  getLogCacheWriteTokens,
} from '../format'

describe('per-log cache metrics', () => {
  it('derives uncached input and cache rate from total prompt tokens', () => {
    expect(getLogCacheMetrics(4387, 3840)).toEqual({
      cacheTokens: 3840,
      uncachedInputTokens: 547,
      cacheRate: (3840 / 4387) * 100,
      hasCacheData: true,
    })
    expect(formatLogCacheRate((3840 / 4387) * 100)).toBe('87.5%')
  })

  it('distinguishes a reported zero cache count from a missing field', () => {
    expect(getLogCacheMetrics(1200, 0)).toMatchObject({
      cacheTokens: 0,
      uncachedInputTokens: 1200,
      cacheRate: 0,
      hasCacheData: true,
    })
    expect(getLogCacheMetrics(1200, undefined)).toMatchObject({
      cacheTokens: 0,
      uncachedInputTokens: 1200,
      cacheRate: 0,
      hasCacheData: false,
    })
  })

  it('preserves reported cache tokens while clamping derived metrics', () => {
    expect(getLogCacheMetrics(100, 150)).toEqual({
      cacheTokens: 150,
      uncachedInputTokens: 0,
      cacheRate: 100,
      hasCacheData: true,
    })
    expect(formatLogCacheRate(100.01)).toBe('100%')
  })

  it('keeps explicit cache data visible when prompt tokens are zero', () => {
    expect(getLogCacheMetrics(0, 150)).toEqual({
      cacheTokens: 150,
      uncachedInputTokens: 0,
      cacheRate: 0,
      hasCacheData: true,
    })
  })

  it('rejects non-finite token values instead of displaying fake metrics', () => {
    expect(getLogCacheMetrics(Number.NaN, Number.POSITIVE_INFINITY)).toEqual({
      cacheTokens: 0,
      uncachedInputTokens: 0,
      cacheRate: 0,
      hasCacheData: false,
    })
    expect(getLogCacheMetrics(Number.NEGATIVE_INFINITY, 50)).toEqual({
      cacheTokens: 50,
      uncachedInputTokens: 0,
      cacheRate: 0,
      hasCacheData: true,
    })
  })

  it('accepts only finite positive cache-write token counts', () => {
    expect(
      getLogCacheWriteTokens({
        cache_creation_tokens: 70,
        cache_creation_tokens_5m: 20,
        cache_creation_tokens_1h: 30,
      })
    ).toBe(50)
    expect(
      getLogCacheWriteTokens({
        cache_creation_tokens: 70,
        cache_creation_tokens_5m: Number.POSITIVE_INFINITY,
        cache_creation_tokens_1h: -1,
      })
    ).toBe(70)
    expect(
      getLogCacheWriteTokens({
        cache_creation_tokens: Number.POSITIVE_INFINITY,
      })
    ).toBe(0)
    expect(
      getLogCacheWriteTokens({
        cache_creation_tokens_5m: Number.MAX_VALUE,
        cache_creation_tokens_1h: Number.MAX_VALUE,
      })
    ).toBe(0)
  })
})
