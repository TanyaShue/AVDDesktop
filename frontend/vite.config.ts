import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// 两个前端入口：
//   index.html  —— 主窗口（设备管理 / 设置）
//   device.html —— 独立设备窗口（辅助进程加载，见 internal/displayhost/window.go）
// 两者共享 chunk 与设计令牌，构建产物一并打进 embed 资源。
// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    rollupOptions: {
      input: {
        main: 'index.html',
        device: 'device.html'
      }
    }
  },
  server: {
    watch: {
      // 编辑器/工具写文件时会先建 `.<name>.<pid>.<uuid>.tmpdir/` 再原子替换。
      // Windows 上这些临时目录随写随删，chokidar 监听它们会偶发 EBUSY 并直接
      // 让 vite dev server 崩溃，这里显式忽略临时目录。
      ignored: ['**/*.tmpdir', '**/*.tmpdir/**', '**/.*.tmp/**']
    }
  }
})
