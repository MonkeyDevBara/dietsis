import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { VitePWA } from 'vite-plugin-pwa'
import { defineConfig } from 'vitest/config'

// The Go API listens on 127.0.0.1:8787 in development (docs/ARCHITECTURE.md#development-lokal).
// Proxying keeps the browser on a single origin, so the session cookie and CSRF rules behave like production.
const apiTarget = process.env.DIETSIS_API_URL ?? 'http://127.0.0.1:8787'

export default defineConfig({
  plugins: [
    react(),
    tailwindcss(),
    VitePWA({
      // 'prompt': a waiting service worker activates only when the user taps "Muat ulang".
      registerType: 'prompt',
      includeAssets: ['favicon.svg', 'apple-touch-icon.png'],
      manifest: {
        name: 'Dietsis',
        short_name: 'Dietsis',
        description: 'Teman diet harianmu. Catat makanan, air, dan berat badan, bahkan saat offline.',
        lang: 'id',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        orientation: 'portrait',
        background_color: '#f6f8f5',
        theme_color: '#23604a',
        icons: [
          { src: 'icon-192.png', sizes: '192x192', type: 'image/png' },
          { src: 'icon-512.png', sizes: '512x512', type: 'image/png' },
          { src: 'maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' },
        ],
      },
      workbox: {
        // App shell only. Data lives in IndexedDB; the API is never cached by the service worker.
        globPatterns: ['**/*.{js,css,html,svg,png,webmanifest}'],
        navigateFallback: '/index.html',
        navigateFallbackDenylist: [/^\/api\//],
        cleanupOutdatedCaches: true,
      },
    }),
  ],
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
    sourcemap: false,
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
  },
})
