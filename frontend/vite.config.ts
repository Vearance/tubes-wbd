import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

const backendProxy = {
  target: 'http://localhost:8080',
  changeOrigin: true,
}

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/auth': backendProxy,
      '/admin': backendProxy,
      '/databases': backendProxy,
      '/migrations': backendProxy,
      '/health': backendProxy,
    },
  },
})
