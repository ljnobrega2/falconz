import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/portal/',
  build: { outDir: 'dist', sourcemap: false },
  server: {
    port: 5173,
    host: true,
    // Portal user React stand-alone (sem WP) — aceita Host de tunnel cloudflare
    // e proxeia a API REST para o serviço Go portal (:8085).
    allowedHosts: true,
    proxy: {
      // Rotas admin → serviço Go admin (:8087); espelha nginx de produção.
      '/wp-json/senderzz/v1/admin': { target: 'http://localhost:8087', changeOrigin: true },
      // Demais /wp-json → portal (:8085).
      '/wp-json': { target: 'http://localhost:8085', changeOrigin: true },
    },
  },
})
