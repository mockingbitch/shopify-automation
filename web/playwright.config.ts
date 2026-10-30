/// <reference types="node" />
import { defineConfig, devices } from '@playwright/test'

// Smoke tests for the dashboard. They need the API (make run) and Mailpit
// (make up) running; the Nuxt dev server is started automatically.
export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  retries: process.env.CI ? 1 : 0,
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://localhost:3100',
    trace: 'retain-on-failure',
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  webServer: process.env.E2E_BASE_URL
    ? undefined
    : { command: 'npm run dev', url: 'http://localhost:3100/login', reuseExistingServer: true, timeout: 120_000 },
})
