import { fileURLToPath } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

const envDir = fileURLToPath(new URL('..', import.meta.url))

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, envDir, '')
  const backendProxy = {
    target: `http://localhost:${env.APP_PORT || '8080'}`,
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
