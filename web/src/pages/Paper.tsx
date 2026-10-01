import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type PaperOverview, type PaperPosition } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Button, Card, CardHead, KpiCard, Skeleton, fmt } from '../components/ui'
import { useIsActive } from '../shell'
// 模拟仓：纸面交易账本（初始 50 万 · T+1 · A股费用口径）。
// 持仓/曲线随 60 秒快照实时估值；卖出在持仓行内进行；
// 买入入口在个股 K 线详情弹窗（带当时的雷达信号上下文）。

const money = (v: number) => '¥' + fmt(v)
const signCls = (v: number) => (v > 0 ? 'up-text' : v < 0 ? 'down-text' : '')

function EquityCurve({ points, initial }: { points: PaperOverview['curve']; initial: number }) {
  const W = 720, H = 150, M = { l: 46, r: 12, t: 12, b: 20 }
  if (points.length < 2) {
    return <div className="chart-empty" style={{ padding: '26px 6px' }}>净值曲线从建仓次日开始积累（每日收盘记一点，请求时实时更新当日）</div>
  }
  const vals = points.map(p => p.total)
  const lo = Math.min(...vals, initial) * 0.999
  const hi = Math.max(...vals, initial) * 1.001
  const x = (i: number) => M.l + (i / (points.length - 1)) * (W - M.l - M.r)
  const y = (v: number) => M.t + (1 - (v - lo) / (hi - lo)) * (H - M.t - M.b)
  const path = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${x(i).toFixed(1)},${y(p.total).toFixed(1)}`).join(' ')
  const last = points[points.length - 1].total
  const up = last >= initial
  const grid = [0, 0.25, 0.5, 0.75, 1]
  return (
    <svg viewBox={`0 0 ${W} ${H}`} style={{ width: '100%', height: 'auto' }} role="img" aria-label="模拟仓净值曲线">
      {grid.map(f => (
        <g key={f}>
          <line x1={M.l} x2={W - M.r} y1={y(lo + (hi - lo) * f)} y2={y(lo + (hi - lo) * f)} stroke="rgba(255,255,255,.06)" />
          <text x={M.l - 6} y={y(lo + (hi - lo) * f) + 4} textAnchor="end" fontSize="10.5" fill="#7E96B5">
            {Math.round((lo + (hi - lo) * f) / 10000)}万
          </text>
        </g>
      ))}
      <line x1={M.l} x2={W - M.r} y1={y(initial)} y2={y(initial)} stroke="rgba(126,150,181,.5)" strokeDasharray="4 4" />
      <text x={W - M.r} y={y(initial) - 4} textAnchor="end" fontSize="10" fill="#7E96B5">初始 {Math.round(initial / 10000)}万</text>
      <path d={path} fill="none" stroke={up ? '#FF6E66' : '#3DDC97'} strokeWidth="1.8" strokeLinejoin="round" />
      {points.map((p, i) => (
        (i % Math.ceil(points.length / 8) === 0 || i === points.length - 1) && (
          <text key={p.date} x={x(i)} y={H - 6} textAnchor="middle" fontSize="10.5" fill="#7E96B5">{p.date.slice(5)}</text>
        )
      ))}
      <circle cx={x(points.length - 1)} cy={y(last)} r="2.6" fill={up ? '#FF6E66' : '#3DDC97'} />
    </svg>
  )
}

export default function Paper() {
  const navigate = useNavigate()
  const isActiveView = useIsActive()
  const kline = useKlinePreview()
  const [data, setData] = useState<PaperOverview | null>(null)
  const [err, setErr] = useState('')
  // 卖出交互：当前操作行 + 数量输入（默认全部可卖）
  const [selling, setSelling] = useState<PaperPosition | null>(null)
  const [sellQty, setSellQty] = useState('')
  const [busy, setBusy] = useState(false)
  const [note, setNote] = useState('')

  const load = () => {
    api.paperOverview()
      .then(d => { setData(d); setErr('') })
      .catch(e => setErr(e instanceof Error ? e.message : '账户加载失败'))
  }

  useEffect(() => {
    if (!isActiveView) return
    load()
    const t = window.setInterval(load, 60_000)
    return () => window.clearInterval(t)
  }, [isActiveView])

  const confirmSell = () => {
    if (!selling) return
    const qty = Math.max(0, Math.floor(Number(sellQty) || 0))
    setBusy(true); setNote('')
    api.paperOrder({ symbol: selling.symbol, name: selling.name, side: 'sell', qty })
      .then(res => {
        if ('filled' in res) {
          setNote(`已卖出 ${res.filled.symbol} ${res.filled.qty} 股 @ ${fmt(res.filled.price)}（费用 ¥${fmt(res.filled.fee + res.filled.tax)}）`)
        } else {
          setNote(`已挂限价卖单：${res.placed.symbol} ${res.placed.qty} 股 @ ${fmt(res.placed.limitPrice)}`)
        }
        setSelling(null)
        load()
      })
      .catch(e => setNote(e instanceof Error ? e.message : '卖出失败'))
      .finally(() => setBusy(false))
  }

  const cancelOrder = (id: number) => {
    api.paperCancelOrder(id)
      .then(() => { setNote('已撤单'); load() })
      .catch(e => setNote(e instanceof Error ? e.message : '撤单失败'))
  }

  const init = data?.account.initialCash ?? 0
  const retPct = useMemo(() => (init > 0 && data ? (data.total / init - 1) * 100 : 0), [data, init])

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>模拟仓</h1>
          <p>纸面交易 · 初始 {money(init)} · T+1 · 佣金万2.5(最低5元) + 过户费双边 + 印花税卖出 · 盘中随快照 60 秒估值</p>
        </div>
        <span className="pre-conn"><span className="dot" />{isActiveView ? '实时估值中' : '已挂起'}</span>
      </header>

      {err && <div className="banner err">{err}</div>}
      {note && <div className="banner info">{note}</div>}

      {data === null ? (
        <Card className="skel-card"><Skeleton h={88} count={4} /></Card>
      ) : (
        <>
          <div className="kpis">
            <KpiCard tone="a" label="总资产" value={money(data.total)} hint={`累计 ${data.totalPnl >= 0 ? '+' : ''}${money(data.totalPnl).slice(1)}（${retPct >= 0 ? '+' : ''}${retPct.toFixed(2)}%）`} />
            <KpiCard tone="p" label="可用现金" value={money(data.cash)} />
            <KpiCard tone="o" label="持仓市值" value={money(data.marketValue)} hint={`${data.positions.length} 只持仓`} />
            <KpiCard tone="g" label="今日盈亏" value={`${data.dayPnl >= 0 ? '+' : ''}${money(Math.abs(data.dayPnl))}`} valueClass={signCls(data.dayPnl)} />
            <KpiCard tone="g" label="累计盈亏" value={`${data.totalPnl >= 0 ? '+' : ''}${money(Math.abs(data.totalPnl))}`} valueClass={signCls(data.totalPnl)} />
          </div>

          <Card>
            <CardHead title="持仓 · 实时估值" sub="可卖 = T+1 解冻；成本含费用（买入金额+费用 ÷ 数量）；点行看K线" />
            {data.positions.length === 0 ? (
              <div className="chart-empty" style={{ padding: '20px 6px' }}>
                暂无持仓——去个股行情/雷达选票，在 K 线详情弹窗里「模拟买入」
              </div>
            ) : selling ? (
              <div style={{ padding: '6px 2px 2px' }}>
                <div className="fbar" style={{ marginBottom: 8 }}>
                  <span className="muted-c" style={{ fontSize: 12.5 }}>
                    卖出 {selling.symbol} {selling.name} · 现价 {fmt(selling.lastPrice)} · 可卖 {selling.availQty} 股 · 成本 {fmt(selling.costPrice)}
                  </span>
                  <input
                    className="input" style={{ flex: '0 0 110px' }} type="number" min={100} step={100}
                    value={sellQty}
                    onChange={e => setSellQty(e.target.value)}
                    placeholder={String(selling.availQty)}
                  />
                  <Button onClick={confirmSell} loading={busy}>确认卖出</Button>
                  <Button variant="ghost" onClick={() => setSelling(null)}>取消</Button>
                </div>
                {(() => {
                  // 盈亏测算：按现价估算成交额、费用与净盈亏（实际以成交价为准）
                  const sq = Math.min(Math.max(0, Math.floor(Number(sellQty) || 0)), selling.availQty)
                  if (sq < 100) return null
                  const amt = selling.lastPrice * sq
                  const fee = Math.max(5, amt * 0.00025) + amt * 0.00001
                  const tax = amt * 0.0005
                  const net = amt - fee - tax
                  const cost = selling.costPrice * sq
                  const pnl = net - cost
                  const pnlPct = cost > 0 ? pnl / cost * 100 : 0
                  return (
                    <div className="sell-estimate">
                      <span>预估成交 <b>{money(amt)}</b></span>
                      <span>费用+税 <b>{money(fee + tax)}</b></span>
                      <span>净入账 <b>{money(net)}</b></span>
                      <span className={signCls(pnl)}>
                        卖出净盈亏 <b>{pnl >= 0 ? '+' : '-'}{money(Math.abs(pnl))}（{pnlPct >= 0 ? '+' : ''}{pnlPct.toFixed(2)}%）</b>
                      </span>
                    </div>
                  )
                })()}
              </div>
            ) : (
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr>
                      <th>股票</th><th className="num">持仓</th><th className="num">可卖</th>
                      <th className="num">成本价</th><th className="num">现价</th><th className="num">当日%</th>
                      <th className="num">市值</th><th className="num">浮盈亏</th><th className="num">盈亏%</th><th className="num">今日盈亏</th><th></th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.positions.map(p => (
                      <tr
                        key={p.symbol} className="rowlink"
                        onClick={() => kline.openDetail({ symbol: p.symbol, name: p.name })}
                        onMouseEnter={e => kline.enterRow({ symbol: p.symbol, name: p.name }, e.currentTarget)}
                        onMouseLeave={kline.leaveRow}
                      >
                        <td><span className="code">{p.symbol}</span><span className="name">{p.name}</span></td>
                        <td className="num">{p.qty}</td>
                        <td className={`num ${p.availQty < p.qty ? 'muted' : ''}`}>{p.availQty}</td>
                        <td className="num">{fmt(p.costPrice)}</td>
                        <td className="num">{fmt(p.lastPrice)}</td>
                        <td className={`num ${signCls(p.dayChgPct)}`}>{p.dayChgPct >= 0 ? '+' : ''}{p.dayChgPct.toFixed(2)}%</td>
                        <td className="num">{money(p.marketValue)}</td>
                        <td className={`num ${signCls(p.pnl)}`}>{p.pnl >= 0 ? '+' : ''}{money(Math.abs(p.pnl))}</td>
                        <td className={`num ${signCls(p.pnl)}`}>{p.pnlPct >= 0 ? '+' : ''}{p.pnlPct.toFixed(2)}%</td>
                        <td className={`num ${signCls(p.dayPnl)}`}>{p.dayPnl >= 0 ? '+' : ''}{money(Math.abs(p.dayPnl))}</td>
                        <td>
                          <Button
                            variant="mini" disabled={p.availQty < 100}
                            onClick={e => { e.stopPropagation(); setSelling(p); setSellQty(String(p.availQty)) }}
                          >{p.availQty < 100 ? 'T+1 冻结' : '卖出'}</Button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>

          <Card>
            <CardHead title="净值曲线" sub="每日收盘记一点；查看页面时实时刷新当日" />
            <EquityCurve points={data.curve} initial={init} />
          </Card>

          {(data.orders ?? []).filter(o => o.status === 'open').length > 0 && (
            <Card>
              <CardHead title="限价挂单" sub="每 30 秒随快照检查触价：买入 ≤ 限价 / 卖出 ≥ 限价即自动成交；现金或 T+1 不满足时留单重试" />
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr><th>挂单时间</th><th>股票</th><th>方向</th><th className="num">限价</th><th className="num">数量</th><th>现价触达</th><th>备注 / 信号</th><th></th></tr>
                  </thead>
                  <tbody>
                    {(data.orders ?? []).filter(o => o.status === 'open').map(o => {
                      const pos = data.positions.find(p => p.symbol === o.symbol)
                      const last = pos?.lastPrice ?? 0
                      const near = last > 0
                        ? (o.side === 'buy'
                          ? `差 ${(((last - o.limitPrice) / o.limitPrice) * 100).toFixed(2)}%`
                          : `差 ${(((o.limitPrice - last) / o.limitPrice) * 100).toFixed(2)}%`)
                        : ''
                      const sig = o.signal as { stage?: string } | undefined
                      return (
                        <tr key={o.id}>
                          <td className="mono muted-c" style={{ fontSize: 11 }}>{o.createdAt}</td>
                          <td><span className="code">{o.symbol}</span><span className="name">{o.name}</span></td>
                          <td><span className={`badge-run ${o.side === 'sell' ? 'sell' : ''}`}>{o.side === 'buy' ? '限价买' : '限价卖'}</span></td>
                          <td className="num">{fmt(o.limitPrice)}</td>
                          <td className="num">{o.qty}</td>
                          <td className="num muted-c">{last > 0 ? `现价 ${fmt(last)} · ${near}` : '—'}</td>
                          <td className="muted-c" style={{ fontSize: 11.5 }}>
                            {sig?.stage ? <span className="badge-run">{sig.stage.toUpperCase()}</span> : null}{' '}{o.note || ''}
                          </td>
                          <td><Button variant="mini-danger" onClick={() => cancelOrder(o.id)}>撤单</Button></td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          )}

          <Card>
            <CardHead title="成交流水" sub={`共 ${data.trades.length} 笔（近 200）· 买入记录当时的雷达信号，复盘可回溯`} />
            {data.trades.length === 0 ? (
              <div className="chart-empty" style={{ padding: '18px 6px' }}>还没有成交——第一笔从个股 K 线详情弹窗开始</div>
            ) : (
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr><th>时间</th><th>股票</th><th>方向</th><th className="num">价格</th><th className="num">数量</th><th className="num">金额</th><th className="num">费用</th><th>备注 / 信号</th></tr>
                  </thead>
                  <tbody>
                    {data.trades.map(t => {
                      const sig = t.signal as { stage?: string; keyDate?: string } | undefined
                      return (
                        <tr key={t.id}>
                          <td className="mono muted-c" style={{ fontSize: 11 }}>{t.tradedAt}</td>
                          <td><span className="code">{t.symbol}</span><span className="name">{t.name}</span></td>
                          <td><span className={`badge-run ${t.side === 'buy' ? '' : 'sell'}`}>{t.side === 'buy' ? '买入' : '卖出'}</span></td>
                          <td className="num">{fmt(t.price)}</td>
                          <td className="num">{t.qty}</td>
                          <td className="num">{money(t.amount)}</td>
                          <td className="num muted">{money(t.fee + t.tax)}</td>
                          <td className="muted-c" style={{ fontSize: 11.5 }}>
                            {sig?.stage ? <span className="badge-run">{sig.stage.toUpperCase()}</span> : null}
                            {' '}{t.note || (sig?.keyDate ? `信号日 ${sig.keyDate}` : '')}
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </>
      )}

      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={symbol => navigate(`/?view=backtest&symbol=${encodeURIComponent(symbol)}`)} />
    </div>
  )
}
