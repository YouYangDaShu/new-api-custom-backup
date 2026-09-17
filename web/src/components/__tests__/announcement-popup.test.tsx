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
*/
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { AnnouncementPopup } from '@/components/announcement-popup'
import { announcementDismissSessionKey } from '@/components/announcement-popup-utils'
import {
  getUnreadAnnouncements,
  markAnnouncementsRead,
  type UnreadAnnouncementItem,
} from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    getUnreadAnnouncements: vi.fn(),
    markAnnouncementsRead: vi.fn(),
  }
})

vi.mock('@/components/ui/scroll-area', () => ({
  ScrollArea: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}))

const mockedGetUnreadAnnouncements = vi.mocked(getUnreadAnnouncements)
const mockedMarkAnnouncementsRead = vi.mocked(markAnnouncementsRead)

function setAuth(userId: number | null, sid: string | null) {
  const state = useAuthStore.getState()
  useAuthStore.setState({
    auth: {
      ...state.auth,
      user:
        userId === null
          ? null
          : {
              id: userId,
              username: `user-${userId}`,
              role: 1,
            },
      session:
        sid === null
          ? null
          : {
              sid,
              current: true,
              login_method: 'password',
              ip: '127.0.0.1',
              user_agent: 'vitest',
              created_at: 1,
              last_active_at: 1,
              expires_at: 2,
            },
    },
  })
}

function renderPopup(items: UnreadAnnouncementItem[]) {
  mockedGetUnreadAnnouncements.mockResolvedValue({
    success: true,
    data: { items, total: items.length },
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <AnnouncementPopup />
    </QueryClientProvider>
  )
  return client
}

describe('announcementDismissSessionKey', () => {
  it('isolates the temporary dismissal by user and login session', () => {
    const a = announcementDismissSessionKey(12, 'session-a')
    const b = announcementDismissSessionKey(13, 'session-a')
    const c = announcementDismissSessionKey(12, 'session-b')
    expect(a).toBe('announcement-popup-dismissed-session:12:session-a')
    expect(b).not.toBe(a)
    expect(c).not.toBe(a)
  })
})

describe('AnnouncementPopup', () => {
  beforeEach(() => {
    sessionStorage.clear()
    vi.clearAllMocks()
    setAuth(null, null)
  })

  afterEach(() => {
    setAuth(null, null)
  })

  it('does not request announcements before authentication is ready', async () => {
    renderPopup([])
    await act(async () => undefined)
    expect(mockedGetUnreadAnnouncements).not.toHaveBeenCalled()
  })

  it('shows 1 / N and supports next and previous navigation', async () => {
    setAuth(12, 'session-a')
    renderPopup([
      { key: 'id:1', content: 'First announcement' },
      { key: 'id:2', content: 'Second announcement' },
    ])
    const user = userEvent.setup()

    expect(await screen.findByText('First announcement')).toBeVisible()
    expect(screen.getByText('1 / 2')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Next' }))
    expect(screen.getByText('Second announcement')).toBeVisible()
    expect(screen.getByText('2 / 2')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Previous' }))
    expect(screen.getByText('First announcement')).toBeVisible()
  })

  it('keeps Later local to the current user login session', async () => {
    setAuth(12, 'session-a')
    renderPopup([{ key: 'id:1', content: 'Read later announcement' }])
    const user = userEvent.setup()

    expect(await screen.findByText('Read later announcement')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Later' }))
    await waitFor(() =>
      expect(
        screen.queryByText('Read later announcement')
      ).not.toBeInTheDocument()
    )
    expect(
      sessionStorage.getItem(announcementDismissSessionKey(12, 'session-a'))
    ).toBe('1')
    expect(mockedMarkAnnouncementsRead).not.toHaveBeenCalled()
  })

  it('marks all read permanently and removes the temporary dismissal', async () => {
    setAuth(12, 'session-a')
    const key = announcementDismissSessionKey(12, 'session-a')
    sessionStorage.setItem(key, '1')
    mockedMarkAnnouncementsRead.mockResolvedValue({
      success: true,
      data: { marked: 1 },
    })
    sessionStorage.removeItem(key)
    renderPopup([{ key: 'id:1', content: 'Permanent announcement' }])
    const user = userEvent.setup()

    expect(await screen.findByText('Permanent announcement')).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Mark all as read' }))
    await waitFor(() =>
      expect(mockedMarkAnnouncementsRead).toHaveBeenCalledWith({ all: true })
    )
    expect(sessionStorage.getItem(key)).toBeNull()
  })
})
