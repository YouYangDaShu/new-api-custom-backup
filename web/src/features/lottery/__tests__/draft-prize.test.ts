import { describe, expect, it } from 'vitest'

import {
  canRemoveDraftPrize,
  createDraftPrize,
  enabledPrizeWeightsTotal100,
  removeDraftPrize,
  updatePityBoundary,
} from '../draft-prize'

describe('lottery draft prize ids', () => {
  it('keeps the permanent first-prize identity available for pity', () => {
    expect(canRemoveDraftPrize(1, 2)).toBe(false)
    expect(canRemoveDraftPrize(2, 2)).toBe(true)
    expect(canRemoveDraftPrize(2, 1)).toBe(false)
  })

  it('numbers fresh tiers sequentially', () => {
    expect(createDraftPrize([], 1).id).toBe('tier-1')
    expect(createDraftPrize([{ id: 'tier-1' }], 2).id).toBe('tier-2')
  })

  it('does not reuse the deleted highest prize level', () => {
    const next = createDraftPrize([{ id: 'tier-1', prizeLevel: 1 }], 3)

    expect(next.id).toBe('tier-3')
    expect(next.prizeLevel).toBe(3)
    expect(next.name).toBe('Tier 3')
  })

  it('suffixes a legacy id collision without changing the level', () => {
    const next = createDraftPrize([{ id: 'tier-3' }], 3)

    expect(next.id).toBe('tier-3-2')
    expect(next.prizeLevel).toBe(3)
  })

  it('reclaims only unsaved draft levels after a draft tier is removed', () => {
    const persisted1 = createDraftPrize([], 1)
    const persisted2 = createDraftPrize([persisted1], 2)
    const draft3 = createDraftPrize([persisted1, persisted2], 3)
    const draft4 = createDraftPrize([persisted1, persisted2, draft3], 4)

    const removedDraft = removeDraftPrize(
      [persisted1, persisted2, draft3, draft4],
      draft3.id,
      3
    )
    expect(removedDraft.nextPrizeLevel).toBe(4)
    expect(removedDraft.prizes.map((prize) => prize.prizeLevel)).toEqual([
      1, 2, 3,
    ])
    expect(removedDraft.prizes[2].name).toBe('Tier 3')

    const removedPersisted = removeDraftPrize(
      [persisted1, persisted2, draft3],
      persisted2.id,
      3
    )
    expect(removedPersisted.nextPrizeLevel).toBe(4)
    expect(removedPersisted.prizes.map((prize) => prize.prizeLevel)).toEqual([
      1, 3,
    ])
  })

  it('accepts only enabled percentage weights totaling 100', () => {
    expect(
      enabledPrizeWeightsTotal100([
        { enabled: true, weight: '0.5' },
        { enabled: true, weight: '99.5' },
        { enabled: false, weight: '900' },
      ])
    ).toBe(true)
    expect(
      enabledPrizeWeightsTotal100([
        { enabled: true, weight: '0.5' },
        { enabled: true, weight: '99.4' },
      ])
    ).toBe(false)
    expect(
      enabledPrizeWeightsTotal100([
        { enabled: true, weight: '0.5' },
        { enabled: true, weight: '99.6' },
      ])
    ).toBe(false)
  })

  it('links all ranges and clears rates after the base boundary changes', () => {
    const update = updatePityBoundary(
      {
        pity_base_end: 10,
        pity_stage_1_start: 11,
        pity_stage_1_end: 15,
        pity_stage_2_start: 16,
        pity_stage_2_end: 19,
      },
      'base',
      5
    )

    expect(update.pity_stage_1_start).toBe(6)
    expect(update.pity_stage_1_end).toBe(15)
    expect(update.pity_stage_2_start).toBe(16)
    expect(update.pity_stage_2_end).toBe(19)
    expect(update.pity_stage_1_rates).toHaveLength(10)
    expect(update.pity_stage_2_rates).toHaveLength(4)
    expect(update.pity_stage_1_rates?.every(Number.isNaN)).toBe(true)
    expect(update.pity_stage_2_rates?.every(Number.isNaN)).toBe(true)
    expect(update.pity_hard_threshold).toBe(20)
  })

  it('keeps later stages continuous and moves hard pity with stage two', () => {
    const config = {
      pity_base_end: 5,
      pity_stage_1_start: 6,
      pity_stage_1_end: 15,
      pity_stage_2_start: 16,
      pity_stage_2_end: 19,
    }
    const stage1 = updatePityBoundary(config, 'stage1', 12)
    expect(stage1.pity_stage_2_start).toBe(13)
    expect(stage1.pity_stage_2_rates).toHaveLength(7)
    expect(stage1.pity_hard_threshold).toBe(20)

    const stage2 = updatePityBoundary(
      { ...config, pity_stage_1_end: 12, pity_stage_2_start: 13 },
      'stage2',
      17
    )
    expect(stage2.pity_stage_2_rates).toHaveLength(5)
    expect(stage2.pity_hard_threshold).toBe(18)
  })
})
