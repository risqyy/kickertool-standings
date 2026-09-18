import { expect, test } from '@playwright/test'

for (const viewport of [{ width: 1280, height: 900 }, { width: 390, height: 844 }]) {
  test(`single tournament refresh: running, failure and retry at ${viewport.width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize(viewport)
    let state = 'running'
    let starts = 0
    let listings = 0
    const tournament = {
      id: 7, source: 'kickertool_api', sourceId: '7', sourceKey: '7', name: 'Freitagsturnier',
      date: '2026-09-18T10:00:00Z', startTime: null, endTime: null, status: 'finished',
      isLive: false, entryType: 'monster_dyp', includedInRanking: false, inclusionVersion: 1,
      inclusionUpdatedAt: null, inclusionReason: '', url: 'https://example.test/7', participants: 8,
      standingCount: 8, playerCount: 8, standingsComplete: true, lastSyncError: false,
      standingsSyncedAt: '2026-09-18T10:00:00Z', lastSeenAt: '2026-09-18T10:00:00Z'
    }
    const job = () => ({ id: 'job-7', tournamentId: 7, state, startedAt: '2026-09-18T10:00:00Z', finishedAt: state === 'running' ? null : '2026-09-18T10:01:00Z' })
    await page.route('**/api/v1/admin/session', route => route.fulfill({ json: { authenticated: true, csrf_token: 'csrf-test' } }))
    await page.route('**/api/v1/admin/tournaments?**', route => {
      listings++
      return route.fulfill({ json: { items: [{ ...tournament, lastSyncError: state === 'failed' }], page: 1, limit: 25, total: 1, last_sync_at: null } })
    })
    await page.route('**/api/v1/admin/tournaments/7/refresh', route => {
      expect(route.request().method()).toBe('POST')
      expect(route.request().headers()['x-csrf-token']).toBe('csrf-test')
      starts++
      return route.fulfill({ status: 202, json: { ...job(), state: 'running', finishedAt: null } })
    })
    await page.route('**/api/v1/admin/tournament-refreshes/job-7', route => route.fulfill({ json: job() }))
    await page.goto('/admin/tournaments')
    const container = viewport.width < 768 ? page.getByRole('article') : page.getByRole('table')
    const refresh = container.getByRole('button', { name: 'Freitagsturnier: Ergebnisse neu laden' })
    await expect(refresh).toBeVisible()
    await page.getByRole('button', { name: 'Liste neu laden', exact: true }).click()
    await expect(refresh).toBeVisible()
    expect(starts).toBe(0)
    await refresh.click()
    await expect(refresh).toBeDisabled()
    await expect(refresh).toHaveText('Wird abgeglichen …')
    state = 'failed'
    await expect(page.getByRole('alert')).toContainText('Der bisherige vollständige Stand bleibt erhalten')
    await expect(refresh).toBeEnabled()
    await expect(container.getByLabel('Freitagsturnier im Ranking')).not.toBeChecked()
    await expect(container.getByText('Fehlerhaft', { exact: true })).toBeVisible()
    state = 'succeeded'
    await refresh.click()
    await expect(page.getByRole('status')).toContainText('Turnierergebnisse erfolgreich abgeglichen')
    await expect(refresh).toBeEnabled()
    await expect(container.getByText('Fehlerhaft', { exact: true })).toHaveCount(0)
    await expect(container.getByText('Zuletzt geprüft:')).toContainText('18.09.26, 12:00')
    expect(starts).toBe(2)
    expect(listings).toBeGreaterThanOrEqual(4)
    await page.screenshot({ path: testInfo.outputPath('refresh-success.png'), fullPage: true })
  })
}
