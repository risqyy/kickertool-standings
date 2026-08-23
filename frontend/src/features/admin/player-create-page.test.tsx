import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, createPlayer } from '@/api/client'
import type { Player } from '@/api/types'
import { PlayerCreatePage, validatePlayerDisplayName } from './player-create-page'

vi.mock('@/api/client', () => ({
  ApiError: class ApiError extends Error {
    status: number
    payload: unknown
    constructor(status: number, message: string, payload: unknown = null) {
      super(message)
      this.status = status
      this.payload = payload
    }
  },
  createPlayer: vi.fn()
}))

vi.mock('@/app/providers', () => ({ useAdminSession: () => ({ csrf: 'csrf-token' }) }))

const player: Player = {
  id: 42,
  displayName: 'Alex Müller',
  canonicalNameKey: 'alex müller',
  aliases: [],
  active: true,
  tournamentCount: 0,
  gamesPlayed: null,
  totalPointsCents: null,
  pointsPerGameCents: null,
  goalDifference: null
}

const createPlayerMock = vi.mocked(createPlayer)

describe('PlayerCreatePage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createPlayerMock.mockResolvedValue({ player, created: true, createdAt: '2026-08-23T10:00:00Z', administrator: 'admin', origin: 'manual' })
  })

  it('validates empty, invalid and too-long names before submitting', () => {
    expect(validatePlayerDisplayName('')).toMatchObject({ displayName: expect.stringContaining('eingeben') })
    expect(validatePlayerDisplayName('x')).toMatchObject({ displayName: expect.stringContaining('mindestens 2') })
    expect(validatePlayerDisplayName('!!!')).toMatchObject({ displayName: expect.stringContaining('Namenszeichen') })
    expect(validatePlayerDisplayName('x'.repeat(121))).toMatchObject({ displayName: expect.stringContaining('höchstens 120') })
  })

  it('creates a player after the name is explicitly repeated and offers the next actions', async () => {
    const user = userEvent.setup()
    render(<MemoryRouter><PlayerCreatePage /></MemoryRouter>)

    const name = screen.getByRole('textbox', { name: /^Anzeigename$/ })
    const confirmation = screen.getByRole('textbox', { name: /^Anzeigename bestätigen$/ })
    await user.type(name, 'Alex Müller')
    await user.type(confirmation, 'Alex Müller')
    await user.click(screen.getByRole('button', { name: 'Spieler anlegen' }))

    expect(createPlayerMock).toHaveBeenCalledWith('csrf-token', 'Alex Müller')
    expect(await screen.findByRole('status')).toHaveTextContent('wurde angelegt')
    expect(screen.getByRole('link', { name: /Ranking korrigieren/ })).toHaveAttribute('href', '/admin/players/corrections?playerId=42&name=Alex%20M%C3%BCller')
  })

  it('shows a conflict with the active player and recovery actions', async () => {
    const user = userEvent.setup()
    createPlayerMock.mockRejectedValue(new ApiError(409, 'player already exists', { player }))
    render(<MemoryRouter><PlayerCreatePage /></MemoryRouter>)

    await user.type(screen.getByRole('textbox', { name: /^Anzeigename$/ }), 'Alex Müller')
    await user.type(screen.getByRole('textbox', { name: /^Anzeigename bestätigen$/ }), 'Alex Müller')
    await user.keyboard('{Enter}')

    expect(await screen.findByText('Aktiver Spieler bereits vorhanden')).toBeInTheDocument()
    expect(screen.getByText('Alex Müller', { selector: 'strong' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Ranking korrigieren' })).toHaveAttribute('href', '/admin/players/corrections?playerId=42&name=Alex%20M%C3%BCller')
  })

  it('keeps the confirmation field keyboard and screen-reader accessible', async () => {
    const user = userEvent.setup()
    render(<MemoryRouter><PlayerCreatePage /></MemoryRouter>)
    const name = screen.getByRole('textbox', { name: /^Anzeigename$/ })
    const confirmation = screen.getByRole('textbox', { name: /^Anzeigename bestätigen$/ })
    expect(name).toHaveAttribute('aria-describedby', 'player-display-name-help')
    expect(confirmation).toHaveAttribute('autocomplete', 'off')
    await user.type(name, 'Alex Müller')
    await user.tab()
    expect(confirmation).toHaveFocus()
  })

  it('prefills the name passed from an empty player search', () => {
    render(<MemoryRouter initialEntries={['/admin/players/new?name=Neue%20Spielerin']}><PlayerCreatePage /></MemoryRouter>)

    expect(screen.getByRole('textbox', { name: /^Anzeigename$/ })).toHaveValue('Neue Spielerin')
    expect(screen.getByRole('textbox', { name: /^Anzeigename bestätigen$/ })).toHaveValue('')
  })
})
