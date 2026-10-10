import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  base: '/admin/',
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        manualChunks: { vue: ['vue'] },
      },
    },
  },
  server: {
    proxy: {
      '^/admin/(state|files(?:/|$)|releases(?:/|$))': {
        target: 'http://127.0.0.1:8080',
      },
    },
  },
})
