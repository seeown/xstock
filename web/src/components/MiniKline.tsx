import { useEffect, useRef } from 'react'
import { init, dispose, registerOverlay } from 'klinecharts'
import type { KLineData } from 'klinecharts'
import type { Candle, ScreenSetup } from '../api'

// 筛查页的形态预览图：只读 K 线（悬浮浮窗 / 详情弹窗共用），把一个
// setup 的完整结构画出来——价位线（涨停价/前高/止损）、低吸区色带、
// 首板「板」徽章、突破菱形、企稳 ★。A股惯例：涨红跌绿。
const UP_COLOR = '#f4577a'
const DOWN_COLOR = '#22c58b'
const GOLD = '#f5b544'
const ORANGE = '#ff9600'
const BLUE = '#4f8cff'

// ztline 价位线：两个同价位锚点横跨窗口首尾，右端带文字标签。
let ztlineRegistered = false
function ensureZtLine() {
  if (ztlineRegistered) return
  ztlineRegistered = true
  registerOverlay({
    name: 'ztline',
    totalStep: 2,
    createPointFigures: ({ overlay, coordinates }) => {
      const d = overlay.extendData as { color: string; label: string; below?: boolean } | undefined
      const c0 = coordinates[0]
      const c1 = coordinates[1]
      if (!c0 || !c1 || !d) return []
      return [
        { type: 'line', attrs: { coordinates: [c0, c1] }, styles: { color: d.color, style: 'dashed', size: 1, dashedValue: [4, 3] } },
        {
          type: 'text',
          attrs: {
            x: c1.x, y: d.below ? c1.y + 11 : c1.y - 4,
            text: d.label, align: 'right', baseline: d.below ? 'top' : 'bottom',
          },
          styles: { color: d.color, size: 10 },
          ignoreEvent: true,
        },
      ]
    },
  })
}

// ztband 低吸区色带：两个锚点分别是区间上沿/下沿在窗口首尾的矩形。
let ztbandRegistered = false
function ensureZtBand() {
  if (ztbandRegistered) return
  ztbandRegistered = true
  registerOverlay({
    name: 'ztband',
    totalStep: 2,
    createPointFigures: ({ coordinates }) => {
      const c0 = coordinates[0]
      const c1 = coordinates[1]
      if (!c0 || !c1) return []
      const y = Math.min(c0.y, c1.y)
      const h = Math.max(2, Math.abs(c0.y - c1.y))
      return [
        { type: 'rect', attrs: { x: c0.x, y, width: c1.x - c0.x, height: h }, styles: { style: 'fill', color: 'rgba(245,181,68,0.10)' }, ignoreEvent: true },
        {
          type: 'text',
          attrs: { x: c0.x + 6, y: y + 3, text: '低吸区', align: 'left', baseline: 'top' },
          styles: { color: GOLD, size: 10 },
          ignoreEvent: true,
        },
      ]
    },
  })
}

// ztmark 形态标记：board=首板描边+「板」徽章；breakout=突破日金色菱形；
// star=企稳日 ★。锚定方式与回测页 nsevent 一致（蜡烛高低价）。
let ztmarkRegistered = false
function ensureZtMark() {
  if (ztmarkRegistered) return
  ztmarkRegistered = true
  registerOverlay({
    name: 'ztmark',
    totalStep: 2,
    createPointFigures: ({ overlay, coordinates }) => {
      const d = overlay.extendData as { kind: string; color: string; label: string } | undefined
      const c = coordinates[0]
      const c1 = coordinates[1]
      if (!c || !d) return []
      if (d.kind === 'board' && c1) {
        const y = Math.min(c.y, c1.y)
        const h = Math.max(2, Math.abs(c.y - c1.y))
        return [
          { type: 'rect', attrs: { x: c.x - 6, y, width: 12, height: h }, styles: { style: 'stroke', color: d.color, borderSize: 1.5 }, ignoreEvent: true },
          { type: 'text', attrs: { x: c.x, y: y - 3, text: d.label || '板', align: 'center', baseline: 'bottom' }, styles: { color: d.color, size: 11, weight: 'bold' }, ignoreEvent: true },
        ]
      }
      if (d.kind === 'breakout') {
        const size = 5
        const cy = c.y + 7 + size
        return [
          {
            type: 'polygon',
            attrs: { coordinates: [
              { x: c.x, y: cy - size }, { x: c.x + size, y: cy },
              { x: c.x, y: cy + size }, { x: c.x - size, y: cy },
            ] },
            styles: { style: 'fill', color: d.color },
            ignoreEvent: true,
          },
          { type: 'text', attrs: { x: c.x, y: cy + size + 3, text: d.label || '突破', align: 'center', baseline: 'top' }, styles: { color: d.color, size: 10 }, ignoreEvent: true },
        ]
      }
      // star
      return [
        { type: 'text', attrs: { x: c.x, y: c.y + 5, text: '★', align: 'center', baseline: 'top' }, styles: { color: d.color, size: 12, weight: 'bold' }, ignoreEvent: true },
      ]
    },
  })
}

// setupWindow 截取形态窗口：首板日前 45 根到序列末尾，最多 150 根；
// 无形态（个股页纯预览）时取最近 120 根。
export function setupWindow(bars: Candle[], setup?: ScreenSetup): Candle[] {
  if (!bars.length) return bars
  const idx = setup ? bars.findIndex(b => b.date === setup.boardDate) : -1
  let start = idx >= 0 ? idx - 45 : bars.length - 120
  if (bars.length - start > 150) start = bars.length - 150
  return bars.slice(Math.max(0, start))
}

export default function MiniKline({
  bars, setup, height = 280,
}: {
  bars: Candle[]
  setup?: ScreenSetup
  height?: number
}) {
  const hostRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = hostRef.current
    if (!el || !bars.length) return
    const data: KLineData[] = bars.map(b => ({
      timestamp: Date.parse(b.date),
      open: b.open, high: b.high, low: b.low, close: b.close, volume: b.volume,
    }))
    const chart = init(el, { locale: 'zh-CN' })
    if (!chart) return

    chart.setStyles({
      grid: { horizontal: { show: false }, vertical: { show: false } },
      candle: {
        bar: {
          upColor: UP_COLOR, downColor: DOWN_COLOR,
          upBorderColor: UP_COLOR, downBorderColor: DOWN_COLOR,
          upWickColor: UP_COLOR, downWickColor: DOWN_COLOR,
        },
        priceMark: { last: { show: false } },
      },
      indicator: {
        tooltip: { showRule: 'follow_cross' },
        bars: [{ style: 'fill', upColor: UP_COLOR, downColor: DOWN_COLOR }],
        lines: [{ style: 'solid', smooth: false, size: 1, color: '#935EBD' }],
      },
    })
    chart.setDataLoader({ getBars: ({ callback }) => callback(data, false) })
    chart.setSymbol({ ticker: setup?.symbol ?? 'local', pricePrecision: 2, volumePrecision: 0 })
    chart.setPeriod({ type: 'day', span: 1 })
    chart.createIndicator({ name: 'MA', calcParams: [20], paneId: 'candle_pane' }, false)
    chart.createIndicator('VOL')

    ensureZtLine()
    ensureZtBand()
    ensureZtMark()

    const byDate = new Map(bars.map(b => [b.date, b]))
    const lastTs = Date.parse(bars[bars.length - 1].date)
    if (setup) {
      const firstTs = Date.parse(bars[0].date)
      const priceLine = (price: number, color: string, label: string, below = false) => {
        if (!(price > 0)) return
        chart.createOverlay({
          name: 'ztline',
          points: [{ timestamp: firstTs, value: price }, { timestamp: lastTs, value: price }],
          extendData: { color, label, below },
        })
      }
      // 低吸区色带（沿窗口首尾）
      if (setup.b1ZoneLow > 0 && setup.b1ZoneHigh > setup.b1ZoneLow) {
        chart.createOverlay({
          name: 'ztband',
          points: [
            { timestamp: firstTs, value: setup.b1ZoneHigh },
            { timestamp: lastTs, value: setup.b1ZoneLow },
          ],
        })
      }
      priceLine(setup.boardClose, GOLD, `涨停价 ${setup.boardClose.toFixed(2)}`)
      priceLine(setup.swingHigh, BLUE, `前高 ${setup.swingHigh.toFixed(2)}`)
      const stop = setup.stage === 'b1' ? setup.stopLossB1 : setup.stage === 'b2' ? setup.stopLossB2 : setup.stopLossB3
      priceLine(stop, '#aab6cc', `止损 ${stop.toFixed(2)}`, true)

      const mark = (date: string, kind: 'board' | 'breakout' | 'star', color: string, label = '') => {
        const bar = byDate.get(date)
        if (!bar) return
        const ts = Date.parse(date)
        const points = [{ timestamp: ts, value: bar.low }]
        if (kind === 'board') points.push({ timestamp: ts, value: bar.high })
        chart.createOverlay({ name: 'ztmark', points, extendData: { kind, color, label } })
      }
      mark(setup.boardDate, 'board', ORANGE)
      if (setup.breakoutDate) mark(setup.breakoutDate, 'breakout', GOLD)
      if (setup.b1Triggered && setup.b1TriggerDate) mark(setup.b1TriggerDate, 'star', ORANGE)
      if (setup.retestTriggered && setup.retestTriggerDate) mark(setup.retestTriggerDate, 'star', GOLD)
    }

    // 适配窗口宽度：整段窗口撑满画布，右端对齐最新K线。
    const width = el.clientWidth || 560
    chart.setBarSpace(Math.min(18, Math.max(2, Math.round((width * 0.92) / bars.length))))
    chart.scrollToTimestamp(lastTs)

    return () => { dispose(el) }
    // setup 字段变化即重建（同一弹窗内不会高频变化，重建成本低）。
  }, [bars, setup])

  return <div ref={hostRef} style={{ width: '100%', height }} />
}
