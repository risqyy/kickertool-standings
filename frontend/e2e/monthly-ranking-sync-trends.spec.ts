import { expect, test } from '@playwright/test'

test('monthly filters retain independent trends and sync status on desktop and mobile', async ({ page }, testInfo) => {
  await page.route('**/api/v1/public/rankings**', async route => {
    const params = new URL(route.request().url()).searchParams
    await route.fulfill({ contentType: 'application/json', body: JSON.stringify({
      items: [
        { rank: 1, trend: 'up', name: 'Anna', includedTournamentCount: 2, gamesPlayed: 4, totalPoints: '42.00', pointsPerGame: '10.50', goalDifference: -2, pointsPerGameTrend: 'up', goalDifferenceTrend: 'down' },
        { rank: 2, trend: 'new', name: 'Ben', includedTournamentCount: 1, gamesPlayed: null, totalPoints: '12.00', pointsPerGame: null, goalDifference: 0, pointsPerGameTrend: 'unavailable', goalDifferenceTrend: 'same' }
      ],
      lastSyncAt: '2026-09-12T10:34:00Z', lastSyncStatus: 'ok',
      availableYears: [2026], availableMonths: [{ year: 2026, month: 9 }, { year: 2026, month: 8 }],
      selectedYear: params.has('year') ? Number(params.get('year')) : null,
      selectedMonth: params.has('month') ? Number(params.get('month')) : null
    }) })
  })
  await page.goto('/standings')
  await page.getByLabel('Zeitraum', { exact: true }).selectOption('2026-9')
  await expect(page.getByText('Aktiver Zeitraum:')).toContainText('Monatsrangliste September 2026')
  await expect(page.getByText('Letzte Synchronisierung:')).toContainText('12.09.2026, 12:34')
  const table = page.getByRole('table')
  await expect(table.getByLabel('Punkte/Spiel: Gestiegen', { exact: true })).toBeVisible()
  await expect(table.getByLabel('Tordifferenz: Gefallen', { exact: true })).toBeVisible()
  await expect(table.getByLabel('Punkte/Spiel: Kein Vergleich', { exact: true })).toBeVisible()
  await expect(table.getByLabel('Tordifferenz: Unverändert', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('monthly-desktop.png'), fullPage: true })
  await page.getByRole('button', { name: 'Punkte/Spiel', exact: true }).click()
  await page.getByLabel('Spieler suchen', { exact: true }).fill('Anna')
  await expect(table.getByLabel('Punkte/Spiel: Gestiegen', { exact: true })).toBeVisible()
  await expect(table.getByLabel('Tordifferenz: Gefallen', { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  const card = page.getByRole('article', { name: 'Anna, Monatsrangliste September 2026' })
  await expect(card).toBeVisible()
  await expect(card.getByLabel('Punkte/Spiel: Gestiegen', { exact: true })).toBeVisible()
  await expect(card.getByLabel('Tordifferenz: Gefallen', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('monthly-mobile.png'), fullPage: true })
  await expect(page.getByText('Letzte Synchronisierung:')).toContainText('12.09.2026, 12:34')
  await page.getByLabel('Zeitraum', { exact: true }).selectOption('2026')
  await expect(page.getByText('Aktiver Zeitraum:')).toContainText('Jahresrangliste 2026')
  await page.getByLabel('Zeitraum', { exact: true }).selectOption('')
  await expect(page.getByText('Aktiver Zeitraum:')).toContainText('Ewigen Tabelle')
})
