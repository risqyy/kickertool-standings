import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import type { PlayerMergeAudit } from '@/api/types'
import { PlayerMergeHistory } from './player-merge-history'

const available: PlayerMergeAudit = {
  id: 11,
  sourcePlayerId: 1,
  targetPlayerId: 2,
  sourceDisplayName: 'Anna Alt',
  targetDisplayName: 'Berta Ziel',
  mergedAt: '2026-08-20T10:00:00Z',
  transferredAliases: 1,
  transferredSourceIdentities: 1,
  transferredAllocations: 2,
  deduplicatedAllocations: 0,
  actor: 'admin',
  reason: 'Doppelt angelegt',
  undoAvailable: true,
  undoUnavailableReason: '',
  undoneAt: null,
  undoneBy: '',
  undoReason: ''
}

const undone: PlayerMergeAudit = { ...available, id: 10, sourceDisplayName: 'Clara Alt', undoAvailable: false, undoneAt: '2026-08-21T08:00:00Z', undoneBy: 'admin', undoReason: 'Falsche Zuordnung' }
const legacy: PlayerMergeAudit = { ...available, id: 9, sourceDisplayName: 'Dora Alt', undoAvailable: false, undoUnavailableReason: 'Für diese ältere Zusammenführung fehlen vollständige Wiederherstellungsdaten.' }
const aggregate = { tournamentCount: 2, gamesPlayed: 6, totalPointsCents: 1250, pointsPerGameCents: 208, goalDifference: 3 }

function jsonResponse(value: unknown, status = 200) {
  return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } })
}

describe('PlayerMergeHistory', () => {
  it('shows audit states and requires an explicit confirmation before restoring both players', async () => {
    const restored = { ...available, undoAvailable: false, undoneAt: '2026-08-21T09:00:00Z', undoneBy: 'admin', undoReason: 'Versehentliche Zusammenführung' }
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ items: [available, undone, legacy] }))
      .mockResolvedValueOnce(jsonResponse({ token: 'undo-token', preview: { merge: available, sourceBefore: aggregate, targetBefore: aggregate } }))
      .mockResolvedValueOnce(jsonResponse({ merge: restored, sourceAfter: aggregate, targetAfter: aggregate, undoneAt: restored.undoneAt }))
      .mockResolvedValueOnce(jsonResponse({ items: [restored, undone, legacy] }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<PlayerMergeHistory csrf="csrf-token" refreshKey={0} />)

    expect(await screen.findByText('Kann rückgängig gemacht werden')).toBeInTheDocument()
    expect(screen.getByText('Rückgängig gemacht')).toBeInTheDocument()
    expect(screen.getByText('Nicht rückgängig machbar')).toBeInTheDocument()
    expect(screen.getByText(legacy.undoUnavailableReason)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Rückgängigmachen prüfen' }))
    expect(await screen.findByRole('alertdialog')).toHaveTextContent('Anna Alt und Berta Ziel')
    const confirm = screen.getByRole('button', { name: 'Rückgängig machen' })
    expect(confirm).toBeDisabled()

    await user.type(screen.getByLabelText('Grund für das Rückgängigmachen (optional)'), 'Versehentliche Zusammenführung')
    await user.click(screen.getByLabelText('Ich habe geprüft, dass diese Zusammenführung rückgängig gemacht werden soll.'))
    expect(confirm).toBeEnabled()
    await user.click(confirm)

    expect(await screen.findByText(/wurden wieder als getrennte Spieler hergestellt/)).toBeInTheDocument()
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(4))
    expect(fetchMock.mock.calls[1][0]).toBe('/api/v1/admin/players/merges/11/undo/preview')
    expect(fetchMock.mock.calls[2][0]).toBe('/api/v1/admin/players/merges/11/undo/confirm')
    const confirmBody = JSON.parse((fetchMock.mock.calls[2][1] as RequestInit).body as string)
    expect(confirmBody).toEqual({ token: 'undo-token', reason: 'Versehentliche Zusammenführung', confirmed: true })
    expect(new Headers((fetchMock.mock.calls[2][1] as RequestInit).headers).get('X-CSRF-Token')).toBe('csrf-token')
  })

  it('keeps the server explanation visible when a safe undo is no longer possible', async () => {
    const message = 'Die Spielerdaten haben sich seit der Zusammenführung geändert; ein vollständiges Rückgängigmachen ist nicht mehr sicher möglich.'
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse({ items: [available] }))
      .mockResolvedValueOnce(jsonResponse({ error: message, code: 'player_merge_undo_unavailable' }, 409))
    vi.stubGlobal('fetch', fetchMock)

    render(<PlayerMergeHistory csrf="csrf-token" refreshKey={0} />)
    await userEvent.click(await screen.findByRole('button', { name: 'Rückgängigmachen prüfen' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(message)
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })
})
