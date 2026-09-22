import { useEffect, useRef } from 'react'
import { init, dispose } from 'klinecharts'
import type { Chart, KLineData } from 'klinecharts'
import type { Candle, Signal, Trade } from '../api'

export const MA_PERIODS = [5, 10, 20, 30, 60, 250]

// The default theme ships only 5 indicator line colors; a 6th series (年线)
// would render with no color at all, so supply one per MA line explicitly.
const MA_COLORS = ['#FF9600', '#935EBD', '#4C6BF5', '#E11D74', '#01C5C4', '#F5B544']

// A股惯例：涨红跌绿。
const UP_COLOR = '#f4577a'
const DOWN_COLOR = '#22c58b'

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
  bars, signals, trades, focus, onMarkClick, height = 420,
}: {
  bars: Candle[]
  signals?: Signal[]
  trades?: Trade[]
  focus?: FocusRange | null
  onMarkClick?: (mark: ChartMark) => void
  height?: number
}) {
  const hostRef = useRef<HTMLDivElement>(null)
  const chartRef = useRef<Chart | null>(null)

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
        horizontal: { show: false },
        vertical: { show: false },
      },
      candle: {
        bar: {
          upColor: UP_COLOR, downColor: DOWN_COLOR,
          upBorderColor: UP_COLOR, downBorderColor: DOWN_COLOR,
          upWickColor: UP_COLOR, downWickColor: DOWN_COLOR,
        },
        priceMark: {
          last: { upColor: UP_COLOR, downColor: DOWN_COLOR },
        },
      },
      indicator: {
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

    // Buy/sell annotations; clicking one lets the page focus this trade.
    const mark = (ts: number, text: string, payload: ChartMark) => {
      chart.createOverlay({
        name: 'simpleAnnotation',
        points: [{ timestamp: ts }],
        extendData: text,
        onClick: () => onMarkClick?.(payload),
      })
    }
    if (focus) {
      for (const s of signals ?? []) {
        mark(Date.parse(s.date), `信 ${s.date.slice(5)}`, { kind: 'signal', signal: s })
      }
    }
    for (const t of trades ?? []) {
      const ret = t.returnPct >= 0 ? `+${t.returnPct.toFixed(1)}%` : `${t.returnPct.toFixed(1)}%`
      mark(Date.parse(t.buyDate), `买 ${t.buyDate.slice(5)}`, { kind: 'trade', trade: t })
      mark(Date.parse(t.sellDate), `卖${ret}`, { kind: 'trade', trade: t })
    }

    if (focus) {
      applyFocus(chart, el, bars, focus)
    }

    return () => {
      chartRef.current = null
      dispose(el)
    }
    // onMarkClick must be a stable callback (useCallback) at the call site,
    // otherwise the chart rebuilds on every parent render.
  }, [bars, signals, trades, focus, onMarkClick])

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
