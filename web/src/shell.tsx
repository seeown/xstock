import { createContext, useContext, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import { Navigate, useSearchParams } from 'react-router-dom'
import { DEFAULT_VIEW, VIEWS, viewByKey } from './views'
import ErrorBoundary from './components/ErrorBoundary'

// ─── 视图上下文：当前 Tab + 切换入口 ───────────────────────────
// 视图就是 URL 上的 ?view=<key>；壳、菜单、页面都从这里读写。
// 悬浮切换用 replace(不污染后退历史)，点击切换压栈(后退可回上一 Tab)。

interface ViewCtl {
  view: string
  switchTo: (key: string, opts?: { replace?: boolean }) => void
}

const ViewContext = createContext<ViewCtl>({ view: DEFAULT_VIEW, switchTo: () => {} })
export const useView = () => useContext(ViewContext)

export function ViewProvider({ children }: { children: ReactNode }) {
  const [searchParams, setSearchParams] = useSearchParams()
  const raw = searchParams.get('view')
  const view = viewByKey(raw)?.key ?? DEFAULT_VIEW

  const switchTo = (key: string, opts?: { replace?: boolean }) => {
    if (!viewByKey(key) || key === view) return
    setSearchParams(prev => {
      const next = new URLSearchParams(prev)
      next.set('view', key)
      return next
    }, { replace: opts?.replace ?? false })
  }

  return <ViewContext.Provider value={{ view, switchTo }}>{children}</ViewContext.Provider>
}

// ─── 激活上下文：页面据此门控副作用 ───────────────────────────
// 结构性约定：轮询/键盘/定时器一律读取 useIsActive()，保活的隐藏页
// 自动静默——新增页面无法绕过(键盘 hook 已内置)。

export const ActiveContext = createContext(true)
export const useIsActive = () => useContext(ActiveContext)

// ─── 保活壳：按需挂载，访问过的视图常驻，隐藏用 hidden 属性 ───
// 每个视图独立滚动容器，切走再切回时滚动位置原样保留；
// 激活瞬间广播一次 window resize，让 klinecharts 等图表校正尺寸
// (隐藏期间若窗口缩放过，ResizeObserver/resize 监听需要这一踢)。

export function Shell() {
  const { view } = useView()
  const [visited, setVisited] = useState<string[]>([view])

  useEffect(() => {
    setVisited(vs => (vs.includes(view) ? vs : [...vs, view]))
  }, [view])

  useEffect(() => {
    const id = requestAnimationFrame(() => window.dispatchEvent(new Event('resize')))
    return () => cancelAnimationFrame(id)
  }, [view])

  return (
    <>
      {VIEWS.map(v =>
        visited.includes(v.key) ? (
          <section key={v.key} className={`view${view === v.key ? ' active' : ''}`} hidden={view !== v.key} aria-hidden={view !== v.key}>
            <ActiveContext.Provider value={view === v.key}>
              <ErrorBoundary>
                <v.Component />
              </ErrorBoundary>
            </ActiveContext.Provider>
          </section>
        ) : null,
      )}
    </>
  )
}

// 旧路径(/screen 等)重定向到 /?view=<key>，保留全部 query 参数。
export function LegacyRedirect({ viewKey }: { viewKey: string }) {
  const [searchParams] = useSearchParams()
  const qs = new URLSearchParams(searchParams)
  qs.set('view', viewKey)
  return <Navigate to={`/?${qs.toString()}`} replace />
}
