import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from 'react'
import { api, type WatchItem } from './api'

// 自选股全局状态：个股行情页、共用K线弹窗等都能加/移自选。
// 单用户工具，启动时全量拉取；乐观更新，失败回滚并提示。
interface WatchCtxValue {
  items: WatchItem[]
  has: (symbol: string) => boolean
  toggle: (symbol: string) => void
}

const noopCtx: WatchCtxValue = { items: [], has: () => false, toggle: () => {} }

const WatchCtx = createContext<WatchCtxValue>(noopCtx)

export function WatchlistProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<WatchItem[]>([])
  const [set, setSet] = useState<Set<string>>(new Set())

  useEffect(() => {
    api.watchlist()
      .then(list => {
        setItems(list)
        setSet(new Set(list.map(i => i.symbol)))
      })
      .catch(() => {})
  }, [])

  const toggle = useCallback((symbol: string) => {
    const had = set.has(symbol)
    setSet(prev => {
      const n = new Set(prev)
      if (had) n.delete(symbol)
      else n.add(symbol)
      return n
    })
    setItems(prev => (had ? prev.filter(i => i.symbol !== symbol) : [...prev, { symbol }]))
    const req = had ? api.watchRemove(symbol) : api.watchAdd(symbol)
    req.catch(() => {
      // 回滚乐观更新
      setSet(prev => {
        const n = new Set(prev)
        if (had) n.add(symbol)
        else n.delete(symbol)
        return n
      })
      setItems(prev => (had ? [...prev, { symbol }] : prev.filter(i => i.symbol !== symbol)))
      window.dispatchEvent(new CustomEvent('xstock:neterr', { detail: '自选更新失败，已回滚' }))
    })
  }, [set])

  const has = useCallback((symbol: string) => set.has(symbol), [set])

  return <WatchCtx.Provider value={{ items, has, toggle }}>{children}</WatchCtx.Provider>
}

export function useWatchlist() {
  return useContext(WatchCtx)
}
