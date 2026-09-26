import { useEffect, useRef, useState } from 'react'
import { init, dispose, registerOverlay } from 'klinecharts'
import type { Chart, KLineData } from 'klinecharts'
import type { Candle, Signal, Trade } from '../api'
import { exitReasonText } from './ui'

export const MA_PERIODS = [5, 10, 20, 30, 60, 250]

// The default theme ships only 5 indicator line colors; a 6th series (年线)
// would render with no color at all, so supply one per MA line explicitly.
const MA_COLORS = ['#2DD4BF', '#A5B4FC', '#38BDF8', '#FFC46B', '#A78BFA', '#94A3B8']

// A股惯例：涨红跌绿。
const UP_COLOR = '#FF6E66'
const DOWN_COLOR = '#3DDC97'

// klinecharts 的 overlay 文字默认带品牌蓝实底(#1677FF)+白字，自定义样式
// 增量合并——必须显式清零背景/边框/内边距，否则文字后面衬一块蓝底。
function plainText(color: string, size: number, weight = 'normal') {
  return {
    color, size, weight,
    backgroundColor: 'transparent',
    borderColor: 'transparent',
    borderSize: 0,
    borderRadius: 0,
    paddingLeft: 0, paddingRight: 0, paddingTop: 0, paddingBottom: 0,
  }
}

// SMA over closes, shared with page-level stat tiles.
export function computeMA(bars: Candle[], period: number): Array<number | null> {
  const out: Array<number | null> = []
  let sum = 0
  for (let i = 0; i < bars.length; i++) {
    sum += bars[i].close
    if (i >= period) sum -= bars[i - period].close
    out.push(i >= period - 1 ? sum / period : null)
  }
  return out
}

// nsmark: 全景模式下的极简买卖标记——纯色小三角锚定在 K 线高低价上，
// 无文字无引导线（simpleAnnotation 的文字在全景下会糊成一片）。
// extendData: { dir: 1 买(▲在线下) | -1 卖(▼在线上), color }
// nsevent: 聚焦模式的事件标记——买卖日蜡烛描边高亮 + 紧贴K线的三角与
// 小字（B 价 / S 收益% / 信），全部锚定在蜡烛高低价上，不飘到图表顶部。
// extendData: { kind: 'buy'|'sell'|'signal', color, label, ring }
let nseventRegistered = false
function ensureNsEvent() {
  if (nseventRegistered) return
  nseventRegistered = true
  registerOverlay({
    name: 'nsevent',
    totalStep: 1,
    createPointFigures: ({ overlay, coordinates }) => {
      const c = coordinates[0]
      const c1 = coordinates[1]
      const d = overlay.extendData as { kind: string; color: string; label: string; ring: string } | undefined
      if (!c || !d) return []
      const figures: Array<{ type: string; attrs: unknown; styles?: unknown; ignoreEvent?: boolean }> = []
      if ((d.kind === 'buy' || d.kind === 'sell' || d.kind === 'board') && c1) {
        // 事件日蜡烛描边：竖框覆盖当根K线高低价范围
        const y = Math.min(c.y, c1.y)
        const h = Math.max(2, Math.abs(c.y - c1.y))
        figures.push({
          type: 'rect',
          attrs: { x: c.x - 6, y, width: 12, height: h },
          styles: { style: 'stroke', color: d.ring, borderColor: d.ring, borderSize: 1.5 },
          ignoreEvent: true,
        })
      }
      const gap = 4
      if (d.kind === 'buy') {
        // 单个紧贴K线下方的 B 徽章（白字，粗一点）
        figures.push({
          type: 'text',
          attrs: { x: c.x, y: c.y + gap, text: 'B', align: 'center', baseline: 'top' },
          styles: plainText(d.color, 13, 'bold'),
          ignoreEvent: true,
        })
      } else if (d.kind === 'sell') {
        // 单个紧贴K线上方的 S 徽章
        figures.push({
          type: 'text',
          attrs: { x: c.x, y: c.y - gap, text: 'S', align: 'center', baseline: 'bottom' },
          styles: plainText(d.color, 13, 'bold'),
          ignoreEvent: true,
        })
      } else if (d.kind === 'board' && c1) {
        // 首板日：橙色描边 + K线上方的「板」徽章
        figures.push({
          type: 'text',
          attrs: { x: c.x, y: c1.y - gap, text: '板', align: 'center', baseline: 'bottom' },
          styles: plainText(d.color, 11, 'bold'),
          ignoreEvent: true,
        })
      } else {
        // 信号日：金色小菱形 + 「信」
        const size = 5
        const cy = c.y + gap + size
        figures.push({
          type: 'polygon',
          attrs: { coordinates: [
            { x: c.x, y: cy - size }, { x: c.x + size, y: cy },
            { x: c.x, y: cy + size }, { x: c.x - size, y: cy },
          ] },
          styles: { style: 'fill', color: d.color, borderColor: 'transparent', borderSize: 0 },
          ignoreEvent: true,
        })
        if (d.label) {
          figures.push({
            type: 'text',
            attrs: { x: c.x, y: cy + size + 10, text: d.label, align: 'center', baseline: 'top' },
            styles: plainText(d.color, 10),
            ignoreEvent: true,
          })
        }
      }
      return figures
    },
  })
}

let nsmarkRegistered = false
function ensureNsMark() {
  if (nsmarkRegistered) return
  nsmarkRegistered = true
  registerOverlay({
    name: 'nsmark',
    totalStep: 1,
    createPointFigures: ({ overlay, coordinates }) => {
      const c = coordinates[0]
      const d = overlay.extendData as { dir: number; color: string } | undefined
      if (!c || !d) return []
      const size = 6, gap = 3
      const tri = d.dir === 1
        ? [
            { x: c.x, y: c.y + gap + size * 2 },
            { x: c.x - size, y: c.y + gap },
            { x: c.x + size, y: c.y + gap },
          ]
        : [
            { x: c.x, y: c.y - gap - size * 2 },
            { x: c.x - size, y: c.y - gap },
            { x: c.x + size, y: c.y - gap },
          ]
      return [
        { type: 'polygon', attrs: { coordinates: tri }, styles: { style: 'fill', color: d.color, borderColor: 'transparent', borderSize: 0 } },
      ]
    },
  })
}

export interface ChartMark {
  kind: 'signal' | 'trade'
  signal?: Signal
  trade?: Trade
}

export interface FocusRange {
  from: string
  to: string
}

// Candlestick chart on klinecharts v10: main pane shows MA5/10/20/30/60/年线
// (250), sub pane shows volume. Zoom/pan/crosshair are built in. v10 loads
// data through a DataLoader instead of applyNewData; our dataset is fully
// local, so the loader hands back the whole series on init.
//
// Signals/trades are drawn as clickable annotations; clicking one reports
// back through onMarkClick so the page can zoom the chart into that trade's
// neighbourhood. In panorama only 买/卖 marks are drawn (信 marks are too
// dense at full zoom); once focused, signal marks join for that window.
export default function KlineChart({
  bars, signals, trades, focus, onMarkClick, onResetFocus, height = 420,
}: {
  bars: Candle[]
  signals?: Signal[]
  trades?: Trade[]
  focus?: FocusRange | null
  onMarkClick?: (mark: ChartMark) => void
  onResetFocus?: () => void
  height?: number
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<Chart | null>(null)
  const [tip, setTip] = useState<{ x: number; y: number; mark: ChartMark } | null>(null)

  useEffect(() => {
    const el = hostRef.current
    if (!el || !bars.length) return

    const data: KLineData[] = bars.map(b => ({
      timestamp: Date.parse(b.date),
      open: b.open,
      high: b.high,
      low: b.low,
      close: b.close,
      volume: b.volume,
    }))

    // Rebuild on every series swap; a few thousand candles re-init instantly
    // and this keeps zoom state consistent with the requested range.
    if (chartRef.current) {
      dispose(chartRef.current)
      chartRef.current = null
    }
    const chart = init(el, { locale: 'zh-CN' })
    if (!chart) return
    chartRef.current = chart

    chart.setStyles({
      grid: {
        horizontal: { show: true, color: 'rgba(255,255,255,.06)', size: 1 },
        vertical: { show: false },
      },
      candle: {
        bar: {
          upColor: UP_COLOR, downColor: DOWN_COLOR,
          upBorderColor: UP_COLOR, downBorderColor: DOWN_COLOR,
          upWickColor: UP_COLOR, downWickColor: DOWN_COLOR,
        },
        priceMark: {
          // 右侧最新价线与标签：常驻挡视线，关闭
          last: { show: false, upColor: UP_COLOR, downColor: DOWN_COLOR },
        },
      },
      indicator: {
        // 均线/VOL 的数值图例只在十字光标悬停时出现，平时不占画面
        tooltip: { showRule: 'follow_cross' },
        bars: [{ style: 'fill', upColor: UP_COLOR, downColor: DOWN_COLOR }],
        lines: MA_COLORS.map(color => ({ style: 'solid', smooth: false, size: 1, dashedValue: [2, 2], color })),
      },
    })
    chart.setDataLoader({
      getBars: ({ callback }) => callback(data, false),
    })
    // v10 only asks the loader for bars once both symbol and period are set.
    chart.setSymbol({ ticker: 'local', pricePrecision: 2, volumePrecision: 0 })
    chart.setPeriod({ type: 'day', span: 1 })
    chart.createIndicator({ name: 'MA', calcParams: MA_PERIODS, paneId: 'candle_pane' }, false)
    chart.createIndicator('VOL')

    // 买卖标注：聚焦时画带日期/收益的文字标注；全景只画锚定在高低价上的
    // 小三角（买=K线下方红▲，卖=上方▼按盈亏红/绿），避免文字糊成一团。
    // 点击任一标记都会让页面聚焦到该笔交易。
    if (focus) {
      ensureNsEvent()
      const barByDate = new Map(bars.map(b => [b.date, b]))
      const event = (pts: Array<{ timestamp: number; value: number }>, data: { kind: string; color: string; label: string; ring: string }, payload: ChartMark) => {
        chart.createOverlay({
          name: 'nsevent',
          points: pts,
          extendData: data,
          onClick: () => onMarkClick?.(payload),
          onMouseEnter: e => setTip({ x: e.pageX ?? 0, y: e.pageY ?? 0, mark: payload }),
          onMouseLeave: () => setTip(null),
        })
      }
      for (const g of signals ?? []) {
        const bar = barByDate.get(g.date)
        if (!bar) continue
        const ts = Date.parse(g.date)
        event([{ timestamp: ts, value: bar.low }], { kind: 'signal', color: '#f5b544', label: '', ring: '#f5b544' }, { kind: 'signal', signal: g })
        // 首板回调策略：首板日画橙色描边 + 「板」徽章，构成 板▲→回调→突破 的三段标注。
        if (g.boardDate) {
          const bBar = barByDate.get(g.boardDate)
          if (bBar) {
            event(
              [{ timestamp: Date.parse(g.boardDate), value: bBar.low }, { timestamp: Date.parse(g.boardDate), value: bBar.high }],
              { kind: 'board', color: '#ff9600', label: '', ring: '#ff9600' },
              { kind: 'signal', signal: g },
            )
          }
        }
      }
      for (const t of trades ?? []) {
        const buyBar = barByDate.get(t.buyDate)
        const sellBar = barByDate.get(t.sellDate)
        const sellColor = '#ffffff'
        if (buyBar) {
          const ts = Date.parse(t.buyDate)
          event(
            [{ timestamp: ts, value: buyBar.low }, { timestamp: ts, value: buyBar.high }],
            { kind: 'buy', color: '#ffffff', label: 'B', ring: '#ffffff' },
            { kind: 'trade', trade: t },
          )
        }
        if (sellBar) {
          const ts = Date.parse(t.sellDate)
          event(
            [{ timestamp: ts, value: sellBar.low }, { timestamp: ts, value: sellBar.high }],
            { kind: 'sell', color: sellColor, label: 'S', ring: sellColor },
            { kind: 'trade', trade: t },
          )
        }
      }
    } else {
      ensureNsMark()
      const barByDate = new Map(bars.map(b => [b.date, b]))
      const triMark = (date: string, dir: number, color: string, payload: ChartMark) => {
        const bar = barByDate.get(date)
        if (!bar) return
        chart.createOverlay({
          name: 'nsmark',
          points: [{ timestamp: Date.parse(date), value: dir === 1 ? bar.low : bar.high }],
          extendData: { dir, color },
          onClick: () => onMarkClick?.(payload),
          onMouseEnter: e => setTip({ x: e.pageX ?? 0, y: e.pageY ?? 0, mark: payload }),
          onMouseLeave: () => setTip(null),
        })
      }
      for (const t of trades ?? []) {
        triMark(t.buyDate, 1, '#f4577a', { kind: 'trade', trade: t })
        triMark(t.sellDate, -1, t.returnPct >= 0 ? '#f4577a' : '#22c58b', { kind: 'trade', trade: t })
      }
    }

    if (focus) {
      applyFocus(chart, el, bars, focus)
    }

    // 聚焦态下双击图表任意处返回全景。
    const onDbl = () => {
      if (focus) onResetFocus?.()
    }
    el.addEventListener('dblclick', onDbl)

    return () => {
      el.removeEventListener('dblclick', onDbl)
      chartRef.current = null
      dispose(el)
    }
    // onMarkClick must be a stable callback (useCallback) at the call site,
    // otherwise the chart rebuilds on every parent render.
  }, [bars, signals, trades, focus, onMarkClick, onResetFocus])

  if (!bars.length) {
    return (
      <div className="chart-empty" style={{ height }}>
        暂无K线数据
      </div>
    )
  }

  return (
    <div className="kline-wrap" style={{ height }}>
      <div ref={hostRef} style={{ width: '100%', height: '100%' }} />
      {tip && (
        <div className="kline-tip" style={{ left: tip.x + 14, top: tip.y - 8 }}>
          {tip.mark.kind === 'trade' && tip.mark.trade
            ? (() => {
                const t = tip.mark.trade
                return (
                  <>
                    <b>{t.buyDate} 买入 @{t.buyPrice.toFixed(2)}</b>
                    <span>卖出 {t.sellDate} @{t.sellPrice.toFixed(2)}</span>
                    <span className={t.returnPct >= 0 ? 'pos' : 'neg'}>
                      收益 {t.returnPct >= 0 ? '+' : ''}{t.returnPct.toFixed(2)}% · {exitReasonText[t.exitReason] ?? t.exitReason}
                    </span>
                    <em>点击放大到该笔交易 · 双击图面返回全景</em>
                  </>
                )
              })()
            : tip.mark.signal
              ? (() => {
                  const g = tip.mark.signal
                  return (
                    <>
                      <b>{g.date} {g.boardDate ? '首板回调信号' : 'N 字信号'}</b>
                      {g.boardDate && (
                        <span>首板 {g.boardDate} · 板日量比 {g.boardVolRatio?.toFixed(1) ?? '—'}×</span>
                      )}
                      <span>突破 {g.breakoutPrice.toFixed(2)} · 量比 {g.volumeRatio.toFixed(1)}×</span>
                      <span>段内涨 {g.risePct.toFixed(1)}% · 回调 {g.pullbackPct.toFixed(1)}%{g.pullbackDays ? ` / ${g.pullbackDays} 天` : ''}</span>
                      <span className={g.dayChangePct >= 0 ? 'pos' : 'neg'}>
                        当日 {g.dayChangePct >= 0 ? '+' : ''}{g.dayChangePct.toFixed(2)}%
                      </span>
                      <em>点击放大到该信号 · 双击图面返回全景</em>
                    </>
                  )
                })()
              : null}
        </div>
      )}
    </div>
  )
}

// applyFocus zooms so the [from, to] window fills the pane: bar width is
// derived from the container size, then the view centres on the window's
// midpoint (v10 has no setVisibleRange; setBarSpace + scrollToTimestamp
// together add up to it).
function applyFocus(chart: Chart, el: HTMLElement, bars: Candle[], focus: FocusRange) {
  const fromTs = Date.parse(focus.from)
  const toTs = Date.parse(focus.to)
  const count = bars.filter(b => {
    const t = Date.parse(b.date)
    return t >= fromTs && t <= toTs
  }).length
  const width = el.clientWidth || 900
  const space = Math.min(24, Math.max(2, Math.round((width * 0.92) / Math.max(count, 20))))
  chart.setBarSpace(space)
  chart.scrollToTimestamp(Math.round((fromTs + toTs) / 2))
}
