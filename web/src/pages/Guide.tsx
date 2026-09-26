import { useEffect, useMemo, useState } from 'react'
import { api, type GuideResult } from '../api'
import { Button, Card, CardHead, ErrorBlock, Skeleton, pct } from '../components/ui'

const stageTone: Record<string, string> = {
  冰点: 'neg', 低迷: 'neg', 中性: '', 活跃: 'pos', 亢奋: 'pos',
}

// 五档色带（与设计稿 07 一致）：冰点青绿 → 亢奋红
const BANDS = [
  { max: 20, name: '冰点', color: '#3DDC97' },
  { max: 40, name: '低迷', color: '#2DD4BF' },
  { max: 60, name: '中性', color: '#38BDF8' },
  { max: 80, name: '活跃', color: '#FFC46B' },
  { max: 100, name: '亢奋', color: '#FF6E66' },
]
const bandOf = (score: number) => BANDS.find(b => score < b.max) ?? BANDS[BANDS.length - 1]

// 情绪温度计：半圆五档色带 + 白色指针 + 大字读数（设计稿 07 SVG 契约）
function Gauge({ score, stage }: { score: number; stage: string }) {
  const band = bandOf(score)
  const clamped = Math.max(0, Math.min(100, score))
  const cx = 200, cy = 190, r = 150
  const seg = 36 // 每档 36°
  const arc = (startDeg: number, endDeg: number, color: string, opacity = 1) => {
    const rad = (d: number) => ((d - 90) * Math.PI) / 180
    const x1 = cx + r * Math.cos(rad(startDeg)), y1 = cy + r * Math.sin(rad(startDeg))
    const x2 = cx + r * Math.cos(rad(endDeg)), y2 = cy + r * Math.sin(rad(endDeg))
    return <path d={`M${x1.toFixed(1)},${y1.toFixed(1)} A${r},${r} 0 0 1 ${x2.toFixed(1)},${y2.toFixed(1)}`} stroke={color} strokeWidth="16" fill="none" strokeLinecap="butt" opacity={opacity} />
  }
  const angle = -90 + (clamped / 100) * 180
  const needleRad = (angle * Math.PI) / 180
  const nx = cx + (r - 26) * Math.cos(needleRad), ny = cy + (r - 26) * Math.sin(needleRad)
  return (
    <svg viewBox="0 0 400 215" className="gauge" role="img" aria-label={`大盘情绪温度 ${clamped.toFixed(0)} 分，${stage}`}>
      {BANDS.map((b, i) => arc(-90 + i * seg, -90 + (i + 1) * seg, b.color, band === b ? 1 : 0.32))}
      {[20, 40, 60, 80].map(v => {
        const rad = ((-90 + (v / 100) * 180) * Math.PI) / 180
        return (
          <line
            key={v} x1={cx + (r - 10) * Math.cos(rad)} y1={cy + (r - 10) * Math.sin(rad)}
            x2={cx + (r + 10) * Math.cos(rad)} y2={cy + (r + 10) * Math.sin(rad)}
            stroke="rgba(255,255,255,.35)" strokeWidth="1.4"
          />
        )
      })}
      <line x1={cx} y1={cy} x2={nx} y2={ny} stroke="#F1F7FD" strokeWidth="5" strokeLinecap="round" />
      <circle cx={cx} cy={cy} r="7" fill="#F1F7FD" />
      <circle cx={cx} cy={cy} r="3" fill={band.color} />
      <text x={cx} y={cy - 52} textAnchor="middle" fontSize="60" fontWeight="800" fill="#F1F7FD" style={{ fontVariantNumeric: 'tabular-nums' }}>
        {clamped.toFixed(0)}
      </text>
      <text x={cx} y={cy - 22} textAnchor="middle" fontSize="15" fontWeight="700" fill={band.color}>{stage}</text>
      <text x={cx - r} y={cy + 22} textAnchor="middle" fontSize="11" fill="#7E96B5">0</text>
      <text x={cx + r} y={cy + 22} textAnchor="middle" fontSize="11" fill="#7E96B5">100</text>
    </svg>
  )
}

// 双轴折线图：左轴家数、右轴成交额（亿）。纯 SVG。
// bands：左轴横向五档色带背景（温度走势图用，7% 透明度）。
function LineChart({
  labels, series, height = 220, bands,
}: {
  labels: string[]
  series: Array<{ name: string; color: string; data: number[]; axis: 'left' | 'right' }>
  height?: number
  bands?: Array<{ from: number; to: number; color: string }>
}) {
  const W = 960, H = height
  const M = { top: 16, right: 64, bottom: 26, left: 52 }
  const iw = W - M.left - M.right, ih = H - M.top - M.bottom
  const n = labels.length
  const scale = (axis: 'left' | 'right') => {
    const vals = series.filter(s => s.axis === axis).flatMap(s => s.data).filter(v => Number.isFinite(v))
    if (!vals.length) return { min: 0, max: 1 }
    let min = Math.min(...vals), max = Math.max(...vals)
    if (max - min < 1e-9) { min -= 1; max += 1 }
    const pad = (max - min) * 0.08
    return { min: Math.max(0, min - pad), max: max + pad }
  }
  const [ls, rs] = [scale('left'), scale('right')]
  const x = (i: number) => M.left + (n <= 1 ? iw / 2 : (i / (n - 1)) * iw)
  const yL = (v: number) => M.top + ih - ((v - ls.min) / (ls.max - ls.min)) * ih
  const yR = (v: number) => M.top + ih - ((v - rs.min) / (rs.max - rs.min)) * ih
  const fmtNum = (v: number) => v >= 10000 ? `${(v / 10000).toFixed(1)}万` : v >= 100 ? v.toFixed(0) : v.toFixed(1)
  const last = series.map(s => ({ ...s, last: s.data[s.data.length - 1] }))
  return (
    <div className="linechart-wrap">
      <div className="linechart-legend">
        {last.map(s => (
          <span key={s.name} className="lc-item">
            <i style={{ background: s.color }} />
            {s.name}
            <b style={{ color: s.color }}>{fmtNum(s.last ?? 0)}</b>
          </span>
        ))}
      </div>
      <svg viewBox={`0 0 ${W} ${H}`} className="linechart" role="img">
        {bands?.map((b, i) => (
          <rect
            key={i} x={M.left} width={iw}
            y={yL(Math.min(b.to, ls.max))} height={Math.max(0, yL(Math.max(b.from, ls.min)) - yL(Math.min(b.to, ls.max)))}
            fill={b.color} opacity="0.07"
          />
        ))}
        {[0, 0.25, 0.5, 0.75, 1].map(f => (
          <g key={f}>
            <line x1={M.left} x2={W - M.right} y1={M.top + ih * f} y2={M.top + ih * f} stroke="rgba(255,255,255,.06)" />
            <text x={M.left - 8} y={M.top + ih * f + 4} textAnchor="end" fontSize="11" fill="#7E96B5">{fmtNum(ls.max - (ls.max - ls.min) * f)}</text>
            <text x={W - M.right + 8} y={M.top + ih * f + 4} fontSize="11" fill="#7E96B5">{fmtNum(rs.max - (rs.max - rs.min) * f)}</text>
          </g>
        ))}
        {labels.map((d, i) => (i % Math.ceil(n / 8) === 0 || i === n - 1) && (
          <text key={d} x={x(i)} y={H - 8} textAnchor="middle" fontSize="11" fill="#7E96B5">{d.slice(5)}</text>
        ))}
        {series.map(s => {
          const y = s.axis === 'left' ? yL : yR
          const path = s.data.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(Number.isFinite(v) ? v : s.axis === 'left' ? ls.min : rs.min).toFixed(1)}`).join(' ')
          return <path key={s.name} d={path} fill="none" stroke={s.color} strokeWidth="1.8" strokeLinejoin="round" />
        })}
        {series.map(s => {
          const y = s.axis === 'left' ? yL : yR
          const i = n - 1, v = s.data[i]
          return Number.isFinite(v) ? <circle key={s.name} cx={x(i)} cy={y(v)} r="2.6" fill={s.color} /> : null
        })}
      </svg>
    </div>
  )
}

export default function Guide() {
  const [data, setData] = useState<GuideResult | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    let cancelled = false
    const load = () => api.marketGuide()
      .then(r => { if (!cancelled) { setData(r); setErr('') } })
      .catch(e => { if (!cancelled) setErr(e instanceof Error ? e.message : '加载失败') })
    load()
    const t = setInterval(load, 60_000)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

  const rt = data?.realtime
  const hist = useMemo(() => data?.history ?? [], [data])
  const upDown = rt ? `${rt.upCount}:${rt.downCount}` : '—'

  if (err) return (
    <div className="page"><ErrorBlock title="情绪数据加载失败" desc={err} onRetry={() => location.reload()} /></div>
  )
  if (!rt) return (
    <div className="page">
      <div className="kpis">{Array.from({ length: 4 }).map((_, i) => <div key={i} className="kpi" style={{ minHeight: 86 }}><Skeleton h={40} w="70%" /></div>)}</div>
      <Card><Skeleton h={180} count={2} /></Card>
    </div>
  )

  const band = bandOf(rt.tempScore)

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>情绪指南</h1>
          <p>量能 × 涨跌停的大盘情绪温度 · 数据截至 {rt.asOf} · 快照 {rt.updatedAt}{rt.final ? ' · 收盘定格' : ' · 盘中'}</p>
        </div>
      </header>

      {/* 温度计 + 结论：设计稿 07 头部 */}
      <Card className="gauge-card">
        <div className="split">
          <div className="gauge-wrap">
            <Gauge score={rt.tempScore} stage={rt.stage} />
          </div>
          <div className="grow stack">
            <div>
              <div className="gauge-stage" style={{ color: band.color }}>{rt.quadrant} · {rt.stage}</div>
              <p className="muted-c" style={{ margin: '6px 0 0', fontSize: 12.5 }}>
                温度 {rt.tempScore.toFixed(0)} / 100 · 五档：冰点&lt;20 · 低迷&lt;40 · 中性&lt;60 · 活跃&lt;80 · 亢奋≥80
              </p>
            </div>
            <div className="kv"><span className="k">涨停 / 跌停</span><span className="up-text">{rt.limitUp}</span> / <span className="down-text">{rt.limitDown}</span></div>
            <div className="kv"><span className="k">炸板率</span><span>{pct(rt.breakRate)}</span></div>
            <div className="kv"><span className="k">晋级率</span><span>{pct(rt.promoteRate)}</span></div>
            <div className="kv"><span className="k">两市成交额</span><span className="mono">{rt.amountToday.toFixed(0)} 亿{rt.final ? '' : `（昨日全天 ${rt.amountYesterday.toFixed(0)} 亿）`}</span></div>
            <div className="kv"><span className="k">量能比</span><span className="mono">{rt.amountRatio.toFixed(2)}×{rt.final ? ' 对前5日均' : ' 对昨日(盘中部分)'}</span></div>
          </div>
          <div className="grow stack">
            <div className="kv"><span className="k">涨跌家数</span><span>{upDown}{rt.upCount + rt.downCount > 0 ? ` · 比 ${(rt.upCount / Math.max(1, rt.downCount)).toFixed(2)}` : ''}</span></div>
            <div className="kv"><span className="k">最高板</span><span className="mono">{rt.maxBoards || '—'} 板</span></div>
            <div className="kv"><span className="k">快照时间</span><span className="mono">{rt.updatedAt}</span></div>
            <div className="kv"><span className="k">口径</span><span>{rt.final ? '收盘定格' : '盘中实时'}</span></div>
            <Button variant="ghost" onClick={() => location.reload()} style={{ marginTop: 4 }}>立即刷新</Button>
          </div>
        </div>
      </Card>

      <div className="guide-tiles">
        {([
            ['涨停家数', String(rt.limitUp), `炸板 ${rt.broke}`],
            ['跌停家数', String(rt.limitDown), ''],
            ['炸板率', pct(rt.breakRate), ''],
            ['最高板', `${rt.maxBoards || '—'} 板`, ''],
            ['晋级率', pct(rt.promoteRate), ''],
            ['涨跌家数', upDown, rt.upCount + rt.downCount > 0 ? `比 ${(rt.upCount / Math.max(1, rt.downCount)).toFixed(2)}` : ''],
            ['两市成交额', `${rt.amountToday.toFixed(0)} 亿`, rt.final ? '' : `昨日全天 ${rt.amountYesterday.toFixed(0)} 亿`],
            ['量能比', `${rt.amountRatio.toFixed(2)}×`, rt.final ? '对前5日均' : '对昨日(盘中部分)'],
          ] as Array<[string, string, string]>).map(([label, value, hint]) => (
            <div key={label} className="gt-tile">
              <span>{label}</span>
              <b>{value}</b>
              {hint && <em>{hint}</em>}
            </div>
          ))}
      </div>

      <Card>
        <CardHead
          title="涨停 · 炸板 · 量能（近30个交易日）"
          sub="左轴：涨停/炸板家数 · 右轴：全市场成交额（亿，成交量×收盘价估算）"
        />
        <LineChart
          labels={hist.map(d => d.date)}
          series={[
            { name: '涨停', color: '#FF6E66', axis: 'left', data: hist.map(d => d.limitUp) },
            { name: '炸板', color: '#FFC46B', axis: 'left', data: hist.map(d => d.broke) },
            { name: '跌停', color: '#3DDC97', axis: 'left', data: hist.map(d => d.limitDown) },
            { name: '成交额(亿)', color: '#38BDF8', axis: 'right', data: hist.map(d => d.amount) },
          ]}
        />
      </Card>

      <Card>
        <CardHead title="情绪温度分（近30个交易日）" sub="涨停30% + 炸板率20% + 晋级率20% + 量能15% + 高度15 · 背景为五档色带" />
        <LineChart
          height={180}
          labels={hist.map(d => d.date)}
          bands={BANDS.map((b, i) => ({ from: BANDS[i - 1]?.max ?? 0, to: b.max, color: b.color }))}
          series={[
            { name: '温度分', color: '#7DD3FC', axis: 'left', data: hist.map(d => d.tempScore) },
            { name: '涨跌家数比', color: '#5EEAD4', axis: 'right', data: hist.map(d => d.upCount / Math.max(1, d.downCount)) },
          ]}
        />
        <div className="table-wrap">
          <table className="tb guide-table">
            <thead>
              <tr>
                <th>日期</th><th className="num">涨停</th><th className="num">跌停</th><th className="num">炸板率</th>
                <th className="num">成交额(亿)</th><th className="num">量能比</th><th className="num">涨/跌</th><th className="num">温度分</th><th>阶段</th>
              </tr>
            </thead>
            <tbody>
              {[...hist].reverse().map(d => (
                <tr key={d.date}>
                  <td>{d.date}</td>
                  <td className="num up-text">{d.limitUp}</td>
                  <td className="num down-text">{d.limitDown}</td>
                  <td className="num">{pct(d.breakRate)}</td>
                  <td className="num">{d.amount.toFixed(0)}</td>
                  <td className={`num ${d.amountRatio >= 1.1 ? 'up-text' : d.amountRatio <= 0.85 ? 'down-text' : ''}`}>{d.amountRatio.toFixed(2)}×</td>
                  <td className="num muted-c">{d.upCount}/{d.downCount}</td>
                  <td className="num">{d.tempScore.toFixed(0)}</td>
                  <td><span className={`stage-chip ${stageTone[d.stage] ?? ''}`}>{d.stage}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </Card>
    </div>
  )
}
