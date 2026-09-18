/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

export interface DraftPrize {
  id: string
  name: string
  amount: string
  weight: string
  prizeLevel: number
  enabled: boolean
  sortOrder: number
}

// prize_level=1 is the permanent first-prize identity used by pity and
// leaderboards. Removing it would make an existing configuration impossible
// to restore from the admin UI because new tiers intentionally never reuse a
// deleted level.
export function canRemoveDraftPrize(prizeLevel: number, prizeCount: number): boolean {
  return prizeCount > 1 && prizeLevel !== 1
}

/**
 * Create a new draft prize tier whose id never collides with an existing tier.
 * The backend rejects duplicate prize ids, and deleting a middle tier used to
 * make the next `tier-${length + 1}` id repeat an id still in the list.
 */
export function createDraftPrize(
  existing: ReadonlyArray<{ id: string; prizeLevel?: number }>
): DraftPrize {
  const usedIds = new Set(existing.map((prize) => prize.id))
  const usedLevels = new Set<number>()
  for (const prize of existing) {
    const level = prize.prizeLevel
    if (typeof level === 'number' && Number.isInteger(level) && level > 0) {
      usedLevels.add(level)
      continue
    }
    const match = /^tier-(\d+)$/.exec(prize.id)
    if (match) usedLevels.add(Number(match[1]))
  }
  let index = existing.length + 1
  while (usedIds.has(`tier-${index}`)) {
    index += 1
  }
  const maxLevel = Math.max(0, ...usedLevels)
  let prizeLevel = maxLevel + 1
  while (usedLevels.has(prizeLevel)) {
    prizeLevel += 1
  }
  return {
    id: `tier-${index}`,
    name: `Tier ${prizeLevel}`,
    amount: '',
    weight: '1',
    prizeLevel,
    enabled: true,
    sortOrder: existing.length,
  }
}
