import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Студия собирается в статику, которую отдаёт сам сервер: один процесс,
// один домен, никакого CORS и отдельной выкатки.
//
// В разработке студия идёт своим сервером, а обращения к редакционному API
// переписываются на сервер. Адрес его берётся из окружения, а не
// вписывается литералом: адрес контура задан в одном месте.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/admin/api': {
        target: process.env.CURATOR_SERVER ?? 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
  },
})
