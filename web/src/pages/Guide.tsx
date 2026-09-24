import { useEffect, useMemo, useState } from 'react'
import { api, type GuideResult } from '../api'
import { Card, CardHead, Spinner, pct } from '../components/ui'

const stageTone: Record<string, string> = {
  冰点: 'neg', 低迷: 'neg', 中性: '', 活跃: 'pos', 亢奋: 'pos',
}

// 双轴折线图：左轴家数、右轴成交额（亿）。纯 SVG。
function LineChart({
  labels, series, height = 320,
}: {
  labels: string[]
  series: Array<{ name: string; color: string; data: number[]; axis: 'left' | 'right' }>
  height?: number
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
        {[0, 0.25, 0.5, 0.75, 1].map(f => (
          <g key={f}>
            <line x1={M.left} x2={W - M.right} y1={M.top + ih * f} y2={M.top + ih * f} stroke="rgba(126,152,191,0.14)" />
            <text x={M.left - 8} y={M.top + ih * f + 4} textAnchor="end" fontSize="11" fill="#64748b">{fmtNum(ls.max - (ls.max - ls.min) * f)}</text>
            <text x={W - M.right + 8} y={M.top + ih * f + 4} fontSize="11" fill="#64748b">{fmtNum(rs.max - (rs.max - rs.min) * f)}</text>
          </g>
        ))}
        {labels.map((d, i) => (i % Math.ceil(n / 8) === 0 || i === n - 1) && (
          <text key={d} x={x(i)} y={H - 8} textAnchor="middle" fontSize="11" fill="#64748b">{d.slice(5)}</text>
        ))}
        {series.map(s => {
          const y = s.axis === 'left' ? yL : yR
          const path = s.data.map((v, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(Number.isFinite(v) ? v : s.axis === 'left' ? ls.min : rs.min).toFixed(1)}`).join(' ')
          return <polyline key={s.name} points={path.replace(/M|L/g, '').split(' ').filter(Boolean).map((p, i) => `${i === 0 ? '' : ''}${p}`).join(' ')} fill="none" stroke={s.color} strokeWidth="1.8" strokeLinejoin="round" />
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
    <div className="page"><Card><CardHead title="情绪指南" sub={err} /></Card></div>
  )
  if (!rt) return <div className="page"><Spinner text="正在计算市场情绪（首算需要几秒）…" /></div>

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>情绪指南</h1>
          <p>量能 × 涨跌停的大盘情绪温度 · 数据截至 {rt.asOf} · 快照 {rt.updatedAt}{rt.final ? ' · 收盘定格' : ' · 盘中'}</p>
        </div>
      </header>

      <div className="guide-verdict">
        <div className={`gv-score ${stageTone[rt.stage] ?? ''}`}>
          <span className="gv-num">{rt.tempScore.toFixed(0)}</span>
          <span className="gv-meta">
            <span className="gv-stage">{rt.stage}</span>
            <span className="gv-quadrant">{rt.quadrant}</span>
          </span>
        </div>
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
      </div>

      <Card>
        <CardHead
          title="涨停 · 炸板 · 量能（近30个交易日）"
          sub="左轴：涨停/炸板家数 · 右轴：全市场成交额（亿，成交量×收盘价估算）"
        />
        <LineChart
          labels={hist.map(d => d.date)}
          series={[
            { name: '涨停', color: '#f4577a', axis: 'left', data: hist.map(d => d.limitUp) },
            { name: '炸板', color: '#f5b544', axis: 'left', data: hist.map(d => d.broke) },
            { name: '跌停', color: '#22c58b', axis: 'left', data: hist.map(d => d.limitDown) },
            { name: '成交额(亿)', color: '#4f8cff', axis: 'right', data: hist.map(d => d.amount) },
          ]}
        />
      </Card>

      <Card>
        <CardHead title="情绪温度分（近30个交易日）" sub="涨停30% + 炸板率20% + 晋级率20% + 量能15% + 高度15 · 冰点<20 低迷<40 中性<60 活跃<80 亢奋≥80" />
        <LineChart
          height={240}
          labels={hist.map(d => d.date)}
          series={[
            { name: '温度分', color: '#9ec1ff', axis: 'left', data: hist.map(d => d.tempScore) },
            { name: '涨跌家数比', color: '#7ce3bb', axis: 'right', data: hist.map(d => d.upCount / Math.max(1, d.downCount)) },
          ]}
        />
        <div className="table-wrap">
          <table className="guide-table">
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
                  <td className="num pos">{d.limitUp}</td>
                  <td className="num neg">{d.limitDown}</td>
                  <td className="num">{pct(d.breakRate)}</td>
                  <td className="num">{d.amount.toFixed(0)}</td>
                  <td className={`num ${d.amountRatio >= 1.1 ? 'pos' : d.amountRatio <= 0.85 ? 'neg' : ''}`}>{d.amountRatio.toFixed(2)}×</td>
                  <td className="num muted">{d.upCount}/{d.downCount}</td>
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
