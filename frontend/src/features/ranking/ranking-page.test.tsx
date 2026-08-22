import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { RankingPage } from './ranking-page'

describe('RankingPage', () => {
  it('loads only the public endpoint and sorts missing values last', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [
      { rank: 1, name: 'Player One', includedTournamentCount: 2, gamesPlayed: null, totalPoints: null, pointsPerGame: null, goalDifference: 0 },
      { rank: 2, name: 'Player Two', includedTournamentCount: 1, gamesPlayed: 4, totalPoints: '12.50', pointsPerGame: '3.13', goalDifference: 2 }
    ], lastSyncAt: null, availableYears: [2025, 2024] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
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

  it('keeps the public hierarchy on mobile and renders trend accessibly with display-only point rounding', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [
      { rank: 1, trend: 'up', name: 'Trend Player', includedTournamentCount: 2, gamesPlayed: 4, totalPoints: '12.50', pointsPerGame: '3.13', goalDifference: 7 }
    ], lastSyncAt: null, availableYears: [], selectedYear: null }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)

    expect((await screen.findAllByText('Trend Player')).length).toBeGreaterThan(0)
    expect(screen.getAllByLabelText('Tendenz: Aufgestiegen')).toHaveLength(2)
    expect(screen.queryByText('12.50')).not.toBeInTheDocument()
    expect(screen.getAllByText('13').length).toBeGreaterThan(0)
    const mobileCard = screen.getByRole('article')
    expect(Array.from(mobileCard.querySelectorAll('dt')).map(node => node.textContent)).toEqual(['Platz', 'Tendenz', 'Name', 'Spiele', 'Turniere', 'Tordifferenz', 'Punkte', 'Punkte/Spiel'])
  })

  it('keeps the period and search controls in one accessible, aligned filter group', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], lastSyncAt: null, availableYears: [2025] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)

    const period = await screen.findByRole('combobox', { name: 'Zeitraum' })
    const search = screen.getByRole('textbox', { name: 'Spieler suchen' })

    expect(screen.getByRole('group', { name: 'Filter' })).toBeInTheDocument()
    expect(period).toHaveClass('h-11')
    expect(search).toHaveClass('h-11')
    await userEvent.type(search, 'Müller')
    expect(search).toHaveValue('Müller')
  })

  it('sorts exact decimal point strings without changing the trend value', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [
      { rank: 1, trend: 'same', name: 'Lower', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '900719925474099.90', pointsPerGame: '1.00', goalDifference: 0 },
      { rank: 2, trend: 'up', name: 'Higher', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '900719925474099.91', pointsPerGame: '1.00', goalDifference: 0 }
    ], lastSyncAt: null, availableYears: [], selectedYear: null }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)
    await screen.findAllByText('Higher')
    await userEvent.click(screen.getByRole('button', { name: 'Punkte' }))
    await waitFor(() => expect(screen.getAllByRole('row')[1]).toHaveTextContent('Lower'))
    expect(screen.getAllByLabelText('Tendenz: Gleich geblieben')).toHaveLength(2)
    expect(screen.getAllByLabelText('Tendenz: Aufgestiegen')).toHaveLength(2)
  })

  it('keeps canonical rank and trend fixed while searching and client-sorting', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [
      { rank: 1, trend: 'up', name: 'Player A', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '10.00', pointsPerGame: '10.00', goalDifference: 1 },
      { rank: 2, trend: 'down', name: 'Player B', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '20.00', pointsPerGame: '20.00', goalDifference: 2 }
    ], lastSyncAt: null, availableYears: [], selectedYear: null }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)
    await screen.findAllByText('Player A')
    const headers = screen.getAllByRole('columnheader')
    expect(headers[0]).toHaveAttribute('aria-sort', 'ascending')
    expect(headers[0].querySelector('button')).not.toHaveAttribute('aria-sort')
    expect(headers[1]).toHaveAttribute('aria-sort', 'none')

    await userEvent.type(screen.getByRole('textbox', { name: 'Spieler suchen' }), 'B')
    await userEvent.click(screen.getByRole('button', { name: 'Punkte' }))
    const row = screen.getAllByRole('row')[1]
    expect(row).toHaveTextContent('Player B')
    expect(row).toHaveTextContent('2')
    expect(within(row).getByLabelText('Tendenz: Abgestiegen')).toBeInTheDocument()
  })

  it('loads a selected year and keeps the active period visible', async () => {
    const fetchMock = vi.fn().mockImplementation((path: string) => {
      const year = new URL(path, 'http://localhost').searchParams.get('year')
      const payload = year === '2025'
        ? { items: [{ rank: 1, name: 'Year Player', includedTournamentCount: 1, gamesPlayed: 2, totalPoints: '5.00', pointsPerGame: '2.50', goalDifference: 1 }], lastSyncAt: null, availableYears: [2025, 2024], selectedYear: 2025 }
        : { items: [], lastSyncAt: null, availableYears: [2025, 2024], selectedYear: null }
      return Promise.resolve(new Response(JSON.stringify(payload), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)

    const period = await screen.findByRole('combobox', { name: /Zeitraum/i })
    expect(screen.getByRole('heading', { name: /Rangliste.*Ewigen Tabelle/i })).toBeInTheDocument()
    await userEvent.selectOptions(period, '2025')

    expect(fetchMock).toHaveBeenLastCalledWith('/api/v1/public/rankings?year=2025', expect.anything())
    expect((await screen.findAllByText('Year Player')).length).toBeGreaterThan(0)
    expect(screen.getByRole('heading', { name: /Rangliste.*Jahresrangliste 2025/i })).toBeInTheDocument()
  })

  it('ignores a stale response when periods are switched quickly', async () => {
    let resolve2025: ((response: Response) => void) | undefined
    let resolve2024: ((response: Response) => void) | undefined
    const fetchMock = vi.fn().mockImplementation((path: string) => {
      if (!path.includes('year=')) return Promise.resolve(new Response(JSON.stringify({ items: [], lastSyncAt: null, availableYears: [2025, 2024], selectedYear: null }), { status: 200 }))
      return new Promise<Response>(resolve => {
        if (path.includes('year=2025')) resolve2025 = resolve
        else resolve2024 = resolve
      })
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)

    const period = await screen.findByRole('combobox', { name: /Zeitraum/i })
    await screen.findByRole('option', { name: /Jahresrangliste 2025/i })
    await userEvent.selectOptions(period, '2025')
    await userEvent.selectOptions(period, '2024')
    resolve2024?.(new Response(JSON.stringify({ items: [{ rank: 1, name: 'New Year Player', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '1.00', pointsPerGame: '1.00', goalDifference: 0 }], lastSyncAt: null, availableYears: [2025, 2024], selectedYear: 2024 }), { status: 200 }))
    expect((await screen.findAllByText('New Year Player')).length).toBeGreaterThan(0)
    resolve2025?.(new Response(JSON.stringify({ items: [{ rank: 1, name: 'Stale Overall Player', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '1.00', pointsPerGame: '1.00', goalDifference: 0 }], lastSyncAt: null, availableYears: [2025, 2024], selectedYear: 2025 }), { status: 200 }))

    await waitFor(() => expect(screen.queryByText('Stale Overall Player')).not.toBeInTheDocument())
    expect((await screen.findAllByText('New Year Player')).length).toBeGreaterThan(0)
  })

  it('shows an error and retries the active period', async () => {
    let attempts = 0
    const fetchMock = vi.fn().mockImplementation(() => {
      attempts += 1
      if (attempts < 2) return Promise.reject(new Error('network error'))
      return Promise.resolve(new Response(JSON.stringify({ items: [{ rank: 1, name: 'Recovered Player', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '1.00', pointsPerGame: '1.00', goalDifference: 0 }], lastSyncAt: null, availableYears: [], selectedYear: null }), { status: 200 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    render(<RankingPage />)

    expect(await screen.findByRole('alert')).toHaveTextContent('Rangliste konnte nicht geladen werden')
    await userEvent.click(screen.getByRole('button', { name: /Erneut versuchen/i }))
    expect((await screen.findAllByText('Recovered Player')).length).toBeGreaterThan(0)
  })
})
