import { useEffect, useMemo, useState } from 'react'
import type { MouseEvent as ReactMouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Quote, type ScreenResult, type NPSetup } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Banner, Card, CardHead, Empty, Spinner, fmt, pct } from '../components/ui'

const PAGE_SIZE = 50

type Stage = 'b1' | 'b2' | 'b3'

const stageTabs: Array<{ key: Stage; label: string; sub: string }> = [
  { key: 'b1', label: '回调低吸 B1', sub: '买点①：B段回撤进入 A段涨幅 0.382~0.5 黄金区 且出现企稳信号（★ 长下影/阳包阴/地量后放量阳/20日线首阳）——未止跌不进名单；止损=B段最低点' },
  { key: 'b2', label: '放量突破 B2', sub: '买点②：B段≥3日后收盘 ≥ 颈线×1.01、量 ≥ B段均量×2、非大盘恐慌日；无量突破/擦线不算；止损=B段最低点，目标 C≈A' },
  { key: 'b3', label: '回踩确认 B3', sub: '突破后 3 日内回踩颈线不破（low ≤ 颈线×1.02、收盘 ≥ 颈线）；★=回踩缩量确认；跌回颈线下方=假突破出局' },
]

// B2 视角包含已进入回踩(b3)的票——突破事件仍在窗口内，B2 战术同适用。
function inTab(item: NPSetup, tab: Stage): boolean {
  if (tab === 'b1') return item.stage === 'b1'
  if (tab === 'b2') return item.stage === 'b2' || item.stage === 'b3'
  return item.stage === 'b3'
}

export default function Screen() {
  const navigate = useNavigate()
  const [days, setDays] = useState(10)
  const [daysInput, setDaysInput] = useState('10')
  const [result, setResult] = useState<ScreenResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [tab, setTab] = useState<Stage>('b1')
  const [page, setPage] = useState(1)
  const [quotes, setQuotes] = useState<Record<string, Quote>>({})

  const run = (d: number) => {
    setRunning(true)
    setError('')
    api.screen(d)
      .then(res => {
        setResult(res)
        setPage(1)
      })
      .catch(e => setError(e instanceof Error ? e.message : '筛查失败'))
      .finally(() => setRunning(false))
  }

  // 挂载即扫一次；之后手动点「开始筛查」刷新。
  useEffect(() => { run(days) }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const items = useMemo(() => (result?.items ?? []).filter(i => inTab(i, tab)), [result, tab])
  const totalPages = Math.max(1, Math.ceil(items.length / PAGE_SIZE))
  const safePage = Math.min(page, totalPages)
  const pageItems = useMemo(
    () => items.slice((safePage - 1) * PAGE_SIZE, safePage * PAGE_SIZE),
    [items, safePage],
  )

  // 当前页的实时行情（现价/当日涨跌），行情缓存未就绪时静默留空。
  const quoteKey = useMemo(() => pageItems.map(i => i.symbol).join(','), [pageItems])
  useEffect(() => {
    if (!quoteKey) { setQuotes({}); return }
    let cancelled = false
    api.quotes(quoteKey.split(','))
      .then(qs => {
        if (cancelled) return
        const m: Record<string, Quote> = {}
        for (const q of qs) m[q.symbol] = q
        setQuotes(m)
      })
      .catch(() => { if (!cancelled) setQuotes({}) })
    return () => { cancelled = true }
  }, [quoteKey])

  const openKline = (symbol: string) => navigate(`/?symbol=${encodeURIComponent(symbol)}`)

  // 行级 K 线预览与详情弹窗（悬浮预览 / 点击弹大图，逻辑在 klinePreview）
  const kline = useKlinePreview()

  const rowProps = (s: NPSetup) => ({
    className: 'row-clickable',
    title: '悬浮预览K线 · 点击查看形态详情',
    onClick: () => kline.openDetail({ symbol: s.symbol, name: s.name, key: s.keyDate, setup: s }),
    onMouseEnter: (e: ReactMouseEvent<HTMLTableRowElement>) =>
      kline.enterRow({ symbol: s.symbol, name: s.name, key: s.keyDate, setup: s }, e.currentTarget),
    onMouseLeave: kline.leaveRow,
  })

  const tabCount = (key: Stage) => {
    const all = result?.items ?? []
    return all.filter(i => inTab(i, key)).length
  }

  const quoteCell = (s: NPSetup) => {
    const q = quotes[s.symbol]
    if (!q) return <td className="num muted">—</td>
    return (
      <>
        <td className="num">{fmt(q.price)}</td>
        <td className={`num ${q.changePct >= 0 ? 'pos' : 'neg'}`}>
          {q.changePct >= 0 ? '+' : ''}{pct(q.changePct)}
        </td>
      </>
    )
  }

  const meta = stageTabs.find(t => t.key === tab)!
  const activeSub = meta.sub

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>股票筛查</h1>
          <p>全市场 N 字战法筛查：首板涨停 → 缩量回调 → 放量突破 → 回踩企稳</p>
        </div>
        <div className="symbol-bar">
          <input
            type="number"
            min={1}
            step={1}
            value={daysInput}
            onChange={e => setDaysInput(e.target.value)}
            onKeyDown={e => {
              if (e.key === 'Enter' && !running) {
                const d = Math.max(1, Number(daysInput) || 10)
                setDays(d); run(d)
              }
            }}
            placeholder="10"
            spellCheck={false}
          />
          <button className="btn primary" disabled={running} onClick={() => {
            const d = Math.max(1, Number(daysInput) || 10)
            setDays(d); run(d)
          }}>
            {running ? '筛查中…' : `筛查近 ${Math.max(1, Number(daysInput) || 10)} 日`}
          </button>
        </div>
      </header>

      <Banner
        text={error || (result ? `数据截至 ${result.asOf} · 窗口：近 ${result.windowDays} 个交易日 · 存活形态 ${result.counts.b1 + result.counts.b2 + result.counts.b3} 个（B1 ${result.counts.b1} / B2+ ${result.counts.b2} / B3 ${result.counts.b3}）` : '正在扫描全市场…')}
        kind={error ? 'error' : 'info'}
      />

      {running && !result ? (
        <Spinner text="正在扫描全市场日线…" />
      ) : (
        <Card>
          <div className="strategy-switch screen-tabs">
            {stageTabs.map(t => (
              <button
                key={t.key}
                className={`btn small ${tab === t.key ? 'primary' : 'ghost'}`}
                onClick={() => { setTab(t.key); setPage(1) }}
              >
                {t.label}（{tabCount(t.key)}）
              </button>
            ))}
          </div>
          <CardHead title={meta.label} sub={activeSub} />

          {pageItems.length ? (
            <div className="table-wrap" onScroll={kline.leaveRow}>
              {tab === 'b1' && (
                <table>
                  <thead>
                    <tr>
                      <th>代码 / 名称</th><th>A段</th><th className="num">A涨幅</th><th className="num">A量能</th>
                      <th className="num">颈线</th><th className="num">低吸区(0.382~0.5)</th>
                      <th className="num">回调</th><th className="num">回撤</th><th className="num">回调量比</th>
                      <th>企稳信号</th><th className="num">止损(B低)</th><th className="num">目标</th><th>追高</th>
                      <th className="num">现价</th><th className="num">当日</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageItems.map(s => (
                      <tr key={s.symbol + s.keyDate} {...rowProps(s)}>
                        <td><b>{s.symbol}</b>{s.name ? ` ${s.name}` : ''}</td>
                        <td>{s.aStartDate.slice(5)}~{s.aEndDate.slice(5)}{s.hasLimitUp && <span className="wash-star" title="A段含涨停（涨停基因）"> 板</span>}</td>
                        <td className="num pos">+{s.aRisePct.toFixed(1)}%</td>
                        <td className="num">{fmt(s.aVolRatio)}×</td>
                        <td className="num">{fmt(s.neckline)}</td>
                        <td className="num">{fmt(s.retr50)}~{fmt(s.retr382)}</td>
                        <td className="num">{s.bDays}天</td>
                        <td className="num">{pct(s.retrRatio * 100)}</td>
                        <td className="num">{fmt(s.bVolRatio)}×A</td>
                        <td>{s.b1Triggered ? <span className="wash-star" title={s.b1TriggerDate}>★{s.b1Signal}</span> : '待触发'}</td>
                        <td className="num">{fmt(s.stopLoss)}</td>
                        <td className="num">{fmt(s.target)}</td>
                        <td>{s.chaseBan ? <span className="neg">过线</span> : '—'}</td>
                        {quoteCell(s)}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {tab === 'b2' && (
                <table>
                  <thead>
                    <tr>
                      <th>代码 / 名称</th><th>A段涨幅</th>
                      <th>突破日</th><th className="num">突破价</th><th className="num">突破量能</th>
                      <th className="num">颈线</th><th className="num">突破后</th><th>回踩状态</th>
                      <th className="num">止损(B低)</th><th className="num">目标</th><th>追高</th>
                      <th className="num">现价</th><th className="num">当日</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageItems.map(s => (
                      <tr key={s.symbol + s.keyDate} {...rowProps(s)}>
                        <td><b>{s.symbol}</b>{s.name ? ` ${s.name}` : ''}</td>
                        <td className="num pos">+{s.aRisePct.toFixed(1)}%</td>
                        <td>{s.breakoutDate}</td>
                        <td className="num">{fmt(s.breakoutPrice)}</td>
                        <td className="num">{fmt(s.breakoutVolRatio)}×</td>
                        <td className="num">{fmt(s.neckline)}</td>
                        <td className="num">{s.daysSinceBreakout}天</td>
                        <td>
                          {s.stage === 'b2' ? '未回踩' : s.retestConfirm
                            ? <span className="wash-star" title={s.retestDate}>★缩量确认</span>
                            : '回踩中'}
                        </td>
                        <td className="num">{fmt(s.stopLoss)}</td>
                        <td className="num">{fmt(s.target)}</td>
                        <td>{s.chaseBan ? <span className="neg">过线</span> : '—'}</td>
                        {quoteCell(s)}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}
              {tab === 'b3' && (
                <table>
                  <thead>
                    <tr>
                      <th>代码 / 名称</th><th>突破日</th><th className="num">突破价</th>
                      <th>回踩日</th><th className="num">回踩低点</th><th>缩量确认</th>
                      <th className="num">颈线</th><th className="num">止损(B低)</th><th className="num">目标</th>
                      <th className="num">现价</th><th className="num">当日</th>
                    </tr>
                  </thead>
                  <tbody>
                    {pageItems.map(s => (
                      <tr key={s.symbol + s.keyDate} {...rowProps(s)}>
                        <td><b>{s.symbol}</b>{s.name ? ` ${s.name}` : ''}</td>
                        <td>{s.breakoutDate}</td>
                        <td className="num">{fmt(s.breakoutPrice)}</td>
                        <td>{s.retestDate ?? '—'}</td>
                        <td className="num">{s.retestLow > 0 ? fmt(s.retestLow) : '—'}</td>
                        <td>{s.retestConfirm ? <span className="wash-star" title={s.retestDate}>★</span> : '量偏大'}</td>
                        <td className="num">{fmt(s.neckline)}</td>
                        <td className="num">{fmt(s.stopLoss)}</td>
                        <td className="num">{fmt(s.target)}</td>
                        {quoteCell(s)}
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}

              {totalPages > 1 && (
                <div className="pager">
                  <button className="btn ghost small" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>‹ 上一页</button>
                  <span className="muted">第 {safePage} / {totalPages} 页 · 共 {items.length} 条</span>
                  <button className="btn ghost small" disabled={safePage >= totalPages} onClick={() => setPage(safePage + 1)}>下一页 ›</button>
                </div>
              )}
            </div>
          ) : (
            <Empty text={`近 ${result?.windowDays ?? days} 个交易日内没有${tab === 'b1' ? '回调低吸' : tab === 'b2' ? '放量突破' : '回踩企稳'}阶段的存活形态`} />
          )}
        </Card>
      )}

      {/* 悬浮K线预览 + 形态详情弹窗（共享组件） */}
      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={openKline} />
    </div>
  )
}
