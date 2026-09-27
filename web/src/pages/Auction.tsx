import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type AuctionMover, type AuctionResult } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Banner, Button, Card, CardHead, KpiCard } from '../components/ui'
import { useIsActive } from '../shell'

// 竞价异动：9:25 定格后的开盘晨报——自选承接 / 梯队承接 / 题材点火 /
// 全市场异动榜。快照 60s 轮询（激活时）；竞价窗口内即为准实时。

function fmtAmt(v: number): string {
  if (!v) return '—'
  if (v >= 1e8) return `${(v / 1e8).toFixed(2)}亿`
  if (v >= 1e4) return `${(v / 1e4).toFixed(0)}万`
  return v.toFixed(0)
}
const gapCls = (g: number) => (g > 0 ? 'up-text' : g < 0 ? 'down-text' : '')
const gapText = (g: number) => `${g >= 0 ? '+' : ''}${g.toFixed(2)}%`
const kindTone = (k: string) =>
  k.includes('警示') ? 'down-text' : k.includes('放量') || k.includes('一字') ? 'up-text' : 'muted-c'

export default function Auction() {
  const navigate = useNavigate()
  const kline = useKlinePreview()
  const isActiveView = useIsActive()
  const [data, setData] = useState<AuctionResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [feedback, setFeedback] = useState({ text: '', kind: 'info' as 'info' | 'error' | 'success' })
  const [archiving, setArchiving] = useState(false)

  const load = useCallback(() => {
    api.auction()
      .then(r => { setData(r); setLoading(false) })
      .catch(e => { setFeedback({ text: e instanceof Error ? e.message : '加载失败', kind: 'error' }); setLoading(false) })
  }, [])

  useEffect(() => {
    if (!isActiveView) return
    load()
    const t = window.setInterval(load, 60_000)
    return () => window.clearInterval(t)
  }, [isActiveView, load])

  const archive = async () => {
    setArchiving(true)
    try {
      const r = await api.auctionArchive()
      setFeedback({ text: `已归档 ${r.date} 竞价定格与晨报。`, kind: 'success' })
      load()
    } catch (e) {
      setFeedback({ text: e instanceof Error ? e.message : '归档失败', kind: 'error' })
    } finally {
      setArchiving(false)
    }
  }

  const openDetail = (m: AuctionMover) =>
    kline.openDetail({ symbol: m.symbol, name: m.name, sub: m.board })

  const moverHover = (m: AuctionMover) =>
    ({
      onMouseEnter: (e: React.MouseEvent<HTMLTableRowElement>) =>
        kline.enterRow({ symbol: m.symbol, name: m.name, sub: m.board }, e.currentTarget),
      onMouseLeave: kline.leaveRow,
    })

  const st = data?.stats ?? {}

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>竞价异动</h1>
          <p>{data ? `${data.phase} · 快照 ${data.asOf}` : '加载中…'}{data?.archivedAt ? ` · ${data.archivedAt.includes('已归档') ? data.archivedAt : `已归档 ${data.archivedAt}`}` : ''}</p>
        </div>
        <span style={{ display: 'flex', gap: 8 }}>
          {data && !data.archivedAt && (
            <Button variant="ghost" onClick={archive} loading={archiving}>归档今日竞价</Button>
          )}
          <Button variant="ghost" onClick={load}>刷新</Button>
        </span>
      </header>

      <Banner text={feedback.text} kind={feedback.kind} />

      {loading && !data ? (
        <Card><div className="chart-empty">正在计算竞价异动…</div></Card>
      ) : data && (
        <>
          <div className="kpis">
            <KpiCard label="竞价高开 ≥5%" value={String(st.gapUp5 ?? 0)} valueClass="up-text" hint={`一字开 ${st.limitOpen ?? 0} 只`} />
            <KpiCard label="点火概念" value={String(st.themes ?? 0)} hint="高开股按概念聚集" />
            <KpiCard
              label="梯队承接"
              value={st.ladder ? `${st.ladderUp}/${st.ladder}` : '—'}
              valueClass={st.ladder && st.ladderUp / st.ladder >= 0.6 ? 'up-text' : st.ladder && st.ladderUp / st.ladder <= 0.3 ? 'down-text' : undefined}
              hint="昨连板今竞价高开数"
            />
            <KpiCard label="涨 / 跌家数" value={`${st.upCnt ?? 0} / ${st.downCnt ?? 0}`} />
          </div>

          <Card>
            <CardHead title="开盘三问" sub="①梯队承接定接力 · ②主线延续定持仓 · ③新点火定新仓" />
            <div className="kv"><span className="k">① 昨日梯队</span><span>{data.q1}</span></div>
            <div className="kv"><span className="k">② 主线延续</span><span>{data.q2}</span></div>
            <div className="kv"><span className="k">③ 新点火</span><span>{data.q3}</span></div>
          </Card>

          {data.themes.length > 0 && (
            <Card>
              <CardHead title="题材点火" sub="竞价高开 ≥5% 按概念聚集，概念规模分档阈值（小概念3只 / 中5只 / 大8只）· 点个股看K线" />
              <div className="ladder">
                {data.themes.map(t => (
                  <div key={t.concept} className="ladder-tier">
                    <div className="tier-head">
                      <b className="tier-name">{t.concept}</b>
                      <span className="muted">{t.count} 只高开 · 成分 {t.size} 家 · 最高 +{t.maxGap.toFixed(1)}%</span>
                    </div>
                    <div className="tier-body">
                      <span className="chips">
                        {t.stocks.map(m => (
                          <button key={m.symbol} className="ladder-chip" onClick={() => openDetail(m)}
                            title={`${m.symbol} ${m.kind} · 点击看K线`}>
                            {m.name || m.symbol}
                            <span className={gapCls(m.gapPct)}>{gapText(m.gapPct)}</span>
                          </button>
                        ))}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          )}

          <Card>
            <CardHead title="自选承接" sub="自选池的竞价表现 · 平稳也在列 · 星标可在自选股页管理" />
            {data.watch.length ? (
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr><th>代码</th><th>名称</th><th className="num">竞价涨幅</th><th className="num">竞价额</th><th>类型</th></tr>
                  </thead>
                  <tbody>
                    {data.watch.map(m => (
                      <tr key={m.symbol} className="rowlink" onClick={() => openDetail(m)} {...moverHover(m)}>
                        <td><span className="code">{m.symbol}</span></td>
                        <td>{m.name || m.symbol}</td>
                        <td className={`num ${gapCls(m.gapPct)}`}>{gapText(m.gapPct)}</td>
                        <td className="num muted-c">{fmtAmt(m.amt)}</td>
                        <td className={kindTone(m.kind)}>{m.kind}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="chart-empty">自选池为空——去个股行情点 ☆ 星标，明早这里看它们的竞价承接。</div>
            )}
          </Card>

          {data.ladder.length > 0 && (
            <Card>
              <CardHead title="梯队承接" sub="昨日连板选手的今竞价表现 · 高开=接力意愿 · 低开=分歧" />
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr><th>昨高度</th><th>名称</th><th className="num">竞价涨幅</th><th className="num">竞价额</th></tr>
                  </thead>
                  <tbody>
                    {data.ladder.map(r => (
                      <tr key={r.symbol} className="rowlink"
                        onClick={() => kline.openDetail({ symbol: r.symbol, name: r.name })}
                        onMouseEnter={e => kline.enterRow({ symbol: r.symbol, name: r.name }, e.currentTarget)}
                        onMouseLeave={kline.leaveRow}>
                        <td><span className="concept-chip board-chip">{r.height === 1 ? '首板' : `${r.height}板`}</span></td>
                        <td>{r.name}</td>
                        <td className={`num ${gapCls(r.gapPct)}`}>{gapText(r.gapPct)}</td>
                        <td className="num muted-c">{fmtAmt(r.amt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Card>
          )}

          <Card>
            <CardHead title={`全市场异动榜（Top ${data.movers.length}）`} sub="按竞价涨跌幅排序 · 类型区分抢筹/虚高/核按钮" />
            {data.movers.length ? (
              <div className="table-wrap">
                <table className="tb">
                  <thead>
                    <tr><th>代码</th><th>名称</th><th className="num">竞价涨幅</th><th className="num">竞价额</th><th className="num">占昨额</th><th>类型</th><th>板块</th></tr>
                  </thead>
                  <tbody>
                    {data.movers.map(m => (
                      <tr key={m.symbol} className="rowlink" onClick={() => openDetail(m)} {...moverHover(m)}>
                        <td><span className="code">{m.symbol}</span></td>
                        <td>{m.name || m.symbol}</td>
                        <td className={`num ${gapCls(m.gapPct)}`}>{gapText(m.gapPct)}</td>
                        <td className="num muted-c">{fmtAmt(m.amt)}</td>
                        <td className="num muted-c">{m.amtCap >= 0 ? `${(m.amtCap * 100).toFixed(1)}%` : '—'}</td>
                        <td className={kindTone(m.kind)}>{m.kind}</td>
                        <td className="muted-c">{m.board || '—'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="chart-empty">暂无满足阈值的竞价异动。</div>
            )}
          </Card>
        </>
      )}

      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={symbol => navigate(`/?symbol=${symbol}`)} />
    </div>
  )
}
