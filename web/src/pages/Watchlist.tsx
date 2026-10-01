import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type ProfileListItem, type Quote } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { PaperQuickBuy } from '../components/paperQuickBuy'
import { Button, Card, EmptyState, KpiCard, pct } from '../components/ui'
import { useIsActive } from '../shell'
import { useWatchlist } from '../watchlist'

// 自选股看板：行情快照口径的小表格，交互与个股行情一致
// （悬浮K线缩略、点行开全周期大图）。竞价承接列是后续落点。
function fmtAmount(v: number): string {
  if (!v) return '—'
  if (v >= 1e8) return `${(v / 1e8).toFixed(1)}亿`
  if (v >= 1e4) return `${(v / 1e4).toFixed(0)}万`
  return v.toFixed(0)
}

export default function Watchlist() {
  const navigate = useNavigate()
  const watch = useWatchlist()
  const kline = useKlinePreview()
  const isActiveView = useIsActive()
  const [items, setItems] = useState<ProfileListItem[]>([])
  const [quotes, setQuotes] = useState<Record<string, Quote>>({})
  const [loading, setLoading] = useState(true)

  // 名单与实时行情：激活时加载 + 60s 轮询（名单随自选增减即时反映在 provider，
  // 这里只在初次与每次轮询时重取，量小无所谓）。
  const load = useCallback(async () => {
    try {
      const r = await api.profiles({ watch: true, pageSize: 200 })
      setItems(r.items)
      if (r.items.length) {
        const qs = await api.quotes(r.items.map(i => i.symbol))
        const m: Record<string, Quote> = {}
        for (const q of qs) m[q.symbol] = q
        setQuotes(m)
      } else {
        setQuotes({})
      }
    } catch { /* 静默：空态/错误以内容呈现 */ }
    finally { setLoading(false) }
  }, [])

  useEffect(() => {
    if (!isActiveView) return
    void load()
    const t = window.setInterval(() => { void load() }, 60_000)
    return () => window.clearInterval(t)
  }, [isActiveView, load])

  // 激活即重拉：保活壳里首次加载若撞上服务重启挂起，「正在加载」会永久楔死，
  // 每次切回本页立即重新加载可自愈（与情绪指南/大盘页同款模式）。
  useEffect(() => {
    if (isActiveView) void load()
  }, [isActiveView, load])

  // 自选集合变化（别处加/移）时立即重拉名单
  useEffect(() => {
    if (isActiveView) void load()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [watch.items.length])

  const stats = useMemo(() => {
    let up = 0, down = 0, amount = 0
    for (const it of items) {
      const q = quotes[it.symbol]
      if (!q || q.price <= 0) continue
      if (q.changePct > 0) up++
      else if (q.changePct < 0) down++
      amount += q.amount
    }
    return { up, down, amount }
  }, [items, quotes])

  const rowHover = (item: ProfileListItem) =>
    item.barCount > 0
      ? {
          onMouseEnter: (e: React.MouseEvent<HTMLTableRowElement>) =>
            kline.enterRow({ symbol: item.symbol, name: item.name, sub: item.industry || undefined }, e.currentTarget),
          onMouseLeave: kline.leaveRow,
        }
      : {}
  const openDetail = (item: ProfileListItem) =>
    kline.openDetail({ symbol: item.symbol, name: item.name, sub: item.industry || undefined })

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>自选股</h1>
          <p>我的关注池 · 实时行情 60 秒自动刷新 · 悬浮看缩略K线，点击开全周期大图</p>
        </div>
      </header>

      {items.length > 0 && (
        <div className="kpis">
          <KpiCard label="自选数量" value={String(items.length)} />
          <KpiCard label="上涨 / 下跌" value={`${stats.up} / ${stats.down}`} />
          <KpiCard label="合计成交额" value={fmtAmount(stats.amount)} />
        </div>
      )}

      <Card>
        {loading ? (
          <div className="chart-empty">正在加载自选…</div>
        ) : items.length === 0 ? (
          <EmptyState
            title="还没有自选股"
            desc="去个股行情把关心的票加上 ☆ 星标（列表行首或K线弹窗里都可以），这里就会实时跟踪它们。"
            actions={<Button onClick={() => navigate('/?view=stocks')}>去个股行情加自选</Button>}
          />
        ) : (
          <div className="table-wrap">
            <table className="tb">
              <thead>
                <tr>
                  <th className="star-col" aria-label="自选" />
                  <th>代码</th>
                  <th>名称</th>
                  <th className="num">现价</th>
                  <th className="num">涨跌幅</th>
                  <th className="num">成交额</th>
                  <th>行业</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {items.map(item => {
                  const q = quotes[item.symbol]
                  return (
                    <tr key={item.symbol} className="rowlink" onClick={() => openDetail(item)} {...rowHover(item)}>
                      <td className="star-col">
                        <button
                          className="star-btn on"
                          title="移出自选"
                          onClick={e => { e.stopPropagation(); watch.toggle(item.symbol) }}
                        >★</button>
                      </td>
                      <td><span className="code">{item.symbol}</span></td>
                      <td><span style={{ color: 'var(--ink)' }}>{item.name || item.symbol}</span></td>
                      <td className="num">{q && q.price > 0 ? q.price.toFixed(2) : '—'}</td>
                      <td className={`num ${q && q.changePct >= 0 ? 'up-text' : 'down-text'}`}>
                        {q && q.price > 0 && q.preClose > 0 ? `${q.changePct >= 0 ? '+' : ''}${pct(q.changePct)}` : '—'}
                      </td>
                      <td className="num muted-c">{q ? fmtAmount(q.amount) : '—'}</td>
                      <td className="muted-c">{item.industry || '—'}</td>
                      <td>
                        <PaperQuickBuy symbol={item.symbol} name={item.name} />
                        <button className="link-btn" onClick={e => { e.stopPropagation(); navigate(`/?view=backtest&symbol=${item.symbol}`) }}>回测</button>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={symbol => navigate(`/?view=backtest&symbol=${symbol}`)} />
    </div>
  )
}
