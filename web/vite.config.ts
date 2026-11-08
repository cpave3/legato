/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'
import path from 'path'
import { SERVER_PAGES } from './src/lib/serverPages'
import { randomUUID } from 'crypto'
import type { Plugin } from 'vite'

const frontendBuildID = randomUUID()

function frontendBuildIdentity(): Plugin {
  return {
    name: 'frontend-build-identity',
    generateBundle() {
      this.emitFile({ type: 'asset', fileName: 'build-id', source: `${frontendBuildID}\n` })
    },
  }
}

export default defineConfig({
  define: {
    __LEGATO_FRONTEND_BUILD_ID__: JSON.stringify(frontendBuildID),
  },
  plugins: [
    react(),
    tailwindcss(),
    frontendBuildIdentity(),
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['favicon.ico', 'favicon-16x16.png', 'favicon-32x32.png', 'apple-touch-icon.png', 'pwa-192x192.png', 'pwa-512x512.png'],
      manifest: {
        name: 'Legato',
        short_name: 'Legato',
        description: 'Agent orchestration board',
        theme_color: '#0a0a0f',
        background_color: '#0a0a0f',
        display: 'standalone',
        scope: '/',
        start_url: '/',
        icons: [
          {
            src: 'pwa-192x192.png',
            sizes: '192x192',
            type: 'image/png',
          },
          {
            src: 'pwa-512x512.png',
            sizes: '512x512',
            type: 'image/png',
          },
          {
            src: 'pwa-512x512.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        globPatterns: ['**/*.{js,css,html,svg,png,woff2}'],
        navigateFallback: 'index.html',
        // Addresses the server answers itself: images opened in a tab of
        // their own, and the OAuth callback (see src/lib/serverPages.ts).
        navigateFallbackDenylist: SERVER_PAGES,
        skipWaiting: true,
        clientsClaim: true,
        runtimeCaching: [
          {
            urlPattern: /^\/api\//,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'api-cache',
              expiration: { maxEntries: 50, maxAgeSeconds: 300 },
            },
          },
          {
            urlPattern: /^\/health/,
            handler: 'NetworkFirst',
            options: {
              cacheName: 'health-cache',
              expiration: { maxEntries: 1, maxAgeSeconds: 60 },
            },
          },
        ],
      },
    }),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  build: {
    outDir: '../internal/server/static/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:3080',
      '/agent-ui-images': 'http://localhost:3080',
      '/plan-images': 'http://localhost:3080',
      '/review-images': 'http://localhost:3080',
      '/ws': {
        target: 'ws://localhost:3080',
        ws: true,
      },
      '/health': 'http://localhost:3080',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    // Tests that render a long table into jsdom take seconds, and more on a
    // shared CI runner or beside another suite; 5s (the default) failed them.
    testTimeout: 20_000,
    hookTimeout: 20_000,
  },
})
