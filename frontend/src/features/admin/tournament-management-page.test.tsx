import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getTournaments, getTournamentRefresh, startTournamentRefresh } from '@/api/client'
import type { Tournament } from '@/api/types'
import { TournamentManagementPage } from './tournament-management-page'

vi.mock('@/api/client', async importOriginal => ({
  ...await importOriginal<typeof import('@/api/client')>(),
  getTournaments: vi.fn(),
  setTournamentInclusion: vi.fn(),
  startTournamentRefresh: vi.fn(),
  getTournamentRefresh: vi.fn()
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
    vi.resetAllMocks()
    getTournamentsMock.mockResolvedValue({ items: [tournament], page: 1, limit: 25, total: 1, last_sync_at: null })
    vi.mocked(startTournamentRefresh).mockResolvedValue({ id: 'job-7', tournamentId: 7, state: 'running', startedAt: '2026-09-18T10:00:00Z', finishedAt: null })
  })

  it('shows a visible failed standings state in desktop and mobile layouts without changing ranking inclusion', async () => {
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)

    expect(await screen.findAllByText('Fehlerhaft')).toHaveLength(2)
    expect(screen.getAllByTitle('Standings-Synchronisierung fehlgeschlagen')).toHaveLength(2)
    expect(screen.getAllByLabelText('Fehlerturnier im Ranking')).toHaveLength(2)
    expect(screen.getAllByLabelText('Fehlerturnier im Ranking').every(input => (input as HTMLInputElement).checked)).toBe(true)
  })

  it.each([0, 1])('refreshes the selected tournament in layout %s and reloads after success', async layout => {
    let finish!: (job: Awaited<ReturnType<typeof getTournamentRefresh>>) => void
    vi.mocked(getTournamentRefresh).mockReturnValue(new Promise(resolve => { finish = resolve }))
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)
    const buttons = await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' })
    fireEvent.click(buttons[layout])
    await waitFor(() => expect(getTournamentRefresh).toHaveBeenCalledWith('job-7'))
    expect(startTournamentRefresh).toHaveBeenCalledWith('test-csrf', 7)
    expect(buttons.every(button => (button as HTMLButtonElement).disabled)).toBe(true)
    expect(screen.getAllByText('Wird abgeglichen …')).toHaveLength(2)
    finish({ id: 'job-7', tournamentId: 7, state: 'succeeded', startedAt: '2026-09-18T10:00:00Z', finishedAt: '2026-09-18T10:01:00Z' })
    expect(await screen.findByText(/Turnierergebnisse erfolgreich abgeglichen/)).toBeVisible()
    await waitFor(() => expect(getTournamentsMock).toHaveBeenCalledTimes(2))
    expect((await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' })).every(button => !(button as HTMLButtonElement).disabled)).toBe(true)
  })

  it('shows a failed job and permits a successful retry', async () => {
    vi.mocked(getTournamentRefresh).mockResolvedValue({ id: 'job-7', tournamentId: 7, state: 'failed', startedAt: '2026-09-18T10:00:00Z', finishedAt: '2026-09-18T10:01:00Z' })
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' }))[0])
    expect(await screen.findByText(/Der bisherige vollständige Stand bleibt erhalten/)).toBeVisible()
    vi.mocked(getTournamentRefresh).mockResolvedValue({ id: 'job-8', tournamentId: 7, state: 'succeeded', startedAt: '2026-09-18T10:00:00Z', finishedAt: '2026-09-18T10:01:00Z' })
    fireEvent.click((await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' }))[0])
    expect(await screen.findByText(/Turnierergebnisse erfolgreich abgeglichen/)).toBeVisible()
    expect(startTournamentRefresh).toHaveBeenCalledTimes(2)
  })

  it('explains a busy crawler without pretending that a job started', async () => {
    vi.mocked(startTournamentRefresh).mockRejectedValue(new ApiError(409, 'sync_busy'))
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' }))[0])
    expect(await screen.findByText(/Ein Abgleich läuft bereits/)).toBeVisible()
    expect(getTournamentRefresh).not.toHaveBeenCalled()
  })

  it('distinguishes an unknown status from a failed source fetch', async () => {
    vi.mocked(getTournamentRefresh).mockRejectedValue(new Error('offline'))
    render(<MemoryRouter><TournamentManagementPage /></MemoryRouter>)
    fireEvent.click((await screen.findAllByRole('button', { name: 'Fehlerturnier: Ergebnisse neu laden' }))[0])
    expect(await screen.findByText(/Der Abgleich kann noch laufen/)).toBeVisible()
  })
})
