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

// Candlestick chart on klinecharts v10: main pane shows MA5/10/20/30/60/年线
// (250), sub pane shows volume. Zoom/pan/crosshair are built in. v10 loads data
// through a DataLoader instead of applyNewData; our dataset is fully local, so
// the loader hands back the whole series on init. Optional N-pattern signals
// and simulated trades are drawn as annotations on the candles.
export default function KlineChart({
  bars, signals, trades, height = 420,
}: { bars: Candle[]; signals?: Signal[]; trades?: Trade[]; height?: number }) {
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
    chart.setSymbol({ ticker: bars.length ? 'local' : 'none', pricePrecision: 2, volumePrecision: 0 })
    chart.setPeriod({ type: 'day', span: 1 })
    chart.createIndicator({ name: 'MA', calcParams: MA_PERIODS, paneId: 'candle_pane' }, false)
    chart.createIndicator('VOL')

    // Buy/sell annotations so the entry points are actually readable on a
    // multi-year chart (the line chart drowns them out).
    const tsOf = (date: string) => Date.parse(date)
    for (const s of signals ?? []) {
      chart.createOverlay({ name: 'simpleAnnotation', points: [{ timestamp: tsOf(s.date) }], extendData: `信 ${s.date.slice(5)}` })
    }
    for (const t of trades ?? []) {
      const ret = t.returnPct >= 0 ? `+${t.returnPct.toFixed(1)}%` : `${t.returnPct.toFixed(1)}%`
      chart.createOverlay({ name: 'simpleAnnotation', points: [{ timestamp: tsOf(t.buyDate) }], extendData: `买 ${t.buyDate.slice(5)}` })
      chart.createOverlay({ name: 'simpleAnnotation', points: [{ timestamp: tsOf(t.sellDate) }], extendData: `卖${ret}` })
    }

    return () => {
      chartRef.current = null
      dispose(el)
    }
  }, [bars, signals, trades])

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
