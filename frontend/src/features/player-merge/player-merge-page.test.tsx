import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { searchPlayers } from '@/api/client'
import type { Player } from '@/api/types'
import { PlayerMergePage } from './player-merge-page'

vi.mock('@/api/client', () => ({
  ApiError: class ApiError extends Error { status = 500 },
  searchPlayers: vi.fn(),
  previewMerge: vi.fn(),
  confirmMerge: vi.fn()
}))
vi.mock('@/app/providers', () => ({ useAdminSession: () => ({ csrf: 'csrf-token' }) }))
vi.mock('./player-merge-history', () => ({ PlayerMergeHistory: () => null }))

const player: Player = {
  id: 42,
  displayName: 'Neue Spielerin',
  canonicalNameKey: 'neue spielerin',
  aliases: ['Neue Spielerin'],
  active: true,
  tournamentCount: 0,
  gamesPlayed: null,
  totalPointsCents: null,
  pointsPerGameCents: null,
  goalDifference: null
}

const searchPlayersMock = vi.mocked(searchPlayers)

describe('PlayerMergePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('selects a newly created player from the follow-up playerId', async () => {
    searchPlayersMock.mockResolvedValue([player])
    render(<MemoryRouter initialEntries={['/admin/players/merge?playerId=42&name=Neue%20Spielerin']}><PlayerMergePage /></MemoryRouter>)

    expect(await screen.findByRole('heading', { name: 'Neue Spielerin' })).toBeInTheDocument()
    expect(searchPlayersMock).toHaveBeenCalledWith('Neue Spielerin')
  })

  it('offers player creation after an empty merge search', async () => {
    searchPlayersMock.mockResolvedValue([])
    render(<MemoryRouter initialEntries={['/admin/players/merge?name=Unbekannt']}><PlayerMergePage /></MemoryRouter>)

    const link = await screen.findAllByRole('link', { name: /Spieler anlegen/ })
    expect(link[0]).toHaveAttribute('href', '/admin/players/new?name=Unbekannt')
  })
})
