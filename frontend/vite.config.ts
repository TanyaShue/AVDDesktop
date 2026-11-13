import {defineConfig} from 'vite'
import react from '@vitejs/plugin-react'

// 前端入口：index.html —— 主窗口（设备管理 / 设置）。
// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  server: {
    watch: {
      // 编辑器/工具写文件时会先建 `.<name>.<pid>.<uuid>.tmpdir/` 再原子替换。
      // Windows 上这些临时目录随写随删，chokidar 监听它们会偶发 EBUSY 并直接
      // 让 vite dev server 崩溃，这里显式忽略临时目录。
      ignored: ['**/*.tmpdir', '**/*.tmpdir/**', '**/.*.tmp/**']
    }
  }
})
