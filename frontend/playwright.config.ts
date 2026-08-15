import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  use: { baseURL: 'http://127.0.0.1:5173' },
  webServer: [
    { command: 'go run ./backend/cmd/crawler', cwd: '..', url: 'http://127.0.0.1:8080/healthz', reuseExistingServer: true, timeout: 120000 },
    { command: 'npm run dev -- --host 127.0.0.1', cwd: '.', url: 'http://127.0.0.1:5173', reuseExistingServer: true, timeout: 120000 }
  ]
})
