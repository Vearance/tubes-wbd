import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

const envDir = fileURLToPath(new URL('..', import.meta.url))

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, envDir, ['APP_PORT', 'BACKEND_HOST'])
  const backendProxy = {
    target: `http://${env.BACKEND_HOST}:${env.APP_PORT}`,
    changeOrigin: true,
  }

  return {
    envDir,
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
  }
})
