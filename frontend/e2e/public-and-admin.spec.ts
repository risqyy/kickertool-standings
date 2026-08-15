import { expect, test } from '@playwright/test'

test('public shell remains available without admin request', async ({ page }) => {
  const adminRequests: string[] = []
  page.on('request', request => { if (request.url().includes('/api/admin')) adminRequests.push(request.url()) })
  await page.goto('/standings')
  await expect(page.getByRole('heading', { name: 'Spieler-Ranking' })).toBeVisible()
  expect(adminRequests).toHaveLength(0)
})
