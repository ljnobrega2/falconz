import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
export default defineConfig({
    plugins: [react()],
    base: '/',
    build: { outDir: 'dist', sourcemap: false },
    server: {
        port: 5173,
        host: true,
        // AUDIT-2026-06-18: dev stand-alone sem WP — aceita Host de tunnel cloudflare
        // e proxeia a API REST para o serviço Go admin (:8087).
        // DEV-CHECKOUT-2026-06-24: o checkout FALK (checkout-ui :5175 + orders :8086)
        // é exposto no MESMO tunnel do painel — assim um link de checkout aberto do
        // admin renderiza a página FALK com dados de dev. Ordem importa: '/checkout-api'
        // antes de '/checkout' (mais específico vence o prefix-match). ws:true mantém o
        // HMR do checkout-ui vivo através do proxy.
        allowedHosts: true,
        proxy: {
            '/checkout-api': { target: 'http://localhost:8086', changeOrigin: true },
            '/checkout': { target: 'http://localhost:5175', changeOrigin: true, ws: true },
            '/wp-json': { target: 'http://localhost:8087', changeOrigin: true },
        },
    },
});
