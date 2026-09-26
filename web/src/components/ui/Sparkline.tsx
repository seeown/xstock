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
