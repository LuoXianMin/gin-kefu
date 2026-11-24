import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// 双入口: 访客 H5(h5.html) + 客服工作台(workbench.html)
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    }
  },
  server: {
    host: '0.0.0.0', // 监听所有网卡, 支持内网其他设备访问(API 走 /api 代理、WS 按 location.hostname 拼接, 均自动跟随访问主机)
    allowedHosts: true,
    port: 8090,
    proxy: {
      // /api 前缀转发后端 8888 并剥掉前缀(后端路由无 /api)
      '/api': {
        target: 'http://127.0.0.1:8888',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, '')
      }
    }
  },
  build: {
    rollupOptions: {
      input: {
        h5: fileURLToPath(new URL('./h5.html', import.meta.url)),
        workbench: fileURLToPath(new URL('./workbench.html', import.meta.url))
      }
    }
  }
})
