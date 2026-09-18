import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import {
  updateLotteryAdmin,
  type LotteryAdminData,
} from '../api'

const config: LotteryAdminData['config'] = {
  enabled: true,
  threshold_display_amount: 20,
  prizes: [
    {
      id: 'first',
      name: 'First prize',
      prize_level: 1,
      weight: 1,
      amount: 2,
      enabled: true,
      sort_order: 0,
    },
  ],
  pity_enabled: false,
  pity_target_prize_level: 1,
  pity_base_rate: 0.5,
  pity_base_end: 10,
  pity_stage_1_start: 11,
  pity_stage_1_end: 15,
  pity_stage_1_rates: [2, 4, 6, 8, 10],
  pity_stage_2_start: 16,
  pity_stage_2_end: 19,
  pity_stage_2_rates: [30, 50, 70, 90],
  pity_hard_threshold: 20,
}

afterEach(() => vi.restoreAllMocks())

describe('lottery admin API', () => {
  it('rejects an HTTP 200 business failure without advancing the saved state', async () => {
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'First prize must be enabled' },
    })

    await expect(updateLotteryAdmin(config)).rejects.toThrow(
      'First prize must be enabled'
    )
    expect(api.put).toHaveBeenCalledWith('/api/lottery/admin/', config)
  })

  it('returns a confirmed business success', async () => {
    vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: true, data: config },
    })

    await expect(updateLotteryAdmin(config)).resolves.toEqual({
      success: true,
      data: config,
    })
  })
})
