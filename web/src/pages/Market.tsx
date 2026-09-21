import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type Candle, type IndexInfo } from '../api'
import KlineChart, { computeMA } from '../components/KlineChart'
import { Banner, Card, CardHead, fmt, pct } from '../components/ui'

type RangeKey = '60' | '250' | '750' | 'all'

const ranges: Array<{ key: RangeKey; label: string; days: number }> = [
  { key: '60', label: '近3月', days: 60 },
  { key: '250', label: '近1年', days: 250 },
  { key: '750', label: '近3年', days: 750 },
  { key: 'all', label: '全部', days: Number.MAX_SAFE_INTEGER },
]

export default function Market() {
  const [indices, setIndices] = useState<IndexInfo[]>([])
  const [active, setActive] = useState('')
  const [barsCache, setBarsCache] = useState<Record<string, Candle[]>>({})
  const [range, setRange] = useState<RangeKey>('250')
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [feedback, setFeedback] = useState<{ text: string; kind: 'info' | 'error' | 'success' }>({ text: '', kind: 'info' })

  const loadBars = useCallback(async (symbol: string): Promise<Candle[]> => {
    try {
      const bars = await api.bars(symbol)
      setBarsCache(prev => ({ ...prev, [symbol]: bars }))
      return bars
    } catch {
      return []
    }
  }, [])

  const syncIndex = useCallback(async (symbol: string, name: string, silent = false) => {
    setSyncing(true)
    if (!silent) setFeedback({ text: `正在拉取 ${name} 日K数据…`, kind: 'info' })
    try {
      const r = await api.sync(symbol)
      const bars = await loadBars(symbol)
      setIndices(prev => prev.map(i => i.symbol === symbol ? { ...i, count: r.count, firstDate: r.firstDate, lastDate: r.lastDate } : i))
      setFeedback({ text: `${name} 已更新：${r.count} 根日K（${r.firstDate} ~ ${r.lastDate}）。`, kind: 'success' })
      return bars
    } catch (e) {
      setFeedback({ text: `${name} 同步失败：${e instanceof Error ? e.message : '未知错误'}`, kind: 'error' })
      return []
    } finally {
      setSyncing(false)
    }
  }, [loadBars])

  const selectIndex = useCallback(async (info: IndexInfo) => {
    setActive(info.symbol)
    setFeedback({ text: '', kind: 'info' })
    setLoading(true)
    let bars = await loadBars(info.symbol)
    if (!bars.length) {
      bars = await syncIndex(info.symbol, info.name, true)
    }
    if (!bars.length) setFeedback({ text: `${info.name} 暂无本地数据，请点击「同步日K」拉取。`, kind: 'error' })
    setLoading(false)
  }, [loadBars, syncIndex])

  useEffect(() => {
    ;(async () => {
      try {
        const list = await api.marketIndices()
        setIndices(list)
        if (list.length) await selectIndex(list[0])
      } catch (e) {
        setFeedback({ text: e instanceof Error ? e.message : '加载指数列表失败', kind: 'error' })
      }
    })()
  }, [selectIndex])

  const activeInfo = indices.find(i => i.symbol === active)
  const allBars = barsCache[active] ?? []
  const rangeDays = ranges.find(r => r.key === range)?.days ?? Number.MAX_SAFE_INTEGER
  const bars = useMemo(() => (rangeDays >= allBars.length ? allBars : allBars.slice(-rangeDays)), [allBars, rangeDays])
  const stats = useMemo(() => {
    if (!allBars.length) return null
    const last = allBars[allBars.length - 1]
    const prev = allBars.length > 1 ? allBars[allBars.length - 2] : null
    const ma5All = computeMA(allBars, 5)
    const ma10All = computeMA(allBars, 10)
    const ma5 = ma5All[ma5All.length - 1]
    const ma10 = ma10All[ma10All.length - 1]
    return {
      close: last.close,
      change: prev ? ((last.close - prev.close) / prev.close) * 100 : 0,
      ma5, ma10,
      bullish: ma5 !== null && ma10 !== null ? ma5 >= ma10 : null,
      first: allBars[0].date,
      lastDate: last.date,
      count: allBars.length,
    }
  }, [allBars])

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>大盘行情</h1>
          <p>上证指数 · 深证成指 · 创业板指 · 科创50，附带 MA5 / MA10 均线</p>
        </div>
        {activeInfo && (
          <button className="btn primary" onClick={() => syncIndex(activeInfo.symbol, activeInfo.name)} disabled={syncing}>
            {syncing ? '同步中…' : '同步日K'}
          </button>
        )}
      </header>

      <Banner text={feedback.text} kind={feedback.kind} />

      <div className="index-tabs">
        {indices.map(i => (
          <button
            key={i.symbol}
            className={`index-tab${active === i.symbol ? ' active' : ''}`}
            onClick={() => selectIndex(i)}
            disabled={syncing}
          >
            <b>{i.name}</b>
            <span>{i.lastDate ? `更新至 ${i.lastDate}` : '未同步'}</span>
          </button>
        ))}
      </div>

      {stats && (
        <div className="metrics-row market-metrics">
          <div className="metric">
            <span className="metric-label">最新收盘</span>
            <strong className="metric-value">{fmt(stats.close)}</strong>
            <span className={`metric-hint ${stats.change >= 0 ? 'pos' : 'neg'}`}>
              今日 {stats.change >= 0 ? '+' : ''}{pct(stats.change)}
            </span>
          </div>
          <div className="metric">
            <span className="metric-label">MA5 五日线</span>
            <strong className="metric-value ma5-text">{stats.ma5 ? fmt(stats.ma5) : '—'}</strong>
          </div>
          <div className="metric">
            <span className="metric-label">MA10 十日线</span>
            <strong className="metric-value ma10-text">{stats.ma10 ? fmt(stats.ma10) : '—'}</strong>
          </div>
          <div className="metric">
            <span className="metric-label">均线状态</span>
            <strong className={`metric-value ${stats.bullish === null ? '' : stats.bullish ? 'pos' : 'neg'}`}>
              {stats.bullish === null ? '—' : stats.bullish ? '多头排列' : '空头排列'}
            </strong>
            <span className="metric-hint">{stats.first} ~ {stats.lastDate} · {stats.count} 根</span>
          </div>
        </div>
      )}

      <Card>
        <CardHead
          title={activeInfo ? `${activeInfo.name} 日K` : '日K'}
          sub="MA5/10/20/30/60/年线(250) + 成交量，支持滚轮缩放与拖动"
          right={
            <div className="range-tabs">
              {ranges.map(r => (
                <button key={r.key} className={`range-tab${range === r.key ? ' active' : ''}`} onClick={() => setRange(r.key)}>
                  {r.label}
                </button>
              ))}
            </div>
          }
        />
        {loading ? <div className="chart-empty">正在加载K线…</div> : <KlineChart bars={bars} />}
      </Card>
    </div>
  )
}
