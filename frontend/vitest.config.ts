import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import path from 'node:path'

export default defineConfig({ plugins: [react()], resolve: { alias: { '@': path.resolve(process.cwd(), 'src') } }, test: { environment: 'jsdom', globals: true, include: ['src/**/*.{test,spec}.{ts,tsx}'], setupFiles: ['./src/test/setup.ts'] } })
