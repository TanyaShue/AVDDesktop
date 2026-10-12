# 前端开发工具

## 界面预览与截图（视觉走查）

Wails 应用平时只能在真实后端下运行，改样式时很难快速看到效果。这里提供一套
**纯前端预览**：把 Wails 桥接层打桩成假数据，用普通浏览器渲染真实组件。

```powershell
cd frontend
npm install                     # 首次
npx vite --port 5199 --strictPort
```

然后打开 <http://localhost:5199/preview.html>。查询参数：

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `page` | `devices` \| `settings` | 进入指定页面 |
| `dialog` | `wizard` \| `env` \| `mirror` \| `images` | 打开指定弹窗 |
| `drawer` | `1` | 展开底部任务面板 |
| `theme` | `light` \| `dark` | 切换主题 |
| `env` | `missing` | 渲染「环境未就绪」分支 |

示例：`preview.html?page=settings&dialog=mirror&theme=dark`

抓图（需要 Edge 或 Chrome）：

```powershell
pwsh -File tools/ui-shots.ps1 -Prefix after -Out ..\build\ui-shots
```

输出目录默认 `build/ui-shots`（`/build/` 已在 .gitignore 中，不会进仓库）。
脚本用 `--virtual-time-budget` 等待异步渲染，所以假数据的 Promise 会先完成再截图。

相关文件：

- `preview.html`：预览入口
- `src/preview/main.tsx`：入口脚本（含按 URL 参数驱动点击的辅助逻辑）
- `src/preview/mock.ts`：`window.go` / `window.runtime` 打桩
- `src/preview/fixtures.ts`：假数据

预览入口不在 `vite.config.ts` 的 `rollupOptions.input` 里，因此**不会进入正式构建产物**。
