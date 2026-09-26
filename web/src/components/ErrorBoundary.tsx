import { Component, type ErrorInfo, type ReactNode } from 'react'

// 全局错误边界：页面组件抛错时给出可恢复的提示，而不是整页白屏。
export default class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack)
  }

  render() {
    if (this.state.error) {
      return (
        <div className="page">
          <div className="errblock" role="alert">
            <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="var(--up-text)" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
              <circle cx="12" cy="12" r="9" />
              <path d="M12 7.5v5.5" />
              <path d="M12 16.5h.01" />
            </svg>
            <div>
              <div className="t">页面渲染出错</div>
              <div className="d">{this.state.error.message}</div>
            </div>
            <button className="btn2-ghost" onClick={() => location.reload()}>刷新页面</button>
          </div>
        </div>
      )
    }
    return this.props.children
  }
}
