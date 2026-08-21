import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { getTournaments } from '@/api/client'
import type { Tournament } from '@/api/types'
import { TournamentManagementPage } from './tournament-management-page'

vi.mock('@/api/client', () => ({
  getTournaments: vi.fn(),
  setTournamentInclusion: vi.fn()
}))

vi.mock('@/app/providers', () => ({
  useAdminSession: () => ({ status: 'authenticated', csrf: 'test-csrf', retry: vi.fn() })
}))

const getTournamentsMock = vi.mocked(getTournaments)

const tournament: Tournament = {
  id: 7,
  source: 'kickertool_html',
  sourceId: 'failed-7',
  sourceKey: 'failed-7',
  name: 'Fehlerturnier',
  date: '2026-08-20T00:00:00Z',
  startTime: null,
  endTime: null,
  status: 'finished',
  isLive: false,
  entryType: 'monster_dyp',
  includedInRanking: true,
  inclusionUpdatedAt: null,
  inclusionVersion: 1,
  inclusionReason: '',
  url: 'https://example.test/tournaments/failed-7',
  participants: 8,
  standingCount: 0,
  playerCount: 0,
  standingsComplete: false,
  lastSyncError: true,
  standingsSyncedAt: null,
  lastSeenAt: '2026-08-20T00:00:00Z'
}

describe('TournamentManagementPage', () => {
  beforeEach(() => {
    getTournamentsMock.mockResolvedValue({ items: [tournament], page: 1, limit: 25, total: 1, last_sync_at: null })
  })

  it('shows a visible failed standings state in desktop and mobile layouts without changing ranking inclusion', async () => {
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)

    expect(await screen.findAllByText('Fehlerhaft')).toHaveLength(2)
    expect(screen.getAllByTitle('Standings-Synchronisierung fehlgeschlagen')).toHaveLength(2)
    expect(screen.getAllByLabelText('Fehlerturnier im Ranking')).toHaveLength(2)
    expect(screen.getAllByLabelText('Fehlerturnier im Ranking').every(input => (input as HTMLInputElement).checked)).toBe(true)
  })
})
