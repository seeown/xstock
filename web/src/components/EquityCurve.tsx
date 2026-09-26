import type { Trade } from '../api'

// 资金曲线：策略权益阶梯线 + 蓝青渐变面积 + 初始资金基准虚线 + 运行峰值细线，
// 回撤区间(权益低于峰值)以淡红填充。数据由 trades 前端合成(权益只在卖出日变化)。
export default function EquityCurve({ initialCash, trades, height = 220 }: { initialCash: number; trades: Trade[]; height?: number }) {
  const sold = [...trades].filter(t => t.sellDate).sort((a, b) => a.sellDate.localeCompare(b.sellDate))
  if (sold.length < 1) return null

  // 权益阶梯:起点=窗口前(初始资金),每笔卖出日跳变
  let cash = initialCash
  const pts: Array<{ date: string; v: number }> = [{ date: sold[0].buyDate, v: cash }]
  for (const t of sold) {
    cash *= 1 + t.returnPct / 100
    pts.push({ date: t.sellDate, v: cash })
  }
  const values = pts.map(p => p.v)
  const min = Math.min(...values, initialCash) * 0.995
  const max = Math.max(...values, initialCash) * 1.005
  const range = max - min || 1

  const W = 1000
  const H = height
  const PAD_L = 8
  const PAD_B = 18
  const x = (i: number) => PAD_L + (i / (pts.length - 1 || 1)) * (W - PAD_L * 2)
  const y = (v: number) => H - PAD_B - ((v - min) / range) * (H - PAD_B - 10)

  // 阶梯路径(卖出日跳变):横到下一跳变日前一天,再垂直跳
  const stepPath = pts.map((p, i) => {
    const seg = i === 0 ? `M${x(0)},${y(p.v)}` : `L${x(i)},${y(pts[i - 1].v)} L${x(i)},${y(p.v)}`
    return seg
  }).join(' ')

  const areaPath = `${stepPath} L${x(pts.length - 1)},${H - PAD_B} L${x(0)},${H - PAD_B} Z`

  // 峰值线 + 回撤填充区:峰值只增不减
  let peak = initialCash
  const peakPts = pts.map(p => { peak = Math.max(peak, p.v); return peak })
  const peakPath = peakPts.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i)},${y(v)}`).join(' ')
  const drawdownParts: Array<string> = []
  let ddStart = -1
  for (let i = 1; i < pts.length; i++) {
    const below = pts[i].v < peakPts[i]
    const wasBelow = pts[i - 1].v < peakPts[i - 1]
    if (below && !wasBelow) ddStart = i - 1
    if (!below && wasBelow && ddStart >= 0) {
      const x0 = x(ddStart), x1 = x(i)
      const yTop = Math.min(y(peakPts[ddStart]), y(peakPts[i]))
      const yLow = Math.max(...Array.from({ length: i - ddStart + 1 }, (_, k) => y(pts[ddStart + k].v)))
      drawdownParts.push(`M${x0},${yTop} L${x1},${yTop} L${x1},${yLow} L${x0},${yLow} Z`)
      ddStart = -1
    }
  }
  if (ddStart >= 0) {
    const x0 = x(ddStart), x1 = x(pts.length - 1)
    const yTop = y(peakPts[ddStart])
    const yLow = Math.max(...Array.from({ length: pts.length - ddStart }, (_, k) => y(pts[ddStart + k].v)))
    drawdownParts.push(`M${x0},${yTop} L${x1},${yTop} L${x1},${yLow} L${x0},${yLow} Z`)
  }

  const yTicks = [min + range * 0.25, min + range * 0.5, min + range * 0.75, max]
  const ret = (cash / initialCash - 1) * 100

  return (
    <div className="equity-wrap" role="img" aria-label={`资金曲线，期末收益 ${ret.toFixed(1)}%`}>
      <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ width: '100%', height }}>
        <defs>
          <linearGradient id="eq-grad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="rgba(56,189,248,.30)" />
            <stop offset="100%" stopColor="rgba(45,212,191,.02)" />
          </linearGradient>
        </defs>
        {yTicks.map((v, i) => (
          <line key={i} x1={PAD_L} x2={W - PAD_L} y1={y(v)} y2={y(v)} stroke="rgba(255,255,255,.06)" strokeWidth="1" />
        ))}
        {drawdownParts.map((d, i) => <path key={i} d={d} fill="rgba(255,110,102,.09)" />)}
        <line x1={PAD_L} x2={W - PAD_L} y1={y(initialCash)} y2={y(initialCash)} stroke="rgba(255,255,255,.22)" strokeWidth="1.2" strokeDasharray="2 5" strokeLinecap="round" />
        <path d={areaPath} fill="url(#eq-grad)" />
        <path d={peakPath} fill="none" stroke="rgba(255,255,255,.14)" strokeWidth="1" strokeDasharray="3 6" />
        <path d={stepPath} fill="none" stroke="#7DD3FC" strokeWidth="2.3" strokeLinejoin="round" />
        {pts.map((p, i) => (
          <circle key={i} cx={x(i)} cy={y(p.v)} r={i === pts.length - 1 ? 4 : 2.4} fill={p.v >= initialCash ? '#7DD3FC' : '#FF8A84'}>
            <title>{`${p.date}  ${p.v.toFixed(0)}`}</title>
          </circle>
        ))}
      </svg>
      <div className="equity-axis">
        {pts.filter((_, i) => i % Math.ceil(pts.length / 8) === 0 || i === pts.length - 1).map((p, i) => (
          <span key={i} className="mono">{p.date.slice(2)}</span>
        ))}
      </div>
      <div className="legend">
        <span><i style={{ background: '#7DD3FC' }} />策略权益</span>
        <span><i style={{ background: 'rgba(255,255,255,.35)' }} />初始资金</span>
        <span><i style={{ background: 'rgba(255,255,255,.15)' }} />运行峰值</span>
        <span><i style={{ background: 'rgba(255,110,102,.30)' }} />回撤区间</span>
      </div>
    </div>
  )
}
