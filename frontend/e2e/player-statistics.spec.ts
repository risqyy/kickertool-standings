import { expect, test } from '@playwright/test'

for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
  test(`admin player list and contribution detail at ${viewport.width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport)
    const summary = { id: 7, displayName: 'Alex Beispiel', active: true, mergedIntoPlayerId: null }
    await page.route('**/api/v1/admin/session', route => route.fulfill({ json: { authenticated: true, csrf_token: 'test-csrf' } }))
    await page.route('**/api/v1/admin/players?**', route => {
      const query = new URL(route.request().url()).searchParams
      expect(query.get('state')).toBe('all')
      return route.fulfill({ json: { items: query.get('q') === 'Niemand' ? [] : [summary, { ...summary, id: 8, displayName: 'Früherer Name', active: false, mergedIntoPlayerId: 7 }], page: 1, total: 2, limit: 25 } })
    })
    await page.route('**/api/v1/admin/players/7/statistics', route => route.fulfill({ json: {
      requestedPlayer: summary,
      player: { ...summary, aliases: [], canonicalNameKey: 'alex beispiel', tournamentCount: 1, gamesPlayed: 4, totalPointsCents: 1250, pointsPerGameCents: 313, goalDifference: null },
      tournaments: [
        { id: 1, tournamentId: 10, name: 'Sommerturnier', date: '2026-08-31T22:30:00Z', source: 'kickertool_api', sourceId: 'summer', standingRank: 6, standingSourceId: 'source-result-42', standingKey: 'summer/final/source-alias', sourcePlayerName: 'Source Alias', url: 'https://example.test/summer/groups/final/standings', status: 'finished', reason: 'counted', gamesPlayed: 4, totalPointsCents: 1000, pointsPerGameCents: 250, goalDifference: null },
        { id: 2, tournamentId: 11, name: 'Abwesenheit', date: '2026-09-18T18:00:00Z', source: 'kickertool_api', sourceId: 'autumn', standingRank: null, standingSourceId: null, standingKey: '', sourcePlayerName: '', url: '', status: 'finished', reason: 'zero_games', gamesPlayed: 0, totalPointsCents: 0, pointsPerGameCents: null, goalDifference: 0 }
      ],
      corrections: [{ effective: true, correction: { id: 1, playerId: 7, playerKey: 'alex beispiel', effectiveDate: '2026-09-01', effectiveYear: 2026, tournamentCountDelta: 0, gamesPlayedDelta: 0, pointsCentsDelta: 250, goalDifferenceDelta: 0, reason: 'Ergebnis nachgetragen', administrator: 'admin', createdAt: '2026-09-18T10:00:00Z', status: 'active', revokedAt: null, revision: 1, version: 1 } }],
      computedAt: '2026-09-18T10:00:00Z'
    } }))
    await page.goto('/admin/players')
    await expect(page.getByRole('heading', { name: 'Spieler', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Spieler', exact: true }).filter({ visible: true })).toBeVisible()
    await page.getByRole('textbox', { name: 'Spieler suchen' }).fill('Niemand')
    await expect(page.getByText('Keine Spieler für diese Auswahl.')).toBeVisible()
    await page.getByRole('textbox', { name: 'Spieler suchen' }).fill('Alex')
    const list = viewport.width < 768 ? page.locator('article').first() : page.getByRole('table')
    await list.getByRole('link', { name: 'Alex Beispiel' }).click()
    await expect(page).toHaveURL('/admin/players/7')
    await expect(page.getByRole('heading', { name: 'Gesamtstatistik · Alex Beispiel' })).toBeVisible()
    await expect(page.getByText('12.50', { exact: true })).toBeVisible()
    await expect(page.getByText('01.09.2026 · finished')).toBeVisible()
    await expect(page.getByText('Nicht gewertet: 0 Spiele')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Korrektur #1 · Wirksam' })).toBeVisible()
    await expect(page.getByText('+2.50', { exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: /Ergebnis in der Quelle öffnen/ })).toHaveAttribute('rel', 'noopener noreferrer')
    await expect(page.getByText('Platz 6', { exact: true })).toBeVisible()
    await expect(page.getByText('Source Alias', { exact: true })).toBeVisible()
    await expect(page.getByText('source-result-42', { exact: true })).toBeVisible()
    await expect(page.getByText('summer/final/source-alias', { exact: true })).toBeVisible()
    await expect(page.getByText('Unbekannt', { exact: true })).toHaveCount(4)
    await expect(page.getByRole('link', { name: /Ergebnis in der Quelle öffnen/ })).toHaveAttribute('href', 'https://example.test/summer/groups/final/standings')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
    await page.screenshot({ path: testInfo.outputPath('player-statistics.png'), fullPage: true })
    await page.getByRole('link', { name: 'Zur Spielerübersicht' }).click()
    await expect(page).toHaveURL('/admin/players?q=Alex')
    await expect(page.getByRole('textbox', { name: 'Spieler suchen' })).toHaveValue('Alex')
    await page.goto('/admin/players/7')
    await expect(page.getByRole('heading', { name: 'Gesamtstatistik · Alex Beispiel' })).toBeVisible()
  })
}
