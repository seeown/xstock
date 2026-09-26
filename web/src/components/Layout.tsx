import { useEffect, type ReactNode } from 'react'
import { NavLink } from 'react-router-dom'
import { ToastStack, useToasts } from './ui'

const navItems = [
  { to: '/guide', label: '情绪指南', no: '01' },
  { to: '/screen', label: '股票筛查', no: '02' },
  { to: '/market', label: '大盘行情', no: '03' },
  { to: '/stocks', label: '个股行情', no: '04' },
  { to: '/', label: '回测分析', no: '05', end: true },
  { to: '/strategy', label: '策略说明', no: '06' },
  { to: '/data', label: '数据管理', no: '07' },
]

export default function Layout({ children }: { children: ReactNode }) {
  const { toasts, push, dismiss } = useToasts()

  // 全局网络错误 Toast：api.ts 在后端不可达时广播（页面级错误仍由页面自处）
  useEffect(() => {
    const onNetErr = (e: Event) => push('连接失败', (e as CustomEvent<string>).detail)
    window.addEventListener('xstock:neterr', onNetErr)
    return () => window.removeEventListener('xstock:neterr', onNetErr)
  }, [push])

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <img src="/logo.png" alt="xstock" className="brand-logo" />
          <div>
            <div className="brand-name">xstock</div>
            <div className="brand-sub">战法研究的交易魔方</div>
          </div>
        </div>
        <nav>
          {navItems.map(item => (
            <NavLink key={item.to} to={item.to} end={item.end} className={({ isActive }) => `nav-item${isActive ? ' active' : ''}`}>
              <span className="no">{item.no}</span>
              <span className="txt">{item.label}</span>
            </NavLink>
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
