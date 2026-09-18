import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Link, MemoryRouter, Route, Routes, useLocation } from 'react-router-dom'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, getPlayers, getPlayerStatistics } from '@/api/client'
import type { ManualRankingCorrection, PlayerPage, PlayerStatistics } from '@/api/types'
import { PlayersPage } from './players-page'
import { PlayerStatisticsPage } from './player-statistics-page'

vi.mock('@/api/client', async importOriginal => ({ ...await importOriginal<typeof import('@/api/client')>(), getPlayers: vi.fn(), getPlayerStatistics: vi.fn() }))
const profile = { id: 2, displayName: 'Alex Beispiel', canonicalNameKey: 'alex beispiel', active: true, aliases: [], tournamentCount: 1, gamesPlayed: 5, totalPointsCents: 1200, pointsPerGameCents: 240, goalDifference: null }
const summary = { id: 2, displayName: 'Alex Beispiel', active: true, mergedIntoPlayerId: null }
const page: PlayerPage = { items: [summary, { id: 1, displayName: 'Alter Name', active: false, mergedIntoPlayerId: 2 }], total: 26, limit: 25, page: 1 }
const contribution = { id: 10, tournamentId: 4, name: 'Septemberturnier', date: '2026-08-31T22:30:00Z', source: 'kickertool_api', sourceId: 'external-id', url: 'https://example.test/4', status: 'finished', reason: 'counted' as const, gamesPlayed: 4, totalPointsCents: 1000, pointsPerGameCents: 250, goalDifference: null }
const correction: ManualRankingCorrection = { id: 1, playerId: 2, playerKey: 'alex beispiel', effectiveDate: '2026-09-01', effectiveYear: 2026, tournamentCountDelta: 0, gamesPlayedDelta: 1, pointsCentsDelta: 200, goalDifferenceDelta: 0, reason: 'Ergebnis korrigiert', administrator: 'admin', createdAt: '2026-09-01T10:00:00Z', status: 'active', revokedAt: null, revision: 1, version: 1 }
const statistics: PlayerStatistics = { requestedPlayer: summary, player: profile, tournaments: [contribution], corrections: [{ correction, effective: true }], computedAt: '2026-09-18T12:00:00Z' }
function Location() { return <span data-testid="location">{useLocation().search}</span> }
function renderPages(path = '/admin/players') {
  return render(<MemoryRouter initialEntries={[path]}><Link to="/admin/players/3">Anderer Spieler</Link><Location /><Routes><Route path="/admin/players" element={<PlayersPage />} /><Route path="/admin/players/:id" element={<PlayerStatisticsPage />} /></Routes></MemoryRouter>)
}
beforeEach(() => { vi.resetAllMocks(); vi.mocked(getPlayers).mockResolvedValue(page); vi.mocked(getPlayerStatistics).mockResolvedValue(statistics) })

describe('admin player list', () => {
  it('loads all identities, searches in URL, resets pagination and navigates to details', async () => {
    const user = userEvent.setup()
    renderPages('/admin/players?q=Alex&state=merged&page=2')
    await screen.findByRole('table')
    expect(getPlayers).toHaveBeenLastCalledWith({ q: 'Alex', state: 'merged', page: 2, limit: 25 })
    fireEvent.change(screen.getByRole('textbox', { name: 'Spieler suchen' }), { target: { value: 'Andere' } })
    await waitFor(() => expect(getPlayers).toHaveBeenLastCalledWith({ q: 'Andere', state: 'merged', page: 1, limit: 25 }))
    expect(screen.getByTestId('location')).not.toHaveTextContent('page=')
    await user.selectOptions(screen.getByRole('combobox', { name: 'Identitäten' }), 'all')
    await screen.findByRole('table')
    await user.click(screen.getByRole('button', { name: 'Weiter' }))
    await waitFor(() => expect(getPlayers).toHaveBeenLastCalledWith({ q: 'Andere', state: 'all', page: 2, limit: 25 }))
    await user.click(within(await screen.findByRole('table')).getByRole('link', { name: 'Alex Beispiel' }))
    expect(await screen.findByRole('heading', { level: 1, name: 'Alex Beispiel' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Zur Spielerübersicht' })).toHaveAttribute('href', '/admin/players?q=Andere&state=all&page=2')
  })

  it('ignores an old list response after a search and recovers from failure', async () => {
    let resolveOld!: (value: PlayerPage) => void
    vi.mocked(getPlayers).mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockRejectedValueOnce(new Error('offline')).mockResolvedValue({ items: [], page: 1, total: 0, limit: 25 })
    renderPages()
    expect(screen.getByRole('status')).toHaveTextContent('geladen')
    fireEvent.change(screen.getByRole('textbox', { name: 'Spieler suchen' }), { target: { value: 'neu' } })
    await screen.findByRole('alert')
    await act(async () => { resolveOld(page) })
    expect(screen.queryByRole('table')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Erneut versuchen' }))
    expect(await screen.findByText('Keine Spieler für diese Auswahl.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Weiter' })).toBeDisabled()
  })
})

describe('player contribution detail', () => {
  it('shows server totals, all reasons, unknown values, Berlin dates and correction states', async () => {
    vi.mocked(getPlayerStatistics).mockResolvedValue({ ...statistics, requestedPlayer: { ...summary, id: 1, displayName: 'Alter Name', active: false, mergedIntoPlayerId: 2 }, tournaments: [contribution, { ...contribution, id: 11, name: 'Abwesend', reason: 'zero_games', gamesPlayed: 0, url: 'javascript:alert(1)' }, { ...contribution, id: 12, reason: 'excluded' }, { ...contribution, id: 13, reason: 'superseded' }], corrections: [{ correction, effective: true }, { correction: { ...correction, id: 2, effectiveDate: '2099-01-01' }, effective: false }, { correction: { ...correction, id: 3, status: 'revoked' }, effective: false }, { correction: { ...correction, id: 4, status: 'replaced' }, effective: false }] })
    renderPages('/admin/players/1')
    expect(await screen.findByRole('heading', { name: 'Alter Name' })).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('Diese Identität wurde zusammengeführt')
    expect(screen.getByRole('link', { name: 'Alex Beispiel' })).toHaveAttribute('href', '/admin/players/2')
    for (const label of ['Gewertet', 'Nicht gewertet: 0 Spiele', 'Turnier ausgeschlossen', 'Überholtes Ergebnis']) expect(screen.getByText(label, { exact: true })).toBeInTheDocument()
    expect(screen.getAllByText('01.09.2026 · finished')).toHaveLength(4)
    expect(screen.getAllByText('—').length).toBeGreaterThan(0)
    expect(screen.getByText('12.00')).toBeInTheDocument()
    expect(screen.getByText('2.40')).toBeInTheDocument()
    expect(screen.getAllByRole('link', { name: /Turnier in der Quelle öffnen/ })).toHaveLength(3)
    for (const label of ['Wirksam', 'Geplant', 'Aufgehoben', 'Ersetzt']) expect(screen.getByRole('heading', { name: new RegExp('Korrektur #\\d · ' + label) })).toBeInTheDocument()
    expect(screen.getAllByText('+2.00')).toHaveLength(4)
  })

  it('handles direct unknown IDs, failures with retry, and players without contributions', async () => {
    vi.mocked(getPlayerStatistics).mockRejectedValueOnce(new Error('offline')).mockRejectedValueOnce(new ApiError(404, 'not found')).mockResolvedValue({ ...statistics, player: { ...profile, id: 3, tournamentCount: 0 }, requestedPlayer: { ...summary, id: 3 }, tournaments: [], corrections: [] })
    renderPages('/admin/players/99')
    expect(await screen.findByRole('alert')).toHaveTextContent('nicht geladen')
    await userEvent.click(screen.getByRole('button', { name: 'Erneut versuchen' }))
    expect(await screen.findByRole('heading', { name: 'Spieler nicht gefunden' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('link', { name: 'Anderer Spieler' }))
    expect(await screen.findByText('Keine Turnierergebnisse vorhanden.')).toBeInTheDocument()
    expect(screen.getByText('Keine manuellen Korrekturen vorhanden.')).toBeInTheDocument()
  })

  it('does not let the previous player response replace the selected player', async () => {
    let resolveOld!: (value: PlayerStatistics) => void
    vi.mocked(getPlayerStatistics).mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockResolvedValue({ ...statistics, requestedPlayer: { ...summary, id: 3, displayName: 'Neuer Spieler' } })
    renderPages('/admin/players/2')
    await userEvent.click(screen.getByRole('link', { name: 'Anderer Spieler' }))
    await screen.findByRole('heading', { name: 'Neuer Spieler' })
    await act(async () => resolveOld(statistics))
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Neuer Spieler')
  })
})
