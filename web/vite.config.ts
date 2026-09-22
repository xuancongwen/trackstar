import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vitest/config'

const backend = process.env.TRACKER_DEV_BACKEND ?? 'http://127.0.0.1:3000'

export default defineConfig({
  plugins: [svelte()],
  // web/static is copied verbatim into dist (it also carries dist/.gitkeep,
  // which keeps the Go embed directive valid on a fresh clone).
  publicDir: 'static',
  build: { outDir: 'dist', emptyOutDir: true, target: 'es2022' },
  server: {
    port: Number(process.env.TRACKER_DEV_PORT ?? 5173),
    // A taken port is an error: silently moving to 5174 would break the
    // TRACKER_PUBLIC_URL the backend was started with.
    strictPort: true,
    proxy: { '/api': backend, '/health': backend },
  },
  // Vitest must resolve Svelte's browser build; leave Vite's defaults alone otherwise.
  ...(process.env.VITEST ? { resolve: { conditions: ['browser'] } } : {}),
  test: { environment: 'jsdom', include: ['src/**/*.test.ts'] },
})
