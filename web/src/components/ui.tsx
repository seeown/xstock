import type { ReactNode } from 'react'

export const fmt = (n: number) =>
  Number(n).toLocaleString('zh-CN', { maximumFractionDigits: 2 })

export const pct = (n: number) => `${Number(n).toFixed(2)}%`

export const money = (n: number) =>
  `¥${Number(n).toLocaleString('zh-CN', { maximumFractionDigits: 0 })}`

export function Card({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <section className={`card ${className}`}>{children}</section>
}

export function CardHead({ title, sub, right }: { title: string; sub?: string; right?: ReactNode }) {
  return (
    <div className="card-head">
      <div>
        <h2>{title}</h2>
        {sub && <p>{sub}</p>}
      </div>
      {right}
    </div>
  )
}

export function MetricCard({
  label,
  value,
  tone = 'neutral',
  hint,
}: {
  label: string
  value: string
  tone?: 'pos' | 'neg' | 'neutral'
  hint?: string
}) {
  return (
    <div className={`metric ${tone}`}>
      <span className="metric-label">{label}</span>
      <strong className="metric-value">{value}</strong>
      {hint && <span className="metric-hint">{hint}</span>}
    </div>
  )
}

export function Banner({ text, kind }: { text: string; kind: 'info' | 'error' | 'success' }) {
  if (!text) return null
  return <div className={`banner ${kind}`}>{text}</div>
}

export function Spinner({ text = '加载中…' }: { text?: string }) {
  return (
    <div className="spinner-wrap">
      <span className="spinner" />
      <span>{text}</span>
    </div>
  )
}

export function Empty({ text }: { text: string }) {
  return <div className="empty">{text}</div>
}

// Sparkline 迷你走势线（纯 SVG polyline），情绪趋势与板块等权指数用。
export function Sparkline({
  values, width = 110, height = 34, color = '#9ec1ff',
}: {
  values: Array<number | null>
  width?: number
  height?: number
  color?: string
}) {
  const pts = values.filter((v): v is number => v != null && Number.isFinite(v))
  if (pts.length < 2) return <svg width={width} height={height} className="spark" />
  const min = Math.min(...pts)
  const max = Math.max(...pts)
  const range = max - min || 1
  const path = pts
    .map((v, i) => `${(i / (pts.length - 1)) * (width - 4) + 2},${height - 3 - ((v - min) / range) * (height - 6)}`)
    .join(' ')
  return (
    <svg width={width} height={height} className="spark" aria-hidden>
      <polyline points={path} fill="none" stroke={color} strokeWidth="1.5" strokeLinejoin="round" />
      <circle cx={width - 2} cy={height - 3 - ((pts[pts.length - 1] - min) / range) * (height - 6)} r="1.8" fill={color} />
    </svg>
  )
}

export const exitReasonText: Record<string, string> = {
  stop_loss: '止损',
  take_profit: '止盈',
  max_hold: '到期平仓',
}
