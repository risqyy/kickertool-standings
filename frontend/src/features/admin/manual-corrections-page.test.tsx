import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { confirmManualCorrection, listManualCorrections, previewManualCorrection, revokeManualCorrection, searchPlayers } from '@/api/client'
import type { ManualRankingCorrection, ManualRankingCorrectionPreview, Player } from '@/api/types'
import { ManualCorrectionsPage } from './manual-corrections-page'

vi.mock('@/app/providers', () => ({ useAdminSession: () => ({ csrf: 'csrf-token' }) }))
vi.mock('@/api/client', () => ({
  ApiError: class ApiError extends Error { status = 0; payload: unknown = null },
  confirmManualCorrection: vi.fn(),
  listManualCorrections: vi.fn(),
  previewManualCorrection: vi.fn(),
  revokeManualCorrection: vi.fn(),
  searchPlayers: vi.fn()
}))

const player: Player = { id: 7, displayName: 'Test Spieler', canonicalNameKey: 'test spieler', aliases: [], active: true, tournamentCount: 2, gamesPlayed: 10, totalPointsCents: 1000, pointsPerGameCents: 100, goalDifference: 3, rankingCorrectionVersion: 3 }
const activeCorrection: ManualRankingCorrection = { id: 9, playerId: 7, playerKey: 'test spieler', effectiveDate: '2026-08-20', effectiveYear: 2026, tournamentCountDelta: 1, gamesPlayedDelta: 1, pointsCentsDelta: 100, goalDifferenceDelta: 1, reason: 'Nachtrag', administrator: 'admin', createdAt: '2026-08-20T00:00:00Z', status: 'active', revokedAt: null, revision: 1, version: 3 }
const revokedCorrection: ManualRankingCorrection = { ...activeCorrection, id: 10, status: 'revoked', revokedAt: '2026-08-21T00:00:00Z', revokedBy: 'admin', revocationReason: 'Fehleingabe' }
const replacedCorrection: ManualRankingCorrection = { ...activeCorrection, id: 11, status: 'replaced', replacedByCorrectionId: 9 }
const preview: ManualRankingCorrectionPreview = { token: 'preview-token', player, correction: activeCorrection, before: { tournamentCount: 2, gamesPlayed: 10, totalPointsCents: 1000, pointsPerGameCents: 100, goalDifference: 3 }, after: { tournamentCount: 3, gamesPlayed: 11, totalPointsCents: 1100, pointsPerGameCents: 100, goalDifference: 4 }, expectedVersion: 3 }

describe('ManualCorrectionsPage', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.mocked(searchPlayers).mockResolvedValue([player])
    vi.mocked(listManualCorrections).mockResolvedValue({ items: [activeCorrection, revokedCorrection, replacedCorrection], version: 3 })
    vi.mocked(previewManualCorrection).mockResolvedValue(preview)
    vi.mocked(confirmManualCorrection).mockResolvedValue({ correction: activeCorrection, before: preview.before, after: preview.after, version: 4 })
    vi.mocked(revokeManualCorrection).mockResolvedValue({ correction: { ...activeCorrection, status: 'revoked' }, before: preview.before, after: preview.before, version: 5 })
  })

  async function choosePlayer(user: ReturnType<typeof userEvent.setup>) {
    await user.type(screen.getByRole('combobox', { name: 'Spieler suchen' }), 'Test')
    await user.click(await screen.findByRole('option', { name: /Test Spieler/ }))
    expect(await screen.findByText(/Ausgewählt:/)).toBeInTheDocument()
  }

  it('requires a year matching the effective date before sending a preview', async () => {
    const user = userEvent.setup()
    render(<ManualCorrectionsPage />)
    await choosePlayer(user)

    const yearInput = screen.getByRole('spinbutton', { name: /Kalenderjahr/ })
    await user.clear(yearInput)
    await user.type(yearInput, '2025')
    await user.click(screen.getByRole('button', { name: 'Vorschau erstellen' }))

    expect(await screen.findByText('Das Kalenderjahr muss zum Datum passen (2026).')).toBeInTheDocument()
    expect(previewManualCorrection).not.toHaveBeenCalled()
  })

  it('shows correction impact and only exposes undo for active entries', async () => {
    const user = userEvent.setup()
    render(<ManualCorrectionsPage />)
    await choosePlayer(user)
    await user.type(screen.getByRole('textbox', { name: /Grund/ }), 'Nachtrag')
    await user.click(screen.getByRole('button', { name: 'Vorschau erstellen' }))
    await user.click(await screen.findByRole('button', { name: 'Prüfen und bestätigen' }))

    const confirmation = await screen.findByRole('alertdialog')
    expect(confirmation).toHaveTextContent('Test Spieler')
    expect(confirmation).toHaveTextContent('Kalenderjahr 2026')
    expect(confirmation).toHaveTextContent('Turniere (Delta)+1')
    expect(confirmation).toHaveTextContent('Gesamtwertung – Vorher')
    expect(confirmation).toHaveTextContent('Gesamtwertung – Nachher')
    await user.click(screen.getByRole('button', { name: 'Abbrechen' }))

    expect(screen.getAllByRole('button', { name: 'Rückgängig machen' })).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: 'Rückgängig machen' }))
    const revokeDialog = await screen.findByRole('alertdialog')
    expect(revokeDialog).toHaveTextContent('Kalenderjahr 2026')
    expect(revokeDialog).toHaveTextContent('Gesamtwertung – Vorher')
    expect(screen.getAllByRole('button', { name: 'Rückgängig machen' }).at(-1)).toBeDisabled()

    await user.type(screen.getByRole('textbox', { name: /Pflichtgrund für das Rückgängigmachen/ }), 'Fehleingabe')
    await user.click(screen.getAllByRole('button', { name: 'Rückgängig machen' }).at(-1)!)
    expect(await screen.findByText('Korrektur revisionssicher aufgehoben.')).toBeInTheDocument()
    expect(revokeManualCorrection).toHaveBeenCalledWith('csrf-token', 7, 9, 3, 'Fehleingabe')
  })
})
