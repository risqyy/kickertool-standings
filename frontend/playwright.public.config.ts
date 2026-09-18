import { defineConfig } from '@playwright/test'

// Public and admin refresh regressions use mocked API responses and need no crawler,
// production credentials, or database. Run with:
// npx playwright test --config playwright.public.config.ts
export default defineConfig({
  testDir: './e2e',
  testMatch: ['public-and-admin.spec.ts', 'monthly-ranking-sync-trends.spec.ts', 'tournament-refresh.spec.ts'],
  use: { baseURL: 'http://127.0.0.1:5175', screenshot: 'only-on-failure' },
  webServer: {
    command: 'npm run dev -- --host 127.0.0.1 --port 5175 --strictPort',
    url: 'http://127.0.0.1:5175',
    reuseExistingServer: false,
    timeout: 30000
  }
})
