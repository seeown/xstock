import type { ComponentType } from 'react'
import Dashboard from './pages/Dashboard'
import DataManager from './pages/DataManager'
import Guide from './pages/Guide'
import Market from './pages/Market'
import Screen from './pages/Screen'
import Stocks from './pages/Stocks'
import Strategy from './pages/Strategy'

// 视图注册表：壳(保活 Tab)、侧栏菜单、旧路径重定向共用这一份。
// path 是改造前的路由，仅用于兼容旧链接；menu 上显示顺序即数组顺序。
export interface ViewDef {
  key: string
  path: string
  label: string
  no: string
  Component: ComponentType
}

export const VIEWS: ViewDef[] = [
  { key: 'guide', path: '/guide', label: '情绪指南', no: '01', Component: Guide },
  { key: 'screen', path: '/screen', label: '股票筛查', no: '02', Component: Screen },
  { key: 'market', path: '/market', label: '大盘行情', no: '03', Component: Market },
  { key: 'stocks', path: '/stocks', label: '个股行情', no: '04', Component: Stocks },
  { key: 'backtest', path: '/', label: '回测分析', no: '05', Component: Dashboard },
  { key: 'strategy', path: '/strategy', label: '策略说明', no: '06', Component: Strategy },
  { key: 'data', path: '/data', label: '数据管理', no: '07', Component: DataManager },
]

// 裸 `/`(无 ?view=)默认落在回测分析——与改造前的首页一致，
// 存量 `/?symbol=X` 深链接也因此无需任何重定向。
export const DEFAULT_VIEW = 'backtest'

export const viewByKey = (key: string | null): ViewDef | undefined =>
  VIEWS.find(v => v.key === key)
