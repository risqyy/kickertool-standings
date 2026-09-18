import { expect, it, vi } from 'vitest'
import { getPlayers, getPlayerStatistics } from './client'

it('requests paginated identities and the statistics snapshot with admin credentials', async () => {
  const tournaments = [{ standingRank: 6, standingSourceId: 'result-42', standingKey: 'final/result-42', sourcePlayerName: 'Source Alias', url: 'https://example.test/final' }, { standingRank: null, standingSourceId: null, standingKey: '', sourcePlayerName: '' }]
  const fetchMock = vi.fn().mockResolvedValueOnce(new Response(JSON.stringify({ items: [], total: 0, page: 2, limit: 25 }))).mockResolvedValueOnce(new Response(JSON.stringify({ player: { id: 7 }, tournaments, corrections: [] })))
  vi.stubGlobal('fetch', fetchMock)
  await expect(getPlayers({ q: 'Alex & Co', state: 'all', page: 2, limit: 25 })).resolves.toMatchObject({ page: 2 })
  await expect(getPlayerStatistics(7)).resolves.toMatchObject({ player: { id: 7 }, tournaments })
  expect(fetchMock).toHaveBeenNthCalledWith(1, '/api/v1/admin/players?q=Alex+%26+Co&state=all&page=2&limit=25', { credentials: 'include' })
  expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/admin/players/7/statistics', { credentials: 'include' })
})
