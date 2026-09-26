import { useEffect, useState } from 'react'

interface SyncState {
  last_daily_update?: string
  [k: string]: unknown
}

export interface Freshness {
  loading: boolean
  lastUpdate: string | null
  stale: boolean
}

// 数据新鲜度徽章：轮询 /api/sync-state，超过 1 个交易日未更新视为过期。
export function useFreshness(intervalMs = 60_000): Freshness {
  const [state, setState] = useState<Freshness>({ loading: true, lastUpdate: null, stale: false })

  useEffect(() => {
    let stopped = false
    const load = async () => {
      try {
        const res = await fetch('/api/sync-state')
        if (!res.ok) throw new Error(String(res.status))
        const data = (await res.json()) as SyncState
        if (stopped) return
        const last = data.last_daily_update ?? null
        const stale = last ? Date.now() - new Date(last.replace(' ', 'T') + '+08:00').getTime() > 3 * 24 * 3600 * 1000 : false
        setState({ loading: false, lastUpdate: last, stale })
      } catch {
        if (!stopped) setState(s => ({ ...s, loading: false }))
      }
    }
    load()
    const id = setInterval(load, intervalMs)
    return () => { stopped = true; clearInterval(id) }
  }, [intervalMs])

  return state
}
