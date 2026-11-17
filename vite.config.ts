import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The Go API listens on 127.0.0.1:8787 in development (docs/ARCHITECTURE.md#development-lokal).
// Proxying keeps the browser on a single origin, so the session cookie and CSRF rules behave like production.
const apiTarget = process.env.DIETSIS_API_URL ?? 'http://127.0.0.1:8787'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // Fixed, non-default port: the dev origin must match DIETSIS_PUBLIC_ORIGIN and Google's authorized origins.
    port: 5178,
    strictPort: true,
    proxy: {
      '/api': { target: apiTarget, changeOrigin: false },
    },
  },
  build: {
    target: 'es2022',
    sourcemap: true,
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'node',
  },
})
