/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

function manualChunks(id: string): string | undefined {
  const normalized = id.split('\\').join('/')
  if (normalized.includes('/node_modules/react/') || normalized.includes('/node_modules/react-dom/') || normalized.includes('/node_modules/react-router-dom/')) {
    return 'vendor-react'
  }
  if (normalized.includes('/node_modules/lucide-react/')) {
    return 'vendor-icons'
  }
}

export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss()],
  base: '/',
  build: {
    outDir: '../web/dist',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2020',
    cssMinify: true,
    rollupOptions: {
      output: {
        manualChunks,
        chunkFileNames: 'assets/[name]-[hash].js',
        entryFileNames: 'assets/[name]-[hash].js',
        assetFileNames: 'assets/[name]-[hash][extname]',
      },
      plugins: mode === 'analyze'
        ? [(async () => (await import('rollup-plugin-visualizer')).visualizer({ open: true, filename: '../web/dist/stats.html' }))()]
        : [],
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
      '/readyz': 'http://localhost:8080',
      '/metrics': 'http://localhost:8080',
      '/openapi.yaml': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
  },
}))
