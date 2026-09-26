import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { useView } from '../shell'
import { VIEWS } from '../views'
import { ToastStack, useToasts } from './ui'

// 悬浮切换的驻留阈值：划过不切，停稳才切。
const HOVER_DWELL_MS = 250

export default function Layout({ children }: { children: ReactNode }) {
  const { toasts, push, dismiss } = useToasts()
  const { view, switchTo } = useView()
  const dwell = useRef<number>(undefined)

  // 卸载时清掉驻留定时器
  useEffect(() => () => window.clearTimeout(dwell.current), [])

  // 全局网络错误 Toast：api.ts 在后端不可达时广播（页面级错误仍由页面自处）
  useEffect(() => {
    const onNetErr = (e: Event) => push('连接失败', (e as CustomEvent<string>).detail)
    window.addEventListener('xstock:neterr', onNetErr)
    return () => window.removeEventListener('xstock:neterr', onNetErr)
  }, [push])

  const enterItem = (key: string) => () => {
    if (key === view) return
    window.clearTimeout(dwell.current)
    dwell.current = window.setTimeout(() => switchTo(key, { replace: true }), HOVER_DWELL_MS)
  }
  const leaveItem = () => window.clearTimeout(dwell.current)
  const clickItem = (e: React.MouseEvent<HTMLAnchorElement>, key: string) => {
    // 修饰键/中键点击交给浏览器原生行为(新标签打开)
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) return
    e.preventDefault()
    window.clearTimeout(dwell.current)
    switchTo(key)
  }

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <img src="/logo.png" alt="xstock" className="brand-logo" />
          <div>
            <div className="brand-name">xstock</div>
            <div className="brand-sub">战法研究交易魔方</div>
          </div>
        </div>
        <nav>
          {VIEWS.map(item => (
            <a
              key={item.key}
              href={`/?view=${item.key}`}
              className={`nav-item${view === item.key ? ' active' : ''}`}
              aria-current={view === item.key ? 'page' : undefined}
              onMouseEnter={enterItem(item.key)}
              onMouseLeave={leaveItem}
              onFocus={leaveItem}
              onClick={e => clickItem(e, item.key)}
            >
              <span className="no">{item.no}</span>
              <span className="txt">{item.label}</span>
            </a>
          ))}
        </nav>
        <div className="sidebar-foot">
          <div className="risk-pill">研究工具 · 非投资建议</div>
        </div>
      </aside>
      <main className="main">{children}</main>
      <ToastStack toasts={toasts} onClose={dismiss} />
    </div>
  )
}
