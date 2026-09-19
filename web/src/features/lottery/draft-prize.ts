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

export interface PityBoundaryConfig {
  pity_base_end: number
  pity_stage_1_start: number
  pity_stage_1_end: number
  pity_stage_2_start: number
  pity_stage_2_end: number
}

export type PityBoundary = 'base' | 'stage1' | 'stage2'

export function enabledPrizeWeightsTotal100(
  prizes: ReadonlyArray<{ enabled: boolean; weight: string }>
): boolean {
  const enabled = prizes.filter((prize) => prize.enabled)
  if (enabled.length === 0) return false
  const total = enabled.reduce((sum, prize) => sum + Number(prize.weight), 0)
  return (
    enabled.every((prize) => {
      const weight = Number(prize.weight)
      return prize.weight.trim() !== '' && weight > 0 && weight <= 100
    }) &&
    Number.isFinite(total) &&
    Math.abs(total - 100) <= 0.000001
  )
}

function blankRates(length: number): number[] {
  return Array.from({ length: Math.max(length, 0) }, () => Number.NaN)
}

export function updatePityBoundary(
  config: PityBoundaryConfig,
  boundary: PityBoundary,
  value: number
) {
  if (boundary === 'base') {
    const stage1Start = value + 1
    const stage1End = Math.max(stage1Start, config.pity_stage_1_end)
    const stage2Start = stage1End + 1
    const stage2End = Math.max(stage2Start, config.pity_stage_2_end)
    return {
      pity_base_end: value,
      pity_stage_1_start: stage1Start,
      pity_stage_1_end: stage1End,
      pity_stage_1_rates: blankRates(stage1End - stage1Start + 1),
      pity_stage_2_start: stage2Start,
      pity_stage_2_end: stage2End,
      pity_stage_2_rates: blankRates(stage2End - stage2Start + 1),
      pity_hard_threshold: stage2End + 1,
    }
  }

  if (boundary === 'stage1') {
    const stage2Start = value + 1
    const stage2End = Math.max(stage2Start, config.pity_stage_2_end)
    return {
      pity_stage_1_end: value,
      pity_stage_1_rates: blankRates(value - config.pity_stage_1_start + 1),
      pity_stage_2_start: stage2Start,
      pity_stage_2_end: stage2End,
      pity_stage_2_rates: blankRates(stage2End - stage2Start + 1),
      pity_hard_threshold: stage2End + 1,
    }
  }

  const stage2End = Math.max(config.pity_stage_2_start, value)
  return {
    pity_stage_2_end: stage2End,
    pity_stage_2_rates: blankRates(stage2End - config.pity_stage_2_start + 1),
    pity_hard_threshold: stage2End + 1,
  }
}

// prize_level=1 is the permanent first-prize identity used by pity and
// leaderboards. Removing it would make an existing configuration impossible
// to restore from the admin UI because new tiers intentionally never reuse a
// deleted level.
export function canRemoveDraftPrize(
  prizeLevel: number,
  prizeCount: number
): boolean {
  return prizeCount > 1 && prizeLevel !== 1
}

/**
 * Create the next permanent prize identity from the persisted creation cursor.
 * Deleted levels stay consumed, while an id suffix handles legacy id collisions.
 */
export function createDraftPrize(
  existing: ReadonlyArray<{ id: string; prizeLevel?: number }>,
  nextPrizeLevel: number
): DraftPrize {
  const usedIds = new Set(existing.map((prize) => prize.id))
  let id = `tier-${nextPrizeLevel}`
  let suffix = 2
  while (usedIds.has(id)) {
    id = `tier-${nextPrizeLevel}-${suffix}`
    suffix += 1
  }
  return {
    id,
    name: `Tier ${nextPrizeLevel}`,
    amount: '',
    weight: '1',
    prizeLevel: nextPrizeLevel,
    enabled: true,
    sortOrder: existing.length,
  }
}

export function removeDraftPrize(
  prizes: ReadonlyArray<DraftPrize>,
  removedId: string,
  persistedNextPrizeLevel: number
): { prizes: DraftPrize[]; nextPrizeLevel: number } {
  const remaining = prizes.filter((prize) => prize.id !== removedId)
  const draftLevels = remaining
    .filter((prize) => prize.prizeLevel >= persistedNextPrizeLevel)
    .sort((a, b) => a.prizeLevel - b.prizeLevel)
  const reassigned = new Map(
    draftLevels.map((prize, index) => [
      prize.id,
      persistedNextPrizeLevel + index,
    ])
  )

  return {
    prizes: remaining.map((prize, sortOrder) => {
      const prizeLevel = reassigned.get(prize.id) ?? prize.prizeLevel
      const defaultName = `Tier ${prize.prizeLevel}`
      return {
        ...prize,
        prizeLevel,
        name: prize.name === defaultName ? `Tier ${prizeLevel}` : prize.name,
        sortOrder,
      }
    }),
    nextPrizeLevel: persistedNextPrizeLevel + draftLevels.length,
  }
}
