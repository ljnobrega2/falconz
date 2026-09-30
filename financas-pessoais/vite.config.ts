import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': {
        target: 'https://90d78023.finanz-4wk.pages.dev',
        changeOrigin: true,
        secure: true,
      },
    },
  },
})
