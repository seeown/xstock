import type { ReactNode } from 'react'
import { NavLink } from 'react-router-dom'

const navItems = [
  { to: '/', label: '回测分析', no: '01', end: true },
  { to: '/screen', label: '股票筛查', no: '02' },
  { to: '/market', label: '大盘行情', no: '03' },
  { to: '/stocks', label: '个股行情', no: '04' },
  { to: '/data', label: '数据管理', no: '05' },
  { to: '/guide', label: '情绪指南', no: '06' },
  { to: '/strategy', label: '策略说明', no: '07' },
]

export default function Layout({ children }: { children: ReactNode }) {
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
    </div>
  )
}
