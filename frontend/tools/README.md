# 前端开发工具

## 界面预览与截图（视觉走查）

Wails 应用平时只能在真实后端下运行，改样式时很难快速看到效果。这里提供一套
**纯前端预览**：把 Wails 桥接层打桩成假数据，用普通浏览器渲染真实组件。

```powershell
cd frontend
npm install                     # 首次
npx vite --port 5199 --strictPort
```

然后打开 <http://localhost:5199/preview.html>（主窗口）或
<http://localhost:5199/device-preview.html>（独立设备窗口）。查询参数：

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `page` | `devices` \| `settings` | 进入指定页面 |
| `dialog` | `wizard` \| `env` \| `mirror` \| `images` | 打开指定弹窗 |
| `drawer` | `1` | 展开底部任务面板 |
| `view` | `compact` | 切换为紧凑列表视图 |
| `sort` | `1` | 展开排序浮层 |
| `menu` | `1` | 展开第一台设备的「更多操作」菜单 |
| `q` | 任意字符串 | 播种设备搜索词（核对清空按钮 / 匹配计数 / 无结果空状态） |
| `toast` | `1` | 派发成功 + 失败事件，核对 Toast 样式 |
| `theme` | `light` \| `dark` | 切换主题（在 React 挂载前就写 `data-theme`，与生产首屏一致） |
| `noanim` | `1` | 关掉全部动画与过渡（见下方「已知坑」） |
| `env` | `missing` | 渲染「环境未就绪」分支 |
| `probe` | CSS 选择器 | 把命中元素的计算样式固定显示在页面顶部（配合 `--dump-dom` 排查样式问题） |

示例：`preview.html?page=settings&dialog=mirror&theme=dark&noanim=1`

### 抓图

```powershell
pwsh -File tools/ui-shots.ps1 -Prefix after                          # 默认 1440x900，全部状态
pwsh -File tools/ui-shots.ps1 -Prefix rw -Sizes 1440x900,1180x760,960x640   # 多尺寸
pwsh -File tools/ui-shots.ps1 -Prefix one -Only "devices,settings-dark"     # 只抓指定状态
```

输出目录默认 `build/ui-shots`（`/build/` 已在 .gitignore 中，不会进仓库）。
主窗口 `MinWidth = 960`（见 `main.go`），所以**每次改布局至少要覆盖 1440 / 1180（默认宽）/ 960（最小宽）**。

### 已知坑：截图里的「假颜色 bug」

headless Edge + `--virtual-time-budget` 下，CSS 动画/过渡的时钟可能停在起始帧。
`data-theme` 生效时 `.btn` 等带 `transition: color/border-color/background` 的元素
会把文字与边框冻结在**上一个主题**的颜色上，截图看起来像「深色下按钮文字消失」，
但计算样式其实是对的（`?probe=` 可以证明）。

判断方法：给 URL 加 `noanim=1` 再抓一次。加了就正常 → 是工具假象，不要去改组件颜色。
（本仓库 2026 年那轮深色审计就在这里误判过一次，`noanim` 就是为此加的。）

相关文件：

- `preview.html` / `device-preview.html`：两个预览入口
- `src/preview/main.tsx`：主窗口预览入口（按 URL 参数驱动点击 / 播种输入 / 样式探针）
- `src/preview/device.tsx`：设备窗口预览入口（打桩 `window.go.displayhost.DeviceWindow`）
- `src/preview/mock.ts`：`window.go` / `window.runtime` 打桩
- `src/preview/fixtures.ts`：假数据
- `tools/frame-sample.svg`：设备窗口预览用的假「设备画面」

两个预览入口都不在 `vite.config.ts` 的 `rollupOptions.input` 里，因此**不会进入正式构建产物**。
