import { useEffect, useRef, useState } from 'react'
import type { Candle, IndexMinute, NPSetup } from '../api'
import { api } from '../api'
import { useWatchlist } from '../watchlist'
import IntradayChart from './IntradayChart'
import MiniKline, { setupWindow } from './MiniKline'
import { CardHead, Spinner, fmt, pct } from './ui'

// 行级 K 线预览（悬浮浮窗）与详情弹窗的共享实现：筛查页与个股行情页
// 都通过 useKlinePreview() 挂在同一行上。K 线按票会话级缓存，同一票
// 反复悬浮/点开只拉一次。

const barsCache = new Map<string, Candle[]>()
function loadBars(symbol: string): Promise<Candle[]> {
  const hit = barsCache.get(symbol)
  if (hit) return Promise.resolve(hit)
  return api.bars(symbol).then(bars => {
    barsCache.set(symbol, bars ?? [])
    return bars ?? []
  })
}

export interface KlineTarget {
  symbol: string
  name?: string
  /** 形态 key（同一票多形态时区分悬浮目标），无形态留空 */
  key?: string
  /** 命中筛查形态时带上，预览/弹窗画出形态标注 */
  setup?: NPSetup
  /** 买点日（归档形态）：弹窗K线上画「买」徽章 */
  buyDate?: string
  /** 无形态时弹窗副标题（如板块·行业） */
  sub?: string
}

export interface KlineHoverState extends KlineTarget {
  bars: Candle[] | null
  top: number
  left: number
  flip: boolean
}

export interface KlineDetailState extends KlineTarget {
  bars: Candle[] | null
}

const stageLabel: Record<string, string> = { b1: '回调低吸 B1', b2: '放量突破 B2', b3: '回踩企稳 B3' }

export function useKlinePreview() {
  const hoverTimer = useRef<number | undefined>(undefined)
  const [hover, setHover] = useState<KlineHoverState | null>(null)
  const [detail, setDetail] = useState<KlineDetailState | null>(null)

  // 悬浮K线预览（防抖 300ms，浮窗不拦截鼠标，离开行即关）
  const enterRow = (target: KlineTarget, tr: HTMLElement) => {
    window.clearTimeout(hoverTimer.current)
    hoverTimer.current = window.setTimeout(() => {
      const r = tr.getBoundingClientRect()
      const width = Math.min(560, window.innerWidth - 24)
      const left = Math.min(Math.max(8, r.left), window.innerWidth - width - 8)
      const flip = r.bottom + 330 > window.innerHeight && r.top > 340
      setHover({ ...target, bars: null, top: flip ? r.top - 8 : r.bottom + 8, left, flip })
      loadBars(target.symbol)
        .then(bars => setHover(h =>
          h && h.symbol === target.symbol && (h.key ?? '') === (target.key ?? '') ? { ...h, bars } : h))
        .catch(() => {})
    }, 300)
  }
  const leaveRow = () => {
    window.clearTimeout(hoverTimer.current)
    setHover(null)
  }

  // 点击弹窗：大图 + 数值 + 跳回测页
  const openDetail = (target: KlineTarget) => {
    leaveRow()
    setDetail({ ...target, bars: null })
    loadBars(target.symbol)
      .then(bars => setDetail(d =>
        d && d.symbol === target.symbol && (d.key ?? '') === (target.key ?? '') ? { ...d, bars } : d))
      .catch(() => {})
  }
  const closeDetail = () => setDetail(null)

  useEffect(() => {
    if (!detail) return
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') setDetail(null) }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [detail])

  return { hover, detail, enterRow, leaveRow, openDetail, closeDetail }
}

export function KlinePopover({ state }: { state: KlineHoverState | null }) {
  if (!state) return null
  return (
    <div
      className="kline-pop"
      style={{ top: state.top, left: state.left, transform: state.flip ? 'translateY(-100%)' : undefined }}
    >
      <div className="kline-pop-head">
        <b>{state.symbol}</b> {state.name} · {state.setup
          ? `A段 ${state.setup.aStartDate.slice(5)}~${state.setup.aEndDate.slice(5)} +${state.setup.aRisePct.toFixed(0)}%${state.setup.hasLimitUp ? ' 涨停基因' : ''} · ${stageLabel[state.setup.stage]}`
          : '日K概览'}
      </div>
      {state.bars && state.bars.length
        ? <MiniKline bars={setupWindow(state.bars, state.setup)} setup={state.setup} height={240} />
        : <div className="kline-pop-loading">加载K线…</div>}
    </div>
  )
}

// setupFacts 形态数值卡（筛查场景，手册口径三价格齐全）。
export function setupFacts(s: NPSetup): Array<[string, string]> {
  const facts: Array<[string, string]> = [
    ['A段', `${s.aStartDate.slice(5)}~${s.aEndDate.slice(5)}`],
    ['A段涨幅', `+${s.aRisePct.toFixed(1)}%`],
    ['A段量能', `${fmt(s.aVolRatio)}×5日均量`],
    ['涨停基因', s.hasLimitUp ? '有' : '无'],
    ['颈线', fmt(s.neckline)],
    ['黄金低吸区', `${fmt(s.retr50)}~${fmt(s.retr382)}`],
    ['止损(B段低点)', fmt(s.stopLoss)],
    ['目标(C≈A)', fmt(s.target)],
    ['MA20', fmt(s.ma20)],
  ]
  if (s.stage === 'b1') {
    facts.push(
      ['回调天数', `${s.bDays}天`],
      ['回撤深度', pct(s.retrRatio * 100)],
      ['回调量比', `${fmt(s.bVolRatio)}×A段均量`],
      ['企稳信号', s.b1Triggered ? `★ ${s.b1TriggerDate}（${s.b1Signal}）` : '待触发'],
    )
  } else {
    facts.push(
      ['突破日', s.breakoutDate ?? '—'],
      ['突破价', fmt(s.breakoutPrice)],
      ['突破量能', `${fmt(s.breakoutVolRatio)}×B段均量`],
      ['突破后天数', `${s.daysSinceBreakout}天`],
    )
    if (s.stage === 'b3') {
      facts.push(
        ['回踩日', s.retestDate ?? '—'],
        ['回踩低点', s.retestLow > 0 ? fmt(s.retestLow) : '—'],
        ['缩量确认', s.retestConfirm ? '★' : '量能偏大'],
      )
    }
  }
  if (s.chaseBan) facts.push(['追高禁令', '已过追高线，放弃追入'])
  return facts
}

// basicFacts 无形态时从K线尾部派生的概览数值。
function basicFacts(bars: Candle[]): Array<[string, string]> {
  const tail = bars.slice(-20)
  if (!tail.length) return []
  const last = bars[bars.length - 1]
  const ma20 = tail.reduce((a, b) => a + b.close, 0) / tail.length
  const high = Math.max(...tail.map(b => b.high))
  const low = Math.min(...tail.map(b => b.low))
  return [
    ['最新日期', last.date],
    ['最新收盘', fmt(last.close)],
    ['MA20', fmt(ma20)],
    ['20日最高', fmt(high)],
    ['20日最低', fmt(low)],
    ['距20日高点', pct((last.close / high - 1) * 100)],
  ]
}

// 详情弹窗的K线周期：日K读会话级缓存的日线（带形态标注），其余周期按需拉。
type KfKey = 'min' | 'day' | 'week' | 'month' | 'year'

const kfPeriods: Array<{ key: KfKey; label: string }> = [
  { key: 'min', label: '分时' },
  { key: 'day', label: '日K' },
  { key: 'week', label: '周K' },
  { key: 'month', label: '月K' },
  { key: 'year', label: '年K' },
]

export function KlineDetailModal({ state, onClose, onOpenBacktest }: {
  state: KlineDetailState | null
  onClose: () => void
  onOpenBacktest: (symbol: string) => void
}) {
  const [kf, setKf] = useState<KfKey>('day')
  const [fullView, setFullView] = useState(false)
  const [periodBars, setPeriodBars] = useState<Record<string, Candle[]>>({})
  const [minute, setMinute] = useState<IndexMinute | null>(null)
  const [periodLoading, setPeriodLoading] = useState(false)
  const [periodErr, setPeriodErr] = useState('')
  const watch = useWatchlist()
  const sym = state?.symbol ?? ''
  const targetKey = state ? `${state.symbol}|${state.key ?? ''}` : ''

  // 换目标票时回到日K形态窗口并清掉上一票的非日线数据
  useEffect(() => {
    setKf('day')
    setFullView(false)
    setMinute(null)
    setPeriodErr('')
  }, [targetKey])

  // 分时/周月年K按需拉（弹窗内按 票|周期 记忆）
  useEffect(() => {
    if (!state || kf === 'day') return
    if (kf === 'min' ? minute : periodBars[`${sym}|${kf}`]) return
    let cancelled = false
    setPeriodLoading(true)
    setPeriodErr('')
    const req = kf === 'min'
      ? api.intraday(sym).then(r => { if (!cancelled) setMinute(r) })
      : api.periodBars(sym, kf).then(bars => { if (!cancelled) setPeriodBars(p => ({ ...p, [`${sym}|${kf}`]: bars })) })
    req
      .catch(e => { if (!cancelled) setPeriodErr(e instanceof Error ? e.message : '拉取失败') })
      .finally(() => { if (!cancelled) setPeriodLoading(false) })
    return () => { cancelled = true }
  }, [state, sym, kf, minute, periodBars])

  // 分时随大盘页同频刷新（仅弹窗开着且停在分时tab）
  useEffect(() => {
    if (!state || kf !== 'min') return
    const t = window.setInterval(() => { api.intraday(sym).then(setMinute).catch(() => {}) }, 60_000)
    return () => window.clearInterval(t)
  }, [state, sym, kf])

  if (!state) return null
  const s = state.setup
  const facts = s
    ? setupFacts(s)
    : state.bars && state.bars.length ? basicFacts(state.bars) : []
  const coarseBars = kf === 'min' || kf === 'day' ? null : periodBars[`${sym}|${kf}`]

  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal-card card" onClick={e => e.stopPropagation()}>
        <CardHead
          title={`${state.symbol} ${state.name ?? ''}${s && kf === 'day' ? ` · ${stageLabel[s.stage]}` : ''}`}
          sub={kf === 'day'
            ? (s ? `A段 +${s.aRisePct.toFixed(1)}%${s.hasLimitUp ? ' · 含涨停' : ''} · 数据截至 ${s.asOf}` : (state.sub ?? '日K概览'))
            : kf === 'min'
              ? '当日分时 · 白线最新价 / 黄线当日均价 · 昨收基准'
              : `${kfPeriods.find(p => p.key === kf)?.label} · 均线随周期计算`}
          right={
            <span className="focus-nav">
              <span className="range-tabs">
                {kfPeriods.map(p => (
                  <button key={p.key} className={`range-tab${kf === p.key ? ' active' : ''}`} onClick={() => setKf(p.key)}>{p.label}</button>
                ))}
              </span>
              <button
                className={`btn ghost small${watch.has(state.symbol) ? ' star-on' : ''}`}
                onClick={() => watch.toggle(state.symbol)}
                title={watch.has(state.symbol) ? '移出自选' : '加入自选'}
              >
                {watch.has(state.symbol) ? '★ 已自选' : '☆ 自选'}
              </button>
              <button className="btn ghost small" onClick={() => onOpenBacktest(state.symbol)}>到回测页深看</button>
              <button className="btn ghost small" onClick={onClose}>关闭</button>
            </span>
          }
        />
        {kf === 'min' ? (
          minute ? <IntradayChart data={minute} height={420} />
            : <div className="chart-empty">{periodLoading ? '正在拉取分时数据…' : periodErr || '暂无分时数据'}</div>
        ) : kf === 'day' ? (
          <>
            <div className="kline-view-toggle">
              <span className="range-tabs">
                <button className={`range-tab${!fullView ? ' active' : ''}`} onClick={() => setFullView(false)}>形态窗口</button>
                <button className={`range-tab${fullView ? ' active' : ''}`} onClick={() => setFullView(true)}>完整K线</button>
              </span>
              {fullView && <span className="muted-c" style={{ fontSize: 11 }}>全量历史 {state.bars?.length ?? 0} 根 · 滚轮缩放 · 拖动回看</span>}
            </div>
            {state.bars === null
              ? <Spinner text="加载K线…" />
              : state.bars.length
                ? (fullView
                    ? <MiniKline bars={state.bars} setup={s} buyDate={state.buyDate} height={420} full />
                    : <MiniKline bars={setupWindow(state.bars, s)} setup={s} buyDate={state.buyDate} height={420} />)
                : <div className="chart-empty">该股未同步日K · 可切换分时 / 周K / 月K / 年K 查看，或到档案抽屉点「同步日K」</div>}
          </>
        ) : coarseBars ? (
          <MiniKline bars={coarseBars} height={420} />
        ) : (
          <div className="chart-empty">{periodLoading ? '正在拉取K线数据…' : periodErr || '暂无数据'}</div>
        )}
        {kf === 'day' && facts.length > 0 && (
          <div className="modal-facts">
            {facts.map(([k, v]) => (
              <div key={k} className="fact"><span>{k}</span><b>{v}</b></div>
            ))}
          </div>
        )}
        <PaperBuyBar state={state} />
      </div>
    </div>
  )
}

// 模拟买入条：市价即时成交 / 限价挂单（触价自动成交），数量 + 备注
// （自动带上当时的雷达信号）；成交/挂单价以服务端为准。
function PaperBuyBar({ state }: { state: KlineDetailState }) {
  const [qty, setQty] = useState('100')
  const [note, setNote] = useState('')
  const [limit, setLimit] = useState(false)
  const [limitPrice, setLimitPrice] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState('')

  const refPrice = state.bars?.length ? state.bars[state.bars.length - 1].close : 0
  const q = Math.max(0, Math.floor(Number(qty) || 0))
  const lp = Math.max(0, Number(limitPrice) || 0)
  const px = limit && lp > 0 ? lp : refPrice
  const est = px > 0 ? px * q : 0
  const estFee = est > 0 ? Math.max(5, est * 0.00025) + est * 0.00001 : 0

  useEffect(() => {
    // 切票或参考价变化时，限价输入默认跟随参考价（用户可改）
    if (refPrice > 0 && !limitPrice) setLimitPrice(refPrice.toFixed(2))
  }, [refPrice]) // eslint-disable-line react-hooks/exhaustive-deps

  const submit = () => {
    setBusy(true); setResult('')
    api.paperOrder({
      symbol: state.symbol, name: state.name ?? '', side: 'buy', qty: q,
      note: note.trim() || undefined,
      signal: state.setup
        ? {
          stage: state.setup.stage, keyDate: state.setup.keyDate, asOf: state.setup.asOf,
          // 快照战法价位：持仓的止损/止盈提醒锚定买入当时的信号，参数后续怎么改都不动
          stopLoss: state.setup.stopLoss, target: state.setup.target,
        }
        : undefined,
      limit, limitPrice: limit ? lp : undefined,
    })
      .then(res => setResult('filled' in res
        ? `✓ 已成交 买入 ${res.filled.symbol} ${res.filled.qty} 股 @ ${fmt(res.filled.price)} · 费用 ¥${fmt(res.filled.fee + res.filled.tax)}`
        : `⏳ 已挂限价单：${res.placed.symbol} ${res.placed.qty} 股 @ ${fmt(res.placed.limitPrice)}（触价自动成交，详见模拟仓）`))
      .catch(e => setResult('✗ ' + (e instanceof Error ? e.message : '下单失败')))
      .finally(() => setBusy(false))
  }

  return (
    <div className="paper-buy">
      <span className="pb-title">模拟买入</span>
      <span className="muted-c" style={{ fontSize: 11.5 }}>
        {state.setup ? `带 ${stageLabel[state.setup.stage]} 信号` : '普通买入'} · 参考价 {refPrice > 0 ? fmt(refPrice) : '—'}
      </span>
      <select
        className="select-pill native" style={{ flex: '0 0 auto' }}
        value={limit ? 'limit' : 'market'}
        onChange={e => setLimit(e.target.value === 'limit')}
        aria-label="委托方式"
      >
        <option value="market">市价</option>
        <option value="limit">限价</option>
      </select>
      {limit && (
        <input className="input" type="number" min={0.01} step={0.01} value={limitPrice}
          onChange={e => setLimitPrice(e.target.value)} style={{ flex: '0 0 92px' }} aria-label="限价" />
      )}
      <input className="input" type="number" min={100} step={100} value={qty}
        onChange={e => setQty(e.target.value)} style={{ flex: '0 0 96px' }} aria-label="买入数量" />
      <span className="muted-c" style={{ fontSize: 11.5 }}>
        {est > 0 ? `≈¥${fmt(est + estFee)}（含费）` : ''}
      </span>
      <input className="input" value={note} onChange={e => setNote(e.target.value)}
        placeholder="备注（买入理由，可留空）" style={{ flex: '1 1 160px', minWidth: 120 }} />
      <button className="btn small primary" disabled={busy || q < 100 || q % 100 !== 0 || (limit && lp <= 0)} onClick={submit}>
        {busy ? '下单中…' : limit ? '挂限价单' : '买入'}
      </button>
      {result && <span className="pb-result" role="status">{result}</span>}
    </div>
  )
}
