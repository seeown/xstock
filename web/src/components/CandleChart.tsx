import { useEffect, useMemo, useRef, useState } from 'react'
import type { Candle } from '../api'

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

const UP = '#22c58b'
const DOWN = '#f4577a'
const MA5_COLOR = '#f5b544'
const MA10_COLOR = '#a78bfa'

export default function CandleChart({ bars }: { bars: Candle[] }) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(960)
  const [hover, setHover] = useState<number | null>(null)
  const height = 400
  const pad = { l: 64, r: 72, t: 18, b: 34 }

  useEffect(() => {
    const el = hostRef.current
    if (!el) return
    const ro = new ResizeObserver(entries => {
      const w = entries[0]?.contentRect.width ?? 960
      setWidth(Math.max(w, 240))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const ma5 = useMemo(() => computeMA(bars, 5), [bars])
  const ma10 = useMemo(() => computeMA(bars, 10), [bars])

  const geo = useMemo(() => {
    if (!bars.length) return null
    const lows = bars.map(b => b.low)
    const highs = bars.map(b => b.high)
    const maVals = [...ma5, ...ma10].filter((v): v is number => v !== null)
    const rawMin = Math.min(...lows, ...maVals)
    const rawMax = Math.max(...highs, ...maVals)
    const span = rawMax - rawMin || 1
    const min = rawMin - span * 0.04
    const max = rawMax + span * 0.04
    const iw = width - pad.l - pad.r
    const ih = height - pad.t - pad.b
    const n = bars.length
    const slot = iw / Math.max(n, 1)
    const candleW = Math.max(1.2, Math.min(slot * 0.66, 14))
    const x = (i: number) => pad.l + slot * (i + 0.5)
    const y = (v: number) => pad.t + ((max - v) * ih) / (max - min)
    const maPath = (ma: Array<number | null>) => {
      let d = ''
      let started = false
      ma.forEach((v, i) => {
        if (v === null) return
        d += `${started ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)} `
        started = true
      })
      return d
    }
    const gridYs = Array.from({ length: 5 }, (_, k) => min + ((max - min) * k) / 4)
    const tickCount = Math.min(6, n)
    const tickIdx = Array.from({ length: tickCount }, (_, k) =>
      n <= tickCount ? k : Math.round((k * (n - 1)) / (tickCount - 1)),
    )
    return { min, max, x, y, slot, candleW, maPath, gridYs, tickIdx }
  }, [bars, ma5, ma10, width])

  const empty = !bars.length || !geo
  const g = geo ?? {
    min: 0, max: 0, x: () => 0, y: () => 0, slot: 1, candleW: 1,
    maPath: () => '', gridYs: [] as number[], tickIdx: [] as number[],
  }
  const { min, max, x, y, slot, candleW, maPath, gridYs, tickIdx } = g
  const last = bars[bars.length - 1]
  const prev = bars.length > 1 ? bars[bars.length - 2] : null
  const dayChange = prev ? ((last.close - prev.close) / prev.close) * 100 : 0
  const hb = hover !== null ? bars[hover] : null
  const tooltipLeft = hover !== null ? Math.min(Math.max(x(hover) + 14, 8), width - 208) : 0

  return (
    <div className="candle-host" ref={hostRef}>
      {empty ? (
        <div className="chart-empty">暂无K线数据</div>
      ) : (
        <>
          <svg
            width={width}
            height={height}
            onMouseLeave={() => setHover(null)}
            onMouseMove={e => {
              const rect = e.currentTarget.getBoundingClientRect()
              const px = e.clientX - rect.left
              const i = Math.floor((px - pad.l) / slot)
              setHover(i >= 0 && i < bars.length ? i : null)
            }}
          >
            {gridYs.map((v, k) => (
              <g key={k}>
                <line className="grid" x1={pad.l} x2={width - pad.r} y1={y(v)} y2={y(v)} />
                <text className="axis-label" x={pad.l - 8} y={y(v) + 4} textAnchor="end">{v.toFixed(v >= 1000 ? 0 : 2)}</text>
              </g>
            ))}
            {tickIdx.map(i => (
              <text key={i} className="axis-label" x={x(i)} y={height - 10} textAnchor="middle">{bars[i].date.slice(2)}</text>
            ))}
            {bars.map((b, i) => {
              const up = b.close >= b.open
              const color = up ? UP : DOWN
              const yo = y(b.open)
              const yc = y(b.close)
              const top = Math.min(yo, yc)
              const bodyH = Math.max(Math.abs(yc - yo), 1)
              return (
                <g key={b.date}>
                  <line x1={x(i)} x2={x(i)} y1={y(b.high)} y2={y(b.low)} stroke={color} strokeWidth={1} />
                  <rect
                    x={x(i) - candleW / 2}
                    y={top}
                    width={candleW}
                    height={bodyH}
                    fill={up ? 'transparent' : color}
                    stroke={color}
                    strokeWidth={1}
                  />
                </g>
              )
            })}
            <path className="ma-line ma5" d={maPath(ma5)} />
            <path className="ma-line ma10" d={maPath(ma10)} />
            <line x1={pad.l} x2={width - pad.r} y1={y(last.close)} y2={y(last.close)} className="last-price-line" stroke={last.close >= (prev?.close ?? last.close) ? UP : DOWN} strokeDasharray="3 4" />
            <rect x={width - pad.r + 4} y={y(last.close) - 10} width={pad.r - 10} height={20} rx={4} className="last-price-tag" fill={last.close >= (prev?.close ?? last.close) ? UP : DOWN} />
            <text x={width - pad.r + 8} y={y(last.close) + 4} className="last-price-text">{last.close.toFixed(2)}</text>
            {hover !== null && (
              <line className="crosshair" x1={x(hover)} x2={x(hover)} y1={pad.t} y2={height - pad.b} />
            )}
          </svg>
          {hb && (
            <div className="candle-tooltip" style={{ left: tooltipLeft, top: 12 }}>
              <div className="tt-date">{hb.date}</div>
              <div>开 <b>{hb.open.toFixed(2)}</b>　高 <b>{hb.high.toFixed(2)}</b></div>
              <div>低 <b>{hb.low.toFixed(2)}</b>　收 <b className={hb.close >= hb.open ? 'pos' : 'neg'}>{hb.close.toFixed(2)}</b></div>
              <div>涨跌 <b className={dayChangeOf(bars, hover!) >= 0 ? 'pos' : 'neg'}>{dayChangeOf(bars, hover!).toFixed(2)}%</b></div>
              <div className="tt-ma">
                <span style={{ color: MA5_COLOR }}>MA5 {ma5[hover!] ? ma5[hover!]!.toFixed(2) : '—'}</span>
                <span style={{ color: MA10_COLOR }}>MA10 {ma10[hover!] ? ma10[hover!]!.toFixed(2) : '—'}</span>
              </div>
            </div>
          )}
          <div className="candle-legend">
            <span><i style={{ background: UP }} />阳线</span>
            <span><i style={{ background: DOWN }} />阴线</span>
            <span><i style={{ background: MA5_COLOR }} />MA5</span>
            <span><i style={{ background: MA10_COLOR }} />MA10</span>
            <em>日涨跌 {dayChange >= 0 ? '+' : ''}{dayChange.toFixed(2)}%</em>
          </div>
        </>
      )}
    </div>
  )
}

function dayChangeOf(bars: Candle[], i: number): number {
  if (i <= 0) return 0
  return ((bars[i].close - bars[i - 1].close) / bars[i - 1].close) * 100
}
