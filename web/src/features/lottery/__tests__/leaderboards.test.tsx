import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/lib/api'

import { LotteryLeaderboards } from '../leaderboards'

function payload() {
  return {
    success: true,
    data: {
      period: 'all' as const,
      timezone: 'Asia/Shanghai',
      count_start: 0,
      count_end: 0,
      lucky: {
        rows: [
          { rank: 1, display_name: 'y***', value: 1, pity_step: 1 },
          { rank: 2, display_name: 'a***', value: 1, pity_step: 2 },
          { rank: 3, display_name: 'b***', value: 1, pity_step: 3 },
        ],
        current_user: {
          rank: 12,
          display_name: 'm***',
          value: 1,
          pity_step: 8,
        },
      },
      draw_count: {
        rows: [{ rank: 1, display_name: 'd***', value: 24 }],
        current_user: { rank: 4, display_name: 'm***', value: 10 },
      },
      hard_pity: {
        rows: [],
      },
    },
  }
}

function renderBoard() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <LotteryLeaderboards />
    </QueryClientProvider>
  )
}

afterEach(() => vi.restoreAllMocks())

describe('LotteryLeaderboards', () => {
  it('renders the three boards, medals, and a persistent current-user slot', async () => {
    vi.spyOn(api, 'get').mockResolvedValue({ data: payload() })
    renderBoard()

    expect(await screen.findByText('y***')).toBeVisible()
    expect(screen.getAllByLabelText('1st')).toHaveLength(2)
    expect(screen.getAllByLabelText('2nd')).toHaveLength(1)
    expect(screen.getAllByLabelText('3rd')).toHaveLength(1)
    expect(screen.getAllByText('Your best record')).toHaveLength(3)
    expect(screen.getByText('#12')).toBeVisible()
    expect(screen.getByText('Not ranked yet')).toBeVisible()
    expect(screen.getByText('No leaderboard records yet')).toBeVisible()
  })

  it('reloads the selected draw-count period and shows the error state', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({ data: payload() })
    renderBoard()
    await screen.findByText('y***')

    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'This week' }))
    await waitFor(() =>
      expect(get).toHaveBeenCalledWith('/api/lottery/leaderboards', {
        params: { period: 'week' },
      })
    )

    get.mockRejectedValueOnce(new Error('unavailable'))
    await userEvent
      .setup()
      .click(screen.getByRole('button', { name: 'This month' }))
    expect(
      await screen.findByText('Unable to load lottery rankings')
    ).toBeVisible()
  })
})
