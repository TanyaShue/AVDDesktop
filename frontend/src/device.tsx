import React from 'react';
import {createRoot} from 'react-dom/client';
import './styles/tokens.css';
import './styles/app.css';
import './styles/device-window.css';
import {DeviceWindowApp} from './components/DeviceWindowApp';
import {ErrorBoundary} from './components/ErrorBoundary';

// 独立设备窗口入口：由辅助进程通过 AssetServer 中间件把 "/" 重写到 /device.html 后加载。
// 与主窗口入口（main.tsx）共用设计令牌，但只渲染设备窗口本体。
const container = document.getElementById('root');

const root = createRoot(container!);

root.render(
    <React.StrictMode>
        <ErrorBoundary scope="设备窗口">
            <DeviceWindowApp/>
        </ErrorBoundary>
    </React.StrictMode>
);
