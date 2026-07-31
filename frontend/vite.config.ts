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
    // 这三条代理刻意都不开 changeOrigin。
    //
    // changeOrigin 会把 Host 头改写成 target 的地址（127.0.0.1:8899），
    // 而 /dashboard/entry 要靠 Host 判断"用户是从哪个地址访问管理端的"，
    // 据此把内核控制地址里的 127.0.0.1 换成同一个主机名。Host 一旦被改写，
    // 后端就以为请求来自本机，返回给面板的地址仍是 127.0.0.1 —— 从局域网
    // 其它设备打开时，那个地址指向设备自己，表现为面板连不上内核。
    // 后端不做任何基于 Host 的路由，保留真实 Host 没有副作用。
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8899',
      },
      '/ws': {
        target: 'ws://127.0.0.1:8899',
        ws: true,
      },
      '/ui': {
        target: 'http://127.0.0.1:8899',
      },
    },
  },
})
