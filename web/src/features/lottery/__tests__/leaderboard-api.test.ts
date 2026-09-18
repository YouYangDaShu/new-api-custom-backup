import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { getLotteryLeaderboards } from '../api'

afterEach(() => vi.restoreAllMocks())

describe('lottery leaderboard API', () => {
  it('requests the selected period and returns the leaderboard payload', async () => {
    const payload = {
      success: true,
      data: {
        period: 'week',
        timezone: 'Asia/Shanghai',
        count_start: 1,
        count_end: 2,
        lucky: { rows: [], current_user: undefined },
        draw_count: { rows: [] },
        hard_pity: { rows: [] },
      },
    }
    vi.spyOn(api, 'get').mockResolvedValue({ data: payload })

    await expect(getLotteryLeaderboards('week')).resolves.toEqual(payload)
    expect(api.get).toHaveBeenCalledWith('/api/lottery/leaderboards', {
      params: { period: 'week' },
    })
  })

  it('rejects an HTTP 200 business failure', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({
      data: { success: false, message: 'leaderboard unavailable' },
    })

    await expect(getLotteryLeaderboards('all')).rejects.toThrow(
      'leaderboard unavailable'
    )
  })
})
