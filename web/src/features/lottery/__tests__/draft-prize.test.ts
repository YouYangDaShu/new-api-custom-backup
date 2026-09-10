import { describe, expect, it } from 'vitest'

import { createDraftPrize } from '../draft-prize'

describe('lottery draft prize ids', () => {
  it('numbers fresh tiers sequentially', () => {
    expect(createDraftPrize([]).id).toBe('tier-1')
    expect(createDraftPrize([{ id: 'tier-1' }]).id).toBe('tier-2')
  })

  it('never reuses an id that survived a middle-tier deletion', () => {
    const next = createDraftPrize([{ id: 'tier-2' }])

    expect(next.id).toBe('tier-3')
    expect(next.name).toBe('Tier 3')
  })

  it('skips occupied ids until it finds a free one', () => {
    const next = createDraftPrize([
      { id: 'tier-2' },
      { id: 'tier-3' },
      { id: 'custom' },
    ])

    expect(next.id).toBe('tier-4')
  })
})
