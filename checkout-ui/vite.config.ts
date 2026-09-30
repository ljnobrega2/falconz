import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// CHECKOUT-UI publico (cliente final, SEM auth). Serve em /checkout/.
export default defineConfig({
  plugins: [react()],
  base: '/checkout/',
  build: { outDir: 'dist', sourcemap: false },
  server: {
    port: 5175,
    host: true,
    // dev stand-alone: aceita Host de tunnel cloudflare e proxeia a API publica
    // do checkout para o servico backend (Go/WP). Espelha o padrao do admin-ui.
    allowedHosts: true,
    proxy: {
      '/checkout-api': { target: 'http://localhost:8086', changeOrigin: true },
    },
  },
})
