import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { RankingPage } from './ranking-page'

describe('RankingPage', () => {
  it('loads only the public endpoint and sorts missing values last', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [
      { rank: 1, name: 'Player One', includedTournamentCount: 2, gamesPlayed: null, totalPoints: null, pointsPerGame: null, goalDifference: 0 },
      { rank: 2, name: 'Player Two', includedTournamentCount: 1, gamesPlayed: 4, totalPoints: '12.50', pointsPerGame: '3.13', goalDifference: 2 }
    ], lastSyncAt: null }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)
    expect((await screen.findAllByText('Player Two')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('—')).length).toBeGreaterThan(0)
    await userEvent.click(screen.getByRole('button', { name: 'Punkte' }))
    await waitFor(() => expect(screen.getAllByRole('row')[1]).toHaveTextContent('Player Two'))
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(fetchMock.mock.calls[0][0]).toBe('/api/v1/public/rankings')
    expect(fetchMock.mock.calls[0][0]).not.toContain('/api/admin')
  })
})
