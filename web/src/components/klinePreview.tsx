import { useEffect, useRef, useState } from 'react'
import type { Candle, NPSetup } from '../api'
import { api } from '../api'
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

export function KlineDetailModal({ state, onClose, onOpenBacktest }: {
  state: KlineDetailState | null
  onClose: () => void
  onOpenBacktest: (symbol: string) => void
}) {
  if (!state) return null
  const s = state.setup
  const facts = s
    ? setupFacts(s)
    : state.bars && state.bars.length ? basicFacts(state.bars) : []
  return (
    <div className="modal-mask" onClick={onClose}>
      <div className="modal-card card" onClick={e => e.stopPropagation()}>
        <CardHead
          title={`${state.symbol} ${state.name ?? ''}${s ? ` · ${stageLabel[s.stage]}` : ''}`}
          sub={s ? `A段 +${s.aRisePct.toFixed(1)}%${s.hasLimitUp ? ' · 含涨停' : ''} · 数据截至 ${s.asOf}` : (state.sub ?? '日K概览')}
          right={
            <span className="focus-nav">
              <button className="btn ghost small" onClick={() => onOpenBacktest(state.symbol)}>到回测页深看</button>
              <button className="btn ghost small" onClick={onClose}>关闭</button>
            </span>
          }
        />
        {state.bars && state.bars.length
          ? <MiniKline bars={setupWindow(state.bars, s)} setup={s} height={420} />
          : <Spinner text="加载K线…" />}
        {facts.length > 0 && (
          <div className="modal-facts">
            {facts.map(([k, v]) => (
              <div key={k} className="fact"><span>{k}</span><b>{v}</b></div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
