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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  detectDashboardRangePresetDays,
  getDashboardDateRange,
} from './filters'

describe('dashboard date range presets', () => {
  test('today spans local midnight to the current moment', () => {
    const now = new Date(2026, 6, 19, 20, 30, 45, 123)

    const range = getDashboardDateRange(0, now)

    assert.deepEqual(range.start, new Date(2026, 6, 19, 0, 0, 0, 0))
    assert.deepEqual(range.end, now)
    assert.equal(
      detectDashboardRangePresetDays(
        {
          start_timestamp: range.start,
          end_timestamp: range.end,
        },
        now
      ),
      0
    )
  })

  test('one day remains a rolling 24 hour range', () => {
    const now = new Date(2026, 6, 19, 20, 30, 45, 123)

    const range = getDashboardDateRange(1, now)

    assert.deepEqual(
      range.start,
      new Date(2026, 6, 18, 20, 30, 45, 123)
    )
    assert.deepEqual(range.end, now)
    assert.equal(
      detectDashboardRangePresetDays({
        start_timestamp: range.start,
        end_timestamp: range.end,
      }),
      1
    )
  })

  test('today switches to the next local midnight after the date changes', () => {
    const nextDay = new Date(2026, 6, 20, 0, 0, 0, 100)

    const range = getDashboardDateRange(0, nextDay)

    assert.deepEqual(range.start, new Date(2026, 6, 20, 0, 0, 0, 0))
    assert.deepEqual(range.end, nextDay)
  })
})
