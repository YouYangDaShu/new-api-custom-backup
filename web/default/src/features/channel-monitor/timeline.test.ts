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

// @ts-expect-error Vitest is provided by bunx for this isolated test.
import { describe, expect, test } from 'vitest'

import {
  buildTimelineSlots,
  DEFAULT_AVAILABILITY_WINDOW,
  getMonitorRefreshInterval,
  getAvailabilityValue,
  HISTORY_SLOTS,
  timelineTickClass,
  type MonitorHistoryPoint,
} from './timeline'

const newest: MonitorHistoryPoint = {
  status: 'success',
  latency_ms: 120,
  checked_at: 300,
}
const middle: MonitorHistoryPoint = {
  status: 'timeout',
  latency_ms: 0,
  checked_at: 200,
}
const oldest: MonitorHistoryPoint = {
  status: 'failed',
  latency_ms: 80,
  checked_at: 100,
}

describe('buildTimelineSlots', () => {
  test('uses 60 slots by default and keeps the newest sample on the right', () => {
    const slots = buildTimelineSlots([oldest, middle, newest])

    expect(HISTORY_SLOTS).toBe(60)
    expect(slots).toHaveLength(60)
    expect(slots.slice(0, 57)).toEqual(Array(57).fill(null))
    expect(slots.slice(-3)).toEqual([oldest, middle, newest])
    expect(slots.at(-1)).toBe(newest)
  })

  test('orders real samples from oldest to newest', () => {
    expect(buildTimelineSlots([oldest, middle, newest], 3)).toEqual([
      oldest,
      middle,
      newest,
    ])
  })

  test('pads missing slots on the left and keeps the newest sample at the right', () => {
    const slots = buildTimelineSlots([oldest, middle, newest], 5)

    expect(slots.slice(0, 2)).toEqual([null, null])
    expect(slots.at(-1)).toBe(newest)
  })

  test('keeps only the newest samples when history exceeds the slot count', () => {
    const older: MonitorHistoryPoint = {
      status: 'success',
      latency_ms: 90,
      checked_at: 50,
    }

    expect(buildTimelineSlots([older, oldest, middle, newest], 2)).toEqual([
      middle,
      newest,
    ])
  })

  test('fills an empty history with null slots', () => {
    expect(buildTimelineSlots([], 4)).toEqual([null, null, null, null])
  })

  test('does not modify the input array', () => {
    const history = [oldest, middle, newest]
    const snapshot = [...history]

    buildTimelineSlots(history, 5)

    expect(history).toEqual(snapshot)
  })
})

describe('channel monitor refresh interval', () => {
  test('uses the shortest enabled interval in milliseconds', () => {
    expect(getMonitorRefreshInterval([{ interval_seconds: 60 }])).toBe(60_000)
    expect(
      getMonitorRefreshInterval([
        { interval_seconds: 120, enabled: true },
        { interval_seconds: 60, enabled: true },
      ])
    ).toBe(60_000)
  })

  test('ignores disabled and invalid intervals without polling too often', () => {
    expect(
      getMonitorRefreshInterval([{ interval_seconds: 60, enabled: false }])
    ).toBe(false)
    expect(
      getMonitorRefreshInterval([
        { interval_seconds: 15, enabled: true },
        { interval_seconds: 60, enabled: false },
      ])
    ).toBe(false)
  })

  test('stops polling when the monitoring module is disabled', () => {
    expect(
      getMonitorRefreshInterval([
        { interval_seconds: 60, module_enabled: false },
      ])
    ).toBe(false)
    expect(
      getMonitorRefreshInterval([
        { interval_seconds: 60 },
        { interval_seconds: 120, module_enabled: false },
      ])
    ).toBe(false)
    expect(getMonitorRefreshInterval([])).toBe(false)
  })

  test('recomputes when monitor configuration changes', () => {
    expect(getMonitorRefreshInterval([{ interval_seconds: 120 }])).toBe(120_000)
    expect(getMonitorRefreshInterval([{ interval_seconds: 60 }])).toBe(60_000)
  })
})

describe('channel monitor availability', () => {
  test('defaults to the 24-hour window and reads each selected window', () => {
    const availability = { '24h': 99.1, '168h': 98.2, '720h': 97.3 }

    expect(DEFAULT_AVAILABILITY_WINDOW).toBe('24h')
    expect(getAvailabilityValue(availability, '24h')).toBe(99.1)
    expect(getAvailabilityValue(availability, '168h')).toBe(98.2)
    expect(getAvailabilityValue(availability, '720h')).toBe(97.3)
  })

  test('keeps selected window values independent for separate monitor modules', () => {
    const firstModule = { '24h': 99.9, '168h': 98.9, '720h': 97.9 }
    const secondModule = { '24h': 91.1, '168h': 92.2, '720h': 93.3 }

    expect(getAvailabilityValue(firstModule, '168h')).toBe(98.9)
    expect(getAvailabilityValue(secondModule, '720h')).toBe(93.3)
    expect(getAvailabilityValue(firstModule, '168h')).not.toBe(
      getAvailabilityValue(secondModule, '720h')
    )
  })
})

describe('timelineTickClass', () => {
  test('uses distinct heights for normal, slow, failed, and empty samples', () => {
    expect(timelineTickClass({ ...newest, latency_ms: 5999 })).toBe(
      'h-5 bg-success'
    )
    expect(timelineTickClass({ ...newest, latency_ms: 6000 })).toBe(
      'h-3.5 bg-warning'
    )
    expect(timelineTickClass(oldest)).toBe('h-2 bg-destructive')
    expect(timelineTickClass(middle)).toBe('h-2 bg-destructive')
    expect(timelineTickClass(null)).toBe('h-1 bg-muted-foreground/25')
  })
})
