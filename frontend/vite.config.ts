import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    host: '0.0.0.0',
    port: 5173,
    // Vite 5.4+ 会校验请求的 Host 头，局域网 IP 访问默认会被拒绝
    // （防 DNS rebinding），显式放行调试用的局域网地址
    allowedHosts: ['192.168.1.121'],
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8899',
        changeOrigin: true,
      },
      '/ws': {
        target: 'ws://127.0.0.1:8899',
        ws: true,
        changeOrigin: true,
      },
      '/ui': {
        target: 'http://127.0.0.1:8899',
        changeOrigin: true,
      },
    },
  },
})
