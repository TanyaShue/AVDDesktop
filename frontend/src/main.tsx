import React from 'react';
import {createRoot} from 'react-dom/client';
import './styles/tokens.css';
import './styles/app.css';
import App from './app/App';
import { ErrorBoundary } from './components/ErrorBoundary';

const container = document.getElementById('root');

const root = createRoot(container!);

root.render(
    <React.StrictMode>
        {/* 最外层兜底：应用壳（标题栏/导航）出错时也不能只剩白屏 */}
        <ErrorBoundary scope="应用">
            <App/>
        </ErrorBoundary>
    </React.StrictMode>
);
