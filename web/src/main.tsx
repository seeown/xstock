import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import './styles.css'

// 顶层错误边界：任何组件崩溃时把错误显示在页面上，而不是整页白屏。
class ErrorBoundary extends React.Component<
  { children: React.ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  componentDidCatch(error: Error) {
    console.error('页面崩溃:', error)
  }

  render() {
    if (this.state.error) {
      return (
        <div style={{ padding: 40, fontFamily: 'system-ui, sans-serif', background: '#070d18', minHeight: '100vh' }}>
          <h2 style={{ color: '#f4577a', fontSize: 18 }}>页面渲染出错</h2>
          <pre style={{ color: '#e8eef9', fontSize: 13, whiteSpace: 'pre-wrap' }}>
            {String(this.state.error?.message || this.state.error)}
          </pre>
          <p style={{ color: '#8ea2bd', fontSize: 13 }}>请把以上错误信息反馈给开发者；刷新页面可重试。</p>
        </div>
      )
    }
    return this.props.children
  }
}

// 模块执行即代表应用脚本已启动；index.html 的自检据此判断。
;(window as unknown as { __boot_done: boolean }).__boot_done = true

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>,
)
