import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { RankingPage } from './ranking-page'

describe('monthly ranking selection', () => {
  it('loads month and year together, preserves search/sort/mobile cards and returns to annual/overall rankings', async () => {
    const fetchMock = vi.fn().mockImplementation((path: string) => {
      const params = new URL(path, 'http://localhost').searchParams
      const year = params.get('year')
      const month = params.get('month')
      return Promise.resolve(new Response(JSON.stringify({
        items: month === '9' ? [
          { rank: 1, name: 'Anna', trend: 'up', includedTournamentCount: 2, gamesPlayed: 10, totalPoints: '30.00', pointsPerGame: '3.00', goalDifference: 8 },
          { rank: 2, name: 'Ben', trend: 'down', includedTournamentCount: 1, gamesPlayed: 10, totalPoints: '10.00', pointsPerGame: '1.00', goalDifference: 2 }
        ] : [],
        lastSyncAt: null, availableYears: [2026], availableMonths: month === '8' ? [{ year: 2026, month: 9 }] : [{ year: 2026, month: 9 }, { year: 2026, month: 8 }],
        selectedYear: year ? Number(year) : null, selectedMonth: month ? Number(month) : null
      }), { status: 200 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)
    await screen.findByRole('option', { name: 'Monatsrangliste September 2026' })
    const period = screen.getByRole('combobox', { name: 'Zeitraum' })
    await userEvent.selectOptions(period, '2026-9')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/public/rankings?year=2026&month=9', expect.anything())
    await screen.findByRole('heading', { name: 'Rangliste – Monatsrangliste September 2026' })
    expect(screen.getByRole('article', { name: 'Anna, Monatsrangliste September 2026' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Punkte' }))
    expect(screen.getAllByRole('row')[1]).toHaveTextContent('Ben')
    await userEvent.type(screen.getByRole('textbox', { name: 'Spieler suchen' }), 'Anna')
    const card = screen.getByRole('article')
    expect(card).toHaveTextContent('Anna')
    expect(within(card).getByLabelText('Tendenz: Aufgestiegen')).toBeInTheDocument()
    await userEvent.clear(screen.getByRole('textbox', { name: 'Spieler suchen' }))
    await userEvent.selectOptions(period, '2026-8')
    expect(await screen.findByText('Für den Zeitraum Monatsrangliste August 2026 liegen keine Ranking-Ergebnisse vor.')).toBeInTheDocument()
    expect(period).toHaveValue('2026-8')
    expect(within(period).getByRole('option', { selected: true })).toHaveTextContent('Monatsrangliste August 2026')
    await userEvent.selectOptions(period, '2026')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/public/rankings?year=2026', expect.anything())
    await userEvent.selectOptions(period, '')
    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/public/rankings', expect.anything())
  })

  it('ignores an older month response during quick switches within the same year', async () => {
    let september: ((response: Response) => void) | undefined
    const metadata = { lastSyncAt: null, availableYears: [2026], availableMonths: [{ year: 2026, month: 9 }, { year: 2026, month: 8 }] }
    vi.stubGlobal('fetch', vi.fn().mockImplementation((path: string) => {
      if (path.includes('month=9')) return new Promise<Response>(resolve => { september = resolve })
      return Promise.resolve(new Response(JSON.stringify({ ...metadata, items: [], selectedYear: path.includes('month=8') ? 2026 : null, selectedMonth: path.includes('month=8') ? 8 : null }), { status: 200 }))
    }))
    render(<RankingPage />)
    await screen.findByRole('option', { name: 'Monatsrangliste September 2026' })
    const period = screen.getByRole('combobox', { name: 'Zeitraum' })
    await userEvent.selectOptions(period, '2026-9')
    await userEvent.selectOptions(period, '2026-8')
    await screen.findByText('Für den Zeitraum Monatsrangliste August 2026 liegen keine Ranking-Ergebnisse vor.')
    september?.(new Response(JSON.stringify({ ...metadata, items: [], selectedYear: 2026, selectedMonth: 9 }), { status: 200 }))
    expect(period).toHaveValue('2026-8')
    expect(screen.getByRole('heading', { name: 'Rangliste – Monatsrangliste August 2026' })).toBeInTheDocument()
  })
})
