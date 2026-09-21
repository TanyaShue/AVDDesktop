// 错误边界：任何页面渲染异常都不应该让窗口变成空白页。
//
// 背景：React 在渲染期抛异常且没有错误边界时，会卸载整棵树 —— 表现为整个窗口白屏
// （连标题栏和导航都不见了，用户无法自救，也看不到任何错误信息）。
// 这里把异常收敛成一张可读的错误卡片：保留导航、暴露错误文本、提供重试入口。
import { Component, type ErrorInfo, type ReactNode } from "react";

interface Props {
  children: ReactNode;
  /** 出错范围（如“首页”），用于提示与日志定位。 */
  scope?: string;
  /** 出错时通知外部（例如上报到应用日志）。 */
  onError?: (error: Error, info: ErrorInfo) => void;
}

interface State {
  error: Error | null;
}

export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo): void {
    // 保留控制台现场，便于用 DevTools 直接看到组件栈
    console.error(`[ui]${this.props.scope ? " " + this.props.scope : ""}渲染失败`, error, info.componentStack);
    this.props.onError?.(error, info);
  }

  private reset = (): void => this.setState({ error: null });

  private copy = (): void => {
    const { error } = this.state;
    if (!error) return;
    void navigator.clipboard?.writeText(String(error.stack || error.message)).catch(() => undefined);
  };

  render(): ReactNode {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <div className="errorfallback">
        <div className="card errorfallback__card">
          <div className="banner banner--danger">
            <span className="banner__icon" aria-hidden>
              ✕
            </span>
            <div className="banner__body">
              <div className="banner__title">{this.props.scope ?? "页面"}渲染出错，已阻止白屏</div>
              <div className="banner__text">
                其它页面仍然可用；如果反复出现，请点“复制错误”把下面内容贴给我们。
              </div>
            </div>
          </div>
          <pre className="errorfallback__pre mono">{String(error.stack || error.message)}</pre>
          <div className="row" style={{ gap: 8, marginTop: 12 }}>
            <button className="btn btn--primary" onClick={this.reset}>
              重试
            </button>
            <button className="btn btn--secondary" onClick={this.copy}>
              复制错误
            </button>
            <button className="btn btn--secondary" onClick={() => window.location.reload()}>
              重新加载
            </button>
          </div>
        </div>
      </div>
    );
  }
}
