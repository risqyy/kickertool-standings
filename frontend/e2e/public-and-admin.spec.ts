import { expect, test } from '@playwright/test'

test('public shell remains available without admin request', async ({ page }) => {
  const adminRequests: string[] = []
  page.on('request', request => { if (request.url().includes('/api/admin')) adminRequests.push(request.url()) })
  await page.goto('/standings')
  await expect(page.getByRole('heading', { name: 'Spieler-Ranking' })).toBeVisible()
  expect(adminRequests).toHaveLength(0)
})

test('public ranking keeps the same responsive hierarchy and exposes trend text', async ({ page }) => {
  await page.route('**/api/v1/public/rankings**', async route => {
    await route.fulfill({
      contentType: 'application/json',
      body: JSON.stringify({
        items: [
          { rank: 1, trend: 'up', name: 'Up Player', includedTournamentCount: 2, gamesPlayed: 4, totalPoints: '12.50', pointsPerGame: '3.13', goalDifference: 7 },
          { rank: 2, trend: 'down', name: 'Down Player', includedTournamentCount: 2, gamesPlayed: 4, totalPoints: '11.00', pointsPerGame: '2.75', goalDifference: 3 },
          { rank: 3, trend: 'same', name: 'Same Player', includedTournamentCount: 1, gamesPlayed: 2, totalPoints: '5.00', pointsPerGame: '2.50', goalDifference: 0 },
          { rank: 4, trend: 'new', name: 'New Player', includedTournamentCount: 1, gamesPlayed: 1, totalPoints: '1.00', pointsPerGame: '1.00', goalDifference: -1 }
        ],
        lastSyncAt: null,
        availableYears: [],
        selectedYear: null
      })
    })
  })
  await page.goto('/standings')

  const expectedColumns = ['Platz', 'Tendenz', 'Name', 'Spiele', 'Turniere', 'Tordifferenz', 'Punkte', 'Punkte/Spiel']
  await expect(page.getByRole('table').getByRole('columnheader')).toHaveText(expectedColumns)
  await expect(page.getByLabel('Tendenz: Aufgestiegen')).toHaveCount(2)
  await expect(page.getByLabel('Tendenz: Abgestiegen')).toHaveCount(2)
  await expect(page.getByLabel('Tendenz: Gleich geblieben')).toHaveCount(2)
  await expect(page.getByLabel('Tendenz: Neu')).toHaveCount(2)
  await expect(page.getByText('13', { exact: true })).toHaveCount(2)

  await page.setViewportSize({ width: 390, height: 844 })
  const mobileCard = page.getByRole('article').first()
  await expect(mobileCard).toBeVisible()
  await expect(mobileCard.locator('dt')).toHaveText(expectedColumns)

  await page.setViewportSize({ width: 1280, height: 900 })
  await expect(page.getByRole('table')).toBeVisible()
})
