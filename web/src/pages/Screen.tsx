import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
import type { MouseEvent as ReactMouseEvent, KeyboardEvent as ReactKeyboardEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type Quote, type ScreenResult, type NPSetup, type GuideRealtime } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Button, Card, EnvBanner, Chip, SearchPill, FilterBar, EmptyState, ErrorBlock, KpiCard, Skeleton, fmt, pct, type EnvTone } from '../components/ui'
import { useQueryState } from '../hooks/useQueryState'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'

const PAGE_SIZE = 50

type Stage = 'b1' | 'b2' | 'b3'

const stageTabs: Array<{ key: Stage; label: string; short: string; sub: string }> = [
  { key: 'b1', label: 'B1 回调低吸', short: 'B1 低吸', sub: '买点①：B段回撤进入 A段涨幅 0.382~0.5 黄金区 且出现企稳信号（★ 长下影/阳包阴/地量后放量阳/20日线首阳）——未止跌不进名单；止损=B段最低点' },
  { key: 'b2', label: 'B2 放量突破', short: 'B2 突破', sub: '买点②：B段≥3日后收盘 ≥ 颈线×1.01、量 ≥ B段均量×2、非大盘恐慌日；无量突破/擦线不算；止损=B段最低点，目标 C≈A' },
  { key: 'b3', label: 'B3 回踩确认', short: 'B3 回踩', sub: '突破后 3 日内回踩颈线不破（low ≤ 颈线×1.02、收盘 ≥ 颈线）；★=回踩缩量确认；跌回颈线下方=假突破出局' },
]

// B2 视角包含已进入回踩(b3)的票——突破事件仍在窗口内，B2 战术同适用。
function inTab(item: NPSetup, tab: Stage): boolean {
  if (tab === 'b1') return item.stage === 'b1'
  if (tab === 'b2') return item.stage === 'b2' || item.stage === 'b3'
  return item.stage === 'b3'
}

// 环境横幅：大盘温度五档映射三态（后端 /api/market/guide 的 stage 语义）
function envOf(rt: GuideRealtime | null): { tone: EnvTone; desc: string; time: string } | null {
  if (!rt) return null
  const tone: EnvTone = rt.stage === '活跃' || rt.stage === '亢奋' ? 'strong' : rt.stage === '中性' ? 'neutral' : 'weak'
  const desc = `大盘温度 ${rt.tempScore.toFixed(0)}（${rt.stage}）· 涨停 ${rt.limitUp} · 上涨/下跌 ${rt.upCount}/${rt.downCount} 家${rt.stage === '弱势' || tone === 'weak' ? ' · 候选谨慎参与' : ''}`
  return { tone, desc, time: rt.updatedAt || rt.asOf }
}

export default function Screen() {
  const navigate = useNavigate()
  const [days, setDays] = useState(10)
  const [daysInput, setDaysInput] = useState('10')
  const [result, setResult] = useState<ScreenResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [page, setPage] = useState(1)
  const [quotes, setQuotes] = useState<Record<string, Quote>>({})
  const [guide, setGuide] = useState<GuideRealtime | null>(null)
  const [expanded, setExpanded] = useState<string | null>(null)
  const [sel, setSel] = useState(-1)

  // 筛选状态进 URL：可分享、可回退（stage / 搜索词）
  const [stageParam, setStageParam] = useQueryState('stage')
  const [qParam, setQParam] = useQueryState('q')
  const tab: Stage = stageParam === 'b2' || stageParam === 'b3' ? stageParam : 'b1'
  const searchRef = useRef<HTMLInputElement>(null)

  const run = (d: number) => {
    setRunning(true)
    setError('')
    api.screen(d)
      .then(res => {
        setResult(res)
        setPage(1)
        setSel(-1)
        setExpanded(null)
      })
      .catch(e => setError(e instanceof Error ? e.message : '筛查失败'))
      .finally(() => setRunning(false))
  }

  // 挂载即扫一次 + 顺带取大盘温度（横幅）；失败静默降级为无横幅。
  useEffect(() => {
    run(days)
    api.marketGuide().then(g => setGuide(g.realtime)).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const q = qParam.trim().toLowerCase()
  const items = useMemo(() => {
    const all = (result?.items ?? []).filter(i => inTab(i, tab))
    if (!q) return all
    return all.filter(i => i.symbol.toLowerCase().includes(q) || (i.name ?? '').toLowerCase().includes(q))
  }, [result, tab, q])
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

  const openDetail = (s: NPSetup) => kline.openDetail({ symbol: s.symbol, name: s.name, key: s.keyDate, setup: s })

  const rowKey = (s: NPSetup) => s.symbol + s.keyDate

  // j/k 行移动 · Enter 详情 · / 搜索 · r 重扫（输入框聚焦自动跳过）
  useKeyboardShortcuts({
    j: () => setSel(i => Math.min(pageItems.length - 1, i + 1)),
    k: () => setSel(i => Math.max(0, i - 1)),
    Enter: () => { const s = pageItems[sel]; if (s) openDetail(s) },
    '/': () => searchRef.current?.focus(),
    r: () => { if (!running) run(days) },
  })

  useEffect(() => {
    const el = document.querySelector<HTMLTableRowElement>('.tb tbody tr.sel')
    el?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  const tabCount = (key: Stage) => (result?.items ?? []).filter(i => inTab(i, key)).length

  const quoteCell = (s: NPSetup) => {
    const qt = quotes[s.symbol]
    if (!qt) return <td className="num muted">—</td>
    return (
      <>
        <td className="num">{fmt(qt.price)}</td>
        <td className={`num ${qt.changePct >= 0 ? 'up-text' : 'down-text'}`}>
          {qt.changePct >= 0 ? '+' : ''}{pct(qt.changePct)}
        </td>
      </>
    )
  }

  // 行展开：三段明细（形态结构 / 量能结构 / 关键价位）——可解释性契约
  const ExpandRow = ({ s }: { s: NPSetup }) => (
    <tr className="expand">
      <td colSpan={16}>
        <div className="expand-grid">
          <div className="seg">
            <div className="t">形态结构</div>
            <div className="kv"><span className="k">A 段</span><span>{s.aStartDate.slice(5)} ~ {s.aEndDate.slice(5)}{s.hasLimitUp && ' · 含涨停'}</span></div>
            <div className="kv"><span className="k">A 段涨幅</span><span className="ok">+{s.aRisePct.toFixed(1)}%</span></div>
            <div className="kv"><span className="k">B 段回撤</span><span>{s.bDays} 天 · {pct(s.retrRatio * 100)}</span></div>
            <div className="kv"><span className="k">低吸区</span><span className="mono">{fmt(s.retr50)} ~ {fmt(s.retr382)}</span></div>
            {s.stage === 'b1' && (
              <div className="kv"><span className="k">企稳信号</span>
                {s.b1Triggered ? <span className="ok">★ {s.b1Signal}（{s.b1TriggerDate}）</span> : <span className="no">待触发</span>}
              </div>
            )}
          </div>
          <div className="seg">
            <div className="t">量能结构</div>
            <div className="kv"><span className="k">A 段量能</span><span>{fmt(s.aVolRatio)}×</span></div>
            <div className="kv"><span className="k">回调量比</span><span>{fmt(s.bVolRatio)}×A</span></div>
            {s.breakoutDate && <div className="kv"><span className="k">突破量能</span><span className="ok">{fmt(s.breakoutVolRatio)}×（{s.breakoutDate}）</span></div>}
            {s.retestDate && (
              <div className="kv"><span className="k">回踩缩量</span>
                {s.retestConfirm ? <span className="ok">★ 确认（{s.retestDate}）</span> : <span className="no">量偏大</span>}
              </div>
            )}
          </div>
          <div className="seg">
            <div className="t">关键价位</div>
            <div className="kv"><span className="k">颈线</span><span className="mono">{fmt(s.neckline)}</span></div>
            <div className="kv"><span className="k">止损（B 低）</span><span className="mono">{fmt(s.stopLoss)}</span></div>
            <div className="kv"><span className="k">目标 C≈A</span><span className="mono">{fmt(s.target)}</span></div>
            <div className="kv"><span className="k">追高限制</span>{s.chaseBan ? <span className="no">✗ 已过线</span> : <span className="ok">✓ 未过线</span>}</div>
            <div className="kv"><span className="k">MA20</span><span className="mono">{fmt(s.ma20)}</span></div>
          </div>
        </div>
      </td>
    </tr>
  )

  const rowProps = (s: NPSetup, idx: number) => ({
    className: `rowlink${sel === idx ? ' sel' : ''}`,
    title: '悬浮预览K线 · 点击查看形态详情',
    onClick: () => { setSel(idx); openDetail(s) },
    onMouseEnter: (e: ReactMouseEvent<HTMLTableRowElement>) =>
      kline.enterRow({ symbol: s.symbol, name: s.name, key: s.keyDate, setup: s }, e.currentTarget),
    onMouseLeave: kline.leaveRow,
  })

  const codeCell = (s: NPSetup) => (
    <td>
      <button
        className="expand-toggle"
        aria-label={expanded === rowKey(s) ? '收起明细' : '展开明细'}
        aria-expanded={expanded === rowKey(s)}
        onClick={e => { e.stopPropagation(); setExpanded(expanded === rowKey(s) ? null : rowKey(s)) }}
      >{expanded === rowKey(s) ? '▾' : '▸'}</button>
      <span className="code">{s.symbol}</span><span className="name">{s.name ?? ''}</span>
    </td>
  )

  const meta = stageTabs.find(t => t.key === tab)!
  const env = envOf(guide)
  const c = result?.counts

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>股票筛查</h1>
          <p>全市场 N 字战法筛查：首板涨停 → 缩量回调 → 放量突破 → 回踩企稳 · <span className="mono">{result ? `截至 ${result.asOf}` : '扫描中'}</span></p>
        </div>
        <div className="symbol-bar">
          <input
            type="number" min={1} step={1}
            value={daysInput}
            onChange={e => setDaysInput(e.target.value)}
            onKeyDown={(e: ReactKeyboardEvent) => {
              if (e.key === 'Enter' && !running) {
                const d = Math.max(1, Number(daysInput) || 10)
                setDays(d); run(d)
              }
            }}
            placeholder="10" spellCheck={false}
          />
          <Button disabled={running} onClick={() => {
            const d = Math.max(1, Number(daysInput) || 10)
            setDays(d); run(d)
          }}>{running ? '扫描中…' : `筛查近 ${Math.max(1, Number(daysInput) || 10)} 日`}</Button>
        </div>
      </header>

      {env && <EnvBanner tone={env.tone} desc={env.desc} time={env.time} />}

      {error ? (
        <ErrorBlock title="筛查失败" desc={error} onRetry={() => run(days)} />
      ) : !result ? (
        <Card className="skel-card"><div style={{ display: 'grid', gap: 14 }}><Skeleton w="30%" /><Skeleton w="100%" h={40} count={6} /></div></Card>
      ) : (
        <>
          <div className="kpis">
            <KpiCard tone="a" label="存活形态" value={String((c?.b1 ?? 0) + (c?.b2 ?? 0) + (c?.b3 ?? 0))} icon={<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden><path d="M12 3l7 9-7 9-7-9z" /></svg>} />
            <KpiCard tone="o" label="B1 回调低吸" value={String(c?.b1 ?? 0)} icon={<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden><path d="M4 17l5-6 4 4 7-9" /></svg>} />
            <KpiCard tone="g" label="B2 放量突破（主信号）" value={String(c?.b2 ?? 0)} icon={<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden><path d="M13 2L4 14h6l-1 8 9-12h-6z" /></svg>} />
            <KpiCard tone="p" label="B3 回踩确认" value={String(c?.b3 ?? 0)} icon={<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden><path d="M3 12h4l3-8 4 16 3-8h4" /></svg>} />
          </div>

          <FilterBar>
            {stageTabs.map(t => (
              <Chip key={t.key} active={tab === t.key} onClick={() => { setStageParam(t.key === 'b1' ? null : t.key); setPage(1); setSel(-1); setExpanded(null) }}>
                {t.short}（{tabCount(t.key)}）
              </Chip>
            ))}
            <span className="muted-c" style={{ fontSize: 11.5 }}>{meta.sub}</span>
            <SearchPill ref={searchRef} value={qParam} onChange={e => { setQParam(e.target.value || null); setPage(1); setSel(-1) }} />
          </FilterBar>

          {pageItems.length ? (
            <div className="table-panel">
              <div className="table-wrap" onScroll={kline.leaveRow}>
                {tab === 'b1' && (
                  <table className="tb">
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
                      {pageItems.map((s, idx) => (
                        <Fragment key={rowKey(s)}>
                          <tr {...rowProps(s, idx)}>
                            {codeCell(s)}
                            <td>{s.aStartDate.slice(5)}~{s.aEndDate.slice(5)}{s.hasLimitUp && <span className="wash-star" title="A段含涨停（涨停基因）"> 板</span>}</td>
                            <td className="num up-text">+{s.aRisePct.toFixed(1)}%</td>
                            <td className="num">{fmt(s.aVolRatio)}×</td>
                            <td className="num">{fmt(s.neckline)}</td>
                            <td className="num">{fmt(s.retr50)}~{fmt(s.retr382)}</td>
                            <td className="num">{s.bDays}天</td>
                            <td className="num">{pct(s.retrRatio * 100)}</td>
                            <td className="num">{fmt(s.bVolRatio)}×A</td>
                            <td>{s.b1Triggered ? <span className="wash-star" title={s.b1TriggerDate}>★{s.b1Signal}</span> : '待触发'}</td>
                            <td className="num">{fmt(s.stopLoss)}</td>
                            <td className="num">{fmt(s.target)}</td>
                            <td>{s.chaseBan ? <span className="down-text">过线</span> : '—'}</td>
                            {quoteCell(s)}
                          </tr>
                          {expanded === rowKey(s) && <ExpandRow s={s} />}
                        </Fragment>
                      ))}
                    </tbody>
                  </table>
                )}
                {tab === 'b2' && (
                  <table className="tb">
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
                      {pageItems.map((s, idx) => (
                        <Fragment key={rowKey(s)}>
                          <tr {...rowProps(s, idx)}>
                            {codeCell(s)}
                            <td className="num up-text">+{s.aRisePct.toFixed(1)}%</td>
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
                            <td>{s.chaseBan ? <span className="down-text">过线</span> : '—'}</td>
                            {quoteCell(s)}
                          </tr>
                          {expanded === rowKey(s) && <ExpandRow s={s} />}
                        </Fragment>
                      ))}
                    </tbody>
                  </table>
                )}
                {tab === 'b3' && (
                  <table className="tb">
                    <thead>
                      <tr>
                        <th>代码 / 名称</th><th>突破日</th><th className="num">突破价</th>
                        <th>回踩日</th><th className="num">回踩低点</th><th>缩量确认</th>
                        <th className="num">颈线</th><th className="num">止损(B低)</th><th className="num">目标</th>
                        <th className="num">现价</th><th className="num">当日</th>
                      </tr>
                    </thead>
                    <tbody>
                      {pageItems.map((s, idx) => (
                        <Fragment key={rowKey(s)}>
                          <tr {...rowProps(s, idx)}>
                            {codeCell(s)}
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
                          {expanded === rowKey(s) && <ExpandRow s={s} />}
                        </Fragment>
                      ))}
                    </tbody>
                  </table>
                )}
              </div>
              <div className="table-foot">
                {tab === 'b1' && meta.label}· 共 {items.length} 条 · 第 {safePage}/{totalPages} 页
                {totalPages > 1 && (
                  <span className="keys" style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
                    <Button variant="mini" disabled={safePage <= 1} onClick={() => setPage(safePage - 1)}>‹ 上一页</Button>
                    <Button variant="mini" disabled={safePage >= totalPages} onClick={() => setPage(safePage + 1)}>下一页 ›</Button>
                  </span>
                )}
                <span className="keys"><span><kbd>j</kbd>/<kbd>k</kbd> 行移动</span><span><kbd>Enter</kbd> 详情</span><span><kbd>/</kbd> 搜索</span><span><kbd>r</kbd> 重扫</span></span>
              </div>
            </div>
          ) : (
            <div className="table-panel">
              <EmptyState
                title={`近 ${result.windowDays} 个交易日内没有${tab === 'b1' ? '回调低吸' : tab === 'b2' ? '放量突破' : '回踩企稳'}阶段的存活形态`}
                desc="常见于弱势环境或窗口过短。可拉长筛查窗口，或切换其他买点阶段查看。"
                actions={<>
                  <Button variant="ghost" onClick={() => { const d = Math.min(60, days + 10); setDays(d); setDaysInput(String(d)); run(d) }}>拉长窗口到 {Math.min(60, days + 10)} 日</Button>
                  <Button variant="ghost" onClick={() => { setStageParam(null); setQParam(null) }}>看全部买点</Button>
                </>}
              />
            </div>
          )}
        </>
      )}

      {/* 悬浮K线预览 + 形态详情弹窗（共享组件） */}
      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={openKline} />
    </div>
  )
}
