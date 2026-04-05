import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 3001,
    proxy: {
      '/api': { target: 'http://localhost:5002', changeOrigin: true }
    }
  },
  build: {
    outDir: 'dist',
    target: 'es2015'
  }
})
