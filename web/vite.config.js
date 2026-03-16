import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  resolve: {
    // Allow importing CSS from packages that don't export via package exports
    conditions: ['import', 'module', 'browser', 'default'],
  },
  css: {
    // Suppress warnings for CSS @charset ordering
    devSourcemap: false,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:18104', changeOrigin: true },
      '/internal': { target: 'http://localhost:18104', changeOrigin: true },
      '/admin': { target: 'http://localhost:18104', changeOrigin: true },
      '/proxy': { target: 'http://localhost:18104', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})
