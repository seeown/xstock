import { useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import type { IndexMinute } from '../api'

// 大盘分时图：241 档时段坐标（09:30–11:30 / 13:01–15:00，午休折叠）。
// 白线=最新价、黄线=当日均价（TDX 服务端累计口径），昨收虚线居中做基准，
// 上下对称比例尺（涨跌百分比）；下方分钟量条；可叠加分钟均线 MA5/10/20/60
// （对分钟收盘价计算）。悬停出十字线与读数。

const SLOTS = 241
const LUNCH = 120 // 11:30 的档位下标；121 起为下午 13:01+

const X_LABELS: Array<{ slot: number; text: string }> = [
  { slot: 0, text: '09:30' },
  { slot: 60, text: '10:30' },
  { slot: LUNCH, text: '11:30/13:00' },
  { slot: 180, text: '14:00' },
  { slot: 240, text: '15:00' },
]

const MA_DEFS = [
  { n: 5, color: '#2DD4BF' },
  { n: 10, color: '#A5B4FC' },
  { n: 20, color: '#38BDF8' },
  { n: 60, color: '#FFC46B' },
] as const

const UP = '#FF6E66'
const DOWN = '#3DDC97'
const AVG = '#F5B544'

const GRID = '#1E2C47'
const TEXT = '#8EA2BD'

function sma(values: number[], period: number): Array<number | null> {
  const out: Array<number | null> = []
  let sum = 0
  for (let i = 0; i < values.length; i++) {
    sum += values[i]
    if (i >= period) sum -= values[i - period]
    out.push(i >= period - 1 ? sum / period : null)
  }
  return out
}

function polyline(vals: Array<number | null>, x: (i: number) => number, y: (v: number) => number): string {
  let d = ''
  let pen = false
  for (let i = 0; i < vals.length; i++) {
    const v = vals[i]
    if (v == null) {
      pen = false
      continue
    }
    d += `${pen ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)} `
    pen = true
  }
  return d.trim()
}

const fmt2 = (v: number) => v.toFixed(2)
const fmtPct = (v: number) => `${v >= 0 ? '+' : ''}${v.toFixed(2)}%`

export default function IntradayChart({ data, height = 420 }: { data: IndexMinute; height?: number }) {
  const hostRef = useRef<HTMLDivElement>(null)
  const [w, setW] = useState(920)
  const [hover, setHover] = useState<number | null>(null)
  const [maOn, setMaOn] = useState<Record<number, boolean>>({})

  useEffect(() => {
    const el = hostRef.current
    if (!el) return
    const ro = new ResizeObserver(entries => {
      for (const e of entries) setW(Math.max(340, Math.round(e.contentRect.width)))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const { points, preClose } = data
  const n = points.length

  // ---- 几何 ----
  const PAD_L = 10
  const PAD_R = 58
  const PAD_T = 12
  const labelRow = 20
  const priceH = Math.round((height - labelRow - PAD_T) * 0.72)
  const volTop = PAD_T + priceH + 6
  const volH = height - labelRow - PAD_T - priceH - 6
  const volBase = volTop + volH
  const plotW = w - PAD_L - PAD_R
  const x = (i: number) => PAD_L + (i / (SLOTS - 1)) * plotW

  // ---- 价格比例尺：围绕昨收对称 ----
  const dev = useMemo(() => {
    let d = preClose * 0.002
    for (const p of points) {
      d = Math.max(d, Math.abs(p.price - preClose), Math.abs(p.avg - preClose))
    }
    return d * 1.08
  }, [points, preClose])
  const y = (v: number) => PAD_T + ((preClose + dev - v) / (2 * dev)) * priceH
  const pctOf = (v: number) => ((v - preClose) / preClose) * 100

  const last = points[n - 1]
  const lastPct = pctOf(last.price)
  const up = last.price >= preClose
  const lineColor = up ? UP : DOWN

  const pricePath = points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.price).toFixed(1)}`).join(' ')
  const avgPath = points.map((p, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(p.avg).toFixed(1)}`).join(' ')
  const areaPath = `${pricePath} L${x(n - 1).toFixed(1)},${y(preClose).toFixed(1)} L${x(0).toFixed(1)},${y(preClose).toFixed(1)} Z`

  const maPaths = useMemo(
    () => MA_DEFS
      .filter(m => maOn[m.n])
      .map(m => ({ ...m, d: polyline(sma(points.map(p => p.price), m.n), x, y) })),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [points, maOn, dev, preClose, w],
  )

  const maxVol = useMemo(() => Math.max(1, ...points.map(p => p.vol)), [points])
  const barW = Math.max(1, (plotW / SLOTS) * 0.7)

  const gridPcts = [1, 0.5, 0, -0.5, -1] // 比例尺档位（×dev）

  const h = hover != null ? points[hover] : null
  const onMove = (e: ReactMouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    const i = Math.round(((e.clientX - rect.left - PAD_L) / plotW) * (SLOTS - 1))
    setHover(i >= 0 && i < n ? i : null)
  }

  const gradId = 'intraday-grad'

  return (
    <div ref={hostRef} className="intraday-wrap" style={{ height }}>
      <div className="intraday-legend">
        <span>
          {data.date} · 最新 <b style={{ color: lineColor }}>{fmt2(last.price)}</b>{' '}
          <span className={lastPct >= 0 ? 'up-text' : 'down-text'}>{fmtPct(lastPct)}</span>
        </span>
        <span className="muted-c">均价 <b style={{ color: AVG }}>{fmt2(last.avg)}</b></span>
        <span className="muted-c">昨收 {fmt2(preClose)}</span>
        <span className="legend-sep" />
        {MA_DEFS.map(m => (
          <button
            key={m.n}
            className={`intraday-ma${maOn[m.n] ? ' on' : ''}`}
            style={maOn[m.n] ? { color: m.color, borderColor: m.color } : undefined}
            onClick={() => setMaOn(prev => ({ ...prev, [m.n]: !prev[m.n] }))}
          >
            MA{m.n}
          </button>
        ))}
      </div>
      <svg
        width={w}
        height={height - 30}
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
        style={{ display: 'block' }}
      >
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={lineColor} stopOpacity="0.22" />
            <stop offset="100%" stopColor={lineColor} stopOpacity="0.02" />
          </linearGradient>
        </defs>

        {/* 网格 + 右侧涨跌幅刻度（昨收居中对称） */}
        {gridPcts.map(k => {
          const v = preClose + k * dev
          const yy = y(v)
          return (
            <g key={k}>
              <line x1={PAD_L} y1={yy} x2={w - PAD_R} y2={yy} stroke={GRID} strokeWidth="1" strokeDasharray={k === 0 ? '3 4' : undefined} />
              <text x={w - PAD_R + 8} y={yy + 3.5} fontSize="10.5" fill={k > 0 ? UP : k < 0 ? DOWN : TEXT}>
                {fmtPct(pctOf(v))}
              </text>
            </g>
          )
        })}
        {X_LABELS.map(l => (
          <g key={l.slot}>
            <line x1={x(l.slot)} y1={PAD_T} x2={x(l.slot)} y2={volBase} stroke={l.slot === LUNCH ? 'rgba(255,255,255,.10)' : GRID} strokeWidth="1" />
            <text x={x(l.slot)} y={volBase + 14} fontSize="10.5" fill={TEXT} textAnchor={l.slot === 0 ? 'start' : l.slot === 240 ? 'end' : 'middle'}>
              {l.text}
            </text>
          </g>
        ))}

        {/* 价格面积 + 均价线 + 可选分钟MA + 价格线 */}
        <path d={areaPath} fill={`url(#${gradId})`} />
        {maPaths.map(m => (
          <path key={m.n} d={m.d} fill="none" stroke={m.color} strokeWidth="1" opacity="0.85" />
        ))}
        <path d={avgPath} fill="none" stroke={AVG} strokeWidth="1.4" />
        <path d={pricePath} fill="none" stroke={lineColor} strokeWidth="1.6" />

        {/* 最新价右侧标签 */}
        <g>
          <rect x={w - PAD_R + 2} y={y(last.price) - 8} width={PAD_R - 4} height={16} rx={4} fill={lineColor} />
          <text x={w - PAD_R + 6} y={y(last.price) + 3.5} fontSize="10.5" fill="#0B1526" fontWeight="700">{fmt2(last.price)}</text>
        </g>

        {/* 分钟成交量：与前一分钟比价着色 */}
        {points.map((p, i) => {
          const prev = i > 0 ? points[i - 1].price : preClose
          const c = p.price >= prev ? UP : DOWN
          const bh = Math.max(0.5, ((p.vol / maxVol) * volH))
          return <rect key={i} x={x(i) - barW / 2} y={volBase - bh} width={barW} height={bh} fill={c} opacity="0.55" />
        })}
        <line x1={PAD_L} y1={volBase} x2={w - PAD_R} y2={volBase} stroke={GRID} strokeWidth="1" />

        {/* 悬停十字线 + 读数点 */}
        {h && hover != null && (
          <g pointerEvents="none">
            <line x1={x(hover)} y1={PAD_T} x2={x(hover)} y2={volBase} stroke="rgba(255,255,255,.35)" strokeWidth="1" />
            <circle cx={x(hover)} cy={y(h.price)} r="3" fill={lineColor} stroke="#0B1526" strokeWidth="1" />
            <circle cx={x(hover)} cy={y(h.avg)} r="2.5" fill={AVG} stroke="#0B1526" strokeWidth="1" />
          </g>
        )}
      </svg>
      {h && hover != null && (
        <div className="intraday-tip">
          <span>{h.time}</span>
          <span>价 <b style={{ color: lineColor }}>{fmt2(h.price)}</b></span>
          <span className={pctOf(h.price) >= 0 ? 'up-text' : 'down-text'}>{fmtPct(pctOf(h.price))}</span>
          <span>均 <b style={{ color: AVG }}>{fmt2(h.avg)}</b></span>
          <span className="muted-c">量 {h.vol}</span>
        </div>
      )}
    </div>
  )
}
