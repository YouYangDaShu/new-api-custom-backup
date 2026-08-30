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

export const HISTORY_SLOTS = 60

export type AvailabilityWindow = '24h' | '168h' | '720h'

export const DEFAULT_AVAILABILITY_WINDOW: AvailabilityWindow = '24h'

export const AVAILABILITY_WINDOWS = [
  {
    key: '24h',
    labelKey: '24h',
    availabilityLabelKey: '24-hour availability',
  },
  {
    key: '168h',
    labelKey: '7d',
    availabilityLabelKey: '7-day availability',
  },
  {
    key: '720h',
    labelKey: '30d',
    availabilityLabelKey: '30-day availability',
  },
] as const

export type MonitorHistoryPoint = {
  status: string
  latency_ms: number
  checked_at: number
}

export function timelineTickClass(point: MonitorHistoryPoint | null): string {
  if (point?.status === 'success' && point.latency_ms < 6000) {
    return 'h-5 bg-success'
  }
  if (point?.status === 'success' && point.latency_ms >= 6000) {
    return 'h-3.5 bg-warning'
  }
  if (point?.status === 'failed' || point?.status === 'timeout') {
    return 'h-2 bg-destructive'
  }
  return 'h-1 bg-muted-foreground/25'
}

export function getAvailabilityValue(
  availability: Record<string, number> | undefined,
  window: AvailabilityWindow
): number | undefined {
  return availability?.[window]
}

export function buildTimelineSlots(
  history: readonly MonitorHistoryPoint[] | null | undefined,
  slotCount = HISTORY_SLOTS
): Array<MonitorHistoryPoint | null> {
  if (slotCount <= 0) return []

  const points = (history ?? []).slice(0, slotCount).reverse()
  const emptySlots = Array<MonitorHistoryPoint | null>(
    slotCount - points.length
  ).fill(null)

  return [...emptySlots, ...points]
}
