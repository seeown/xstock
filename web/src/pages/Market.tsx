import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Candle, type IndexInfo, type LadderStock, type SectorRow, type SentimentResult, type SectorsResult } from '../api'
import KlineChart, { computeMA } from '../components/KlineChart'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Banner, Card, CardHead, Sparkline, fmt, pct } from '../components/ui'

// 板块表可排序列（数值列，默认降序）。
type SectorSortKey = 'limitUp' | 'avgChange' | 'count'

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
  const [sent, setSent] = useState<SentimentResult | null>(null)
  const [sectors, setSectors] = useState<SectorsResult | null>(null)
  const [sortKey, setSortKey] = useState<SectorSortKey>('limitUp')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('desc')

  // 梯队选手 chip 点击弹出该票K线（共享预览组件）
  const navigate = useNavigate()
  const kline = useKlinePreview()

  const toggleSort = (key: SectorSortKey) => {
    if (sortKey === key) {
      setSortOrder(prev => (prev === 'desc' ? 'asc' : 'desc'))
      return
    }
    setSortKey(key)
    setSortOrder('desc')
  }
  const sortIndicator = (key: SectorSortKey) => (sortKey === key ? (sortOrder === 'desc' ? ' ▼' : ' ▲') : '')
  const sortedSectors = (rows: SectorRow[]): SectorRow[] => {
    const list = [...rows]
    list.sort((a, b) => {
      const d = a[sortKey] - b[sortKey]
      if (d !== 0) return sortOrder === 'desc' ? -d : d
      return a.name < b.name ? -1 : 1
    })
    return list
  }

  // 情绪 + 板块：首算可能要几秒（全市场扫描），之后每分钟随快照刷新。
  useEffect(() => {
    let cancelled = false
    const load = () => {
      api.marketSentiment().then(r => { if (!cancelled) setSent(r) }).catch(() => {})
      api.marketSectors().then(r => { if (!cancelled) setSectors(r) }).catch(() => {})
    }
    load()
    const t = setInterval(load, 60_000)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

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

      {/* ---- 市场情绪：实时口径 + 30 日趋势 ---- */}
      <Card>
        <CardHead
          title="市场情绪"
          sub={`涨停/跌停 · 炸板率 · 最高板 · 晋级率 · 快照 ${sent?.realtime.updatedAt || '—'}（每分钟自动刷新；历史口径不识别 ST，家数略偏保守）`}
        />
        {sent ? (
          <>
            <div className="metrics-row market-metrics">
              <div className="metric">
                <span className="metric-label">涨停</span>
                <strong className="metric-value pos">{sent.realtime.limitUp}</strong>
                <span className="metric-hint">盘中触板 {sent.realtime.touched}</span>
              </div>
              <div className="metric">
                <span className="metric-label">跌停</span>
                <strong className="metric-value neg">{sent.realtime.limitDown}</strong>
              </div>
              <div className="metric">
                <span className="metric-label">炸板率</span>
                <strong className="metric-value">{pct(sent.realtime.breakRate)}</strong>
                <span className="metric-hint">触板未封 {sent.realtime.broke} 家</span>
              </div>
              <div className="metric">
                <span className="metric-label">最高板</span>
                <strong className="metric-value">{sent.realtime.maxBoards || '—'} 连板</strong>
              </div>
              <div className="metric">
                <span className="metric-label">晋级率</span>
                <strong className="metric-value">{pct(sent.realtime.promoteRate)}</strong>
                <span className="metric-hint">昨日涨停 {sent.realtime.yesterdayLimit} → 今日仍封 {sent.realtime.promoted}</span>
              </div>
            </div>
            <div className="spark-row">
              {([
                ['涨停家数（近30日）', sent.history.map(d => d.limitUp), '#f4577a'],
                ['跌停家数（近30日）', sent.history.map(d => d.limitDown), '#22c58b'],
                ['炸板率 %（近30日）', sent.history.map(d => d.breakRate), '#f5b544'],
                ['最高板（近30日）', sent.history.map(d => d.maxBoards), '#9ec1ff'],
              ] as Array<[string, number[], string]>).map(([label, values, color]) => (
                <div key={label} className="spark-card">
                  <span className="spark-label">{label}</span>
                  <Sparkline values={values} color={color} width={150} height={40} />
                  <span className="spark-last">{values.length ? String(values[values.length - 1]) : '—'}</span>
                </div>
              ))}
            </div>
          </>
        ) : (
          <div className="chart-empty">正在全市场扫描情绪数据（首算需要几秒）…</div>
        )}
      </Card>

      {/* ---- 连板梯队：每档晋级率 + 全量选手 ---- */}
      <Card>
        <CardHead
          title="连板梯队"
          sub={
            sent?.ladder
              ? `N进N+1 = 昨日N板冲击今日N+1板 · 高标在上 · ${sent.ladder.final ? '收盘定格' : '盘中进行时（未封仍可能回封）'} · 点击选手看K线`
              : '加载中…'
          }
        />
        {sent?.ladder ? (
          <div className="ladder">
            {sent.ladder.tiers.map(t => (
              <div key={t.height} className="ladder-tier">
                <div className="tier-head">
                  <b className="tier-name">{`${t.height}进${t.height + 1}`}</b>
                  <span className="muted">昨{t.total}只 → 晋{t.promoted.length}只</span>
                  <span className={`tier-rate ${t.promoteRate >= 30 ? 'pos' : t.promoteRate < 15 ? 'neg' : ''}`}>
                    {pct(t.promoteRate)}
                  </span>
                </div>
                <div className="tier-body">
                  {t.promoted.length > 0 && (
                    <div className="tier-group">
                      <em className="ok">晋级</em>
                      <span className="chips">
                        {t.promoted.map(s => <LadderChip key={s.symbol} s={s} onPick={kline.openDetail} />)}
                      </span>
                    </div>
                  )}
                  {t.failed.length > 0 && (
                    <div className="tier-group">
                      <em className="fail">失败</em>
                      <span className="chips">
                        {t.failed.map(s => <LadderChip key={s.symbol} s={s} failed onPick={kline.openDetail} />)}
                      </span>
                    </div>
                  )}
                </div>
              </div>
            ))}
            {sent.ladder.newBoards.length > 0 && (
              <div className="ladder-tier new-boards">
                <div className="tier-head">
                  <b className="tier-name">首板</b>
                  <span className="muted">{sent.ladder.newBoards.length} 只</span>
                </div>
                <div className="tier-body">
                  <span className="chips">
                    {sent.ladder.newBoards.map(s => <LadderChip key={s.symbol} s={s} onPick={kline.openDetail} />)}
                  </span>
                </div>
              </div>
            )}
            {!sent.ladder.tiers.length && !sent.ladder.newBoards.length && (
              <div className="chart-empty">昨日无连板梯队，今日暂无新晋首板</div>
            )}
          </div>
        ) : (
          <div className="chart-empty">正在生成梯队…</div>
        )}
      </Card>

      {/* ---- 主线板块：行业为主、概念为辅 ---- */}
      <Card>
        <CardHead
          title="主线板块"
          sub={
            sectors?.mainline
              ? `当前主线：${sectors.mainline}（涨停家数连续 ${sectors.mainlineStreak} 日居首）· 主线熄火当天不做非主线的票`
              : '暂无明显主线（无行业涨停家数居首）· 表中涨停/均涨为快照实时口径'
          }
        />
        {sectors ? (
          <>
            <div className="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>行业</th>
                    <th className={`sortable num${sortKey === 'count' ? ' sorted' : ''}`} onClick={() => toggleSort('count')}>家数{sortIndicator('count')}</th>
                    <th className={`sortable num${sortKey === 'avgChange' ? ' sorted' : ''}`} onClick={() => toggleSort('avgChange')}>平均涨幅{sortIndicator('avgChange')}</th>
                    <th className={`sortable num${sortKey === 'limitUp' ? ' sorted' : ''}`} onClick={() => toggleSort('limitUp')}>涨停{sortIndicator('limitUp')}</th>
                    <th className="num">近5日涨停</th><th>等权指数(60日)</th>
                  </tr>
                </thead>
                <tbody>
                  {sortedSectors(sectors.byIndustry).slice(0, 20).map(r => (
                    <tr key={r.name} className={sectors.mainline === r.name ? 'mainline-row' : ''}>
                      <td>
                        {r.name}
                        {sectors.mainline === r.name && <span className="concept-chip board-chip">主线{sectors.mainlineStreak}日</span>}
                      </td>
                      <td className="num">{r.count}</td>
                      <td className={`num ${r.avgChange >= 0 ? 'pos' : 'neg'}`}>{r.avgChange >= 0 ? '+' : ''}{pct(r.avgChange)}</td>
                      <td className="num">{r.limitUp || '—'}</td>
                      <td className="num muted">{(r.limitUpRecent ?? []).join(' / ')}</td>
                      <td><Sparkline values={r.index} width={130} /></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <details className="concept-cloud">
              <summary>概念板块（成员有 400 上限截断，家数偏保守）</summary>
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>概念</th>
                      <th className={`sortable num${sortKey === 'count' ? ' sorted' : ''}`} onClick={() => toggleSort('count')}>家数{sortIndicator('count')}</th>
                      <th className={`sortable num${sortKey === 'avgChange' ? ' sorted' : ''}`} onClick={() => toggleSort('avgChange')}>平均涨幅{sortIndicator('avgChange')}</th>
                      <th className={`sortable num${sortKey === 'limitUp' ? ' sorted' : ''}`} onClick={() => toggleSort('limitUp')}>涨停{sortIndicator('limitUp')}</th>
                      <th className="num">近5日涨停</th><th>等权指数(60日)</th>
                    </tr>
                  </thead>
                  <tbody>
                    {sortedSectors(sectors.byConcept ?? []).slice(0, 15).map(r => (
                      <tr key={r.name}>
                        <td>{r.name}</td>
                        <td className="num">{r.count}</td>
                        <td className={`num ${r.avgChange >= 0 ? 'pos' : 'neg'}`}>{r.avgChange >= 0 ? '+' : ''}{pct(r.avgChange)}</td>
                        <td className="num">{r.limitUp || '—'}</td>
                        <td className="num muted">{(r.limitUpRecent ?? []).join(' / ')}</td>
                        <td><Sparkline values={r.index} width={130} /></td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </details>
          </>
        ) : (
          <div className="chart-empty">正在聚合板块数据…</div>
        )}
      </Card>

      {/* 悬浮K线预览 + 选手详情弹窗（共享组件） */}
      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={symbol => navigate(`/?symbol=${symbol}`)} />
    </div>
  )
}

// LadderChip 梯队选手标签：名称 + 实时涨跌幅，点击弹K线。
function LadderChip({ s, failed, onPick }: { s: LadderStock; failed?: boolean; onPick: (t: { symbol: string; name?: string }) => void }) {
  return (
    <button
      className={`ladder-chip${failed ? ' failed' : ''}`}
      title={`${s.symbol} · ${s.height === 1 ? '首板' : `${s.height} 板`} · 点击查看K线`}
      onClick={() => onPick({ symbol: s.symbol, name: s.name })}
    >
      {s.name || s.symbol}
      <span className={s.changePct >= 0 ? 'pos' : 'neg'}>
        {s.changePct >= 0 ? '+' : ''}{s.changePct.toFixed(1)}%
      </span>
    </button>
  )
}
