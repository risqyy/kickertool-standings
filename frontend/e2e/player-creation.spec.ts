import { expect, test } from '@playwright/test'

test('manual player creation is keyboard accessible and offers direct follow-up actions', async ({ page }) => {
  let createPayload: Record<string, unknown> | undefined
  await page.route('**/api/v1/admin/session', route => route.fulfill({
    status: 200,
    contentType: 'application/json',
    body: JSON.stringify({ authenticated: true, csrf_token: 'e2e-csrf' })
  }))
  await page.route('**/api/v1/admin/players', route => {
    createPayload = route.request().postDataJSON() as Record<string, unknown>
    return route.fulfill({
      status: 201,
      contentType: 'application/json',
      body: JSON.stringify({
        created: true,
        createdAt: '2026-08-23T10:00:00Z',
        administrator: 'admin',
        origin: 'manual',
        player: {
          id: 42,
          displayName: 'Alex Müller',
          canonicalNameKey: 'alex müller',
          createdAt: '2026-08-23T10:00:00Z',
          createdBy: 'admin',
          origin: 'manual',
          aliases: ['Alex Müller'],
          active: true,
          tournamentCount: 0,
          gamesPlayed: null,
          totalPointsCents: null,
          pointsPerGameCents: null,
          goalDifference: null,
          rankingCorrectionVersion: 0
        }
      })
    })
  })

  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/admin/players/new?name=Alex%20M%C3%BCller')
  const name = page.getByRole('textbox', { name: /^Anzeigename$/ })
  const confirmation = page.getByRole('textbox', { name: /^Anzeigename bestätigen$/ })
  await expect(name).toHaveValue('Alex Müller')
  await expect(confirmation).toHaveValue('')

  await name.press('Tab')
  await expect(confirmation).toBeFocused()
  await confirmation.fill('Alex Müller')
  await confirmation.press('Enter')

  await expect.poll(() => createPayload).toEqual({ displayName: 'Alex Müller', confirmed: true })
  const success = page.getByRole('status')
  await expect(success).toContainText('wurde angelegt')
  await expect(success.getByRole('link', { name: /Ranking korrigieren/ })).toHaveAttribute('href', '/admin/players/corrections?playerId=42&name=Alex%20M%C3%BCller')
  await expect(success.getByRole('link', { name: /Zur Zusammenführung/ })).toHaveAttribute('href', '/admin/players/merge?playerId=42&name=Alex%20M%C3%BCller')
})
