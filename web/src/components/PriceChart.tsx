import { useEffect, useMemo, useRef, useState } from 'react'
import type { Candle, Signal, Trade } from '../api'

interface Props {
  bars: Candle[]
  signals: Signal[]
  trades: Trade[]
}

export default function PriceChart({ bars, signals, trades }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(900)
  const height = 320
  const pad = { l: 60, r: 18, t: 20, b: 36 }

  useEffect(() => {
    const el = hostRef.current
    if (!el) return
    const ro = new ResizeObserver(entries => {
      const w = entries[0]?.contentRect.width ?? 900
      setWidth(Math.max(w, 240))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const chart = useMemo(() => {
    if (!bars.length) return null
    const base = bars.map(b => b.close)
    const rawMin = Math.min(...base)
    const rawMax = Math.max(...base)
    const min = rawMin * 0.97
    const max = rawMax * 1.03
    const count = base.length
    const iw = width - pad.l - pad.r
    const ih = height - pad.t - pad.b
    const x = (i: number) => pad.l + (i * iw) / Math.max(count - 1, 1)
    const y = (v: number) => pad.t + ((max - v) * ih) / (max - min || 1)
    const line = base.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ')
    const area = `${line} L${x(count - 1).toFixed(1)},${height - pad.b} L${pad.l},${height - pad.b} Z`
    const gridYs = Array.from({ length: 5 }, (_, k) => min + ((max - min) * k) / 4)
    const tickIdx = Array.from({ length: Math.min(5, count) }, (_, k) =>
      count <= 5 ? k : Math.round((k * (count - 1)) / 4),
    )
    const indexForDate = (d: string) => bars.findIndex(b => b.date === d)
    return { base, min, max, x, y, line, area, gridYs, tickIdx, indexForDate }
  }, [bars, width])

  const empty = !bars.length || !chart
  const g = chart ?? {
    base: [] as number[], min: 0, max: 0, x: () => 0, y: () => 0,
    line: '', area: '', gridYs: [] as number[], tickIdx: [] as number[], indexForDate: () => -1,
  }
  const { base, min, max, x, y, line, area, gridYs, tickIdx, indexForDate } = g

  return (
    <div className="chart-host" ref={hostRef}>
      {empty ? (
        <div className="chart-empty">暂无行情数据</div>
      ) : (
        <>
          <svg width={width} height={height} role="img" aria-label="收盘价与策略信号">
            <defs>
              <linearGradient id="areaFill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#4f8cff" stopOpacity="0.35" />
                <stop offset="100%" stopColor="#4f8cff" stopOpacity="0" />
              </linearGradient>
            </defs>
            {gridYs.map((v, k) => {
              const gy = y(v)
              return (
                <g key={k}>
                  <line className="grid" x1={pad.l} x2={width - pad.r} y1={gy} y2={gy} />
                  <text className="axis-label" x={pad.l - 10} y={gy + 4} textAnchor="end">
                    {v.toFixed(v >= 100 ? 0 : 2)}
                  </text>
                </g>
              )
            })}
            {tickIdx.map(i => (
              <text key={i} className="axis-label" x={x(i)} y={height - 10} textAnchor="middle">
                {bars[i].date.slice(5)}
              </text>
            ))}
            <path className="area" d={area} fill="url(#areaFill)" />
            <path className="close-line" d={line} />
            {signals.map(s => {
              const i = indexForDate(s.date)
              if (i < 0) return null
              return (
                <circle key={`s-${s.date}`} className="signal-dot" cx={x(i)} cy={y(base[i])} r={5}>
                  <title>{`${s.date} N字信号 · 突破 ${s.breakoutPrice.toFixed(2)}`}</title>
                </circle>
              )
            })}
            {trades.map(t => {
              const i = indexForDate(t.buyDate)
              if (i < 0) return null
              const cx = x(i)
              const cy = y(base[i]) - 10
              return (
                <path key={`t-${t.buyDate}`} className="trade-marker" d={`M${cx},${cy} l5.5,9 h-11 z`}>
                  <title>{`${t.buyDate} 模拟买入 ${t.buyPrice.toFixed(2)}`}</title>
                </path>
              )
            })}
          </svg>
          <div className="chart-range">
            {bars[0].date} ~ {bars[bars.length - 1].date}
          </div>
        </>
      )}
    </div>
  )
}
