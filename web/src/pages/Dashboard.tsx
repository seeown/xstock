import { useCallback, useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, type BacktestResult, type Candle, type NParams, type Signal, type Trade } from '../api'
import KlineChart from '../components/KlineChart'
import PriceChart from '../components/PriceChart'
import StockProfileCard from '../components/StockProfileCard'
import {
  Banner, Card, CardHead, Empty, MetricCard, Spinner, exitReasonText, fmt, money, pct,
} from '../components/ui'

const signalFields: Array<{ key: keyof NParams; label: string; unit: string; step?: number; min: number }> = [
  { key: 'riseDays', label: '上涨周期', unit: '天', min: 2 },
  { key: 'riseMinPct', label: '最小涨幅', unit: '%', step: 0.1, min: 0.1 },
  { key: 'pullbackMinDays', label: '最短回调', unit: '天', min: 1 },
  { key: 'pullbackMaxDays', label: '最长回调', unit: '天', min: 1 },
  { key: 'pullbackMaxPct', label: '最大回撤', unit: '%', step: 0.1, min: 0.1 },
  { key: 'pullbackVolRatioMax', label: '回调量比', unit: '×', step: 0.05, min: 0.1 },
  { key: 'volumeRatioMin', label: '突破量比', unit: '×', step: 0.1, min: 0.1 },
]

const tradeFields: Array<{ key: keyof NParams; label: string; unit: string; step?: number; min: number }> = [
  { key: 'stopLossPct', label: '止损线', unit: '%', step: 0.1, min: 0.1 },
  { key: 'takeProfitPct', label: '止盈线', unit: '%', step: 0.1, min: 0.1 },
  { key: 'maxHoldDays', label: '最长持仓', unit: '天', min: 1 },
]

interface Feedback {
  text: string
  kind: 'info' | 'error' | 'success'
}

export default function Dashboard() {
  const [searchParams] = useSearchParams()
  const initialSymbol = (searchParams.get('symbol') || '000021.SZ').toUpperCase()

  const [symbolInput, setSymbolInput] = useState(initialSymbol)
  const [activeSymbol, setActiveSymbol] = useState(initialSymbol)
  const [params, setParams] = useState<NParams | null>(null)
  const [windowDays, setWindowDays] = useState(10)
  const [allHistory, setAllHistory] = useState(false)
  const [focus, setFocus] = useState<{ from: string; to: string; label: string; trade?: Trade; signal?: Signal } | null>(null)
  // 最新K线序列的引用，供点击回调读取而不闭包旧值。
  const barsRef = useRef<Candle[]>([])

  // 聚焦某笔交易/信号：K线图缩放到其邻域（前 45 根覆盖上涨与回调，后 15 根看离场）。
  const focusAt = useCallback((anchor: string, tail: string | undefined, label: string, mark: { trade?: Trade; signal?: Signal }) => {
    setFocus(prev => {
      const list = barsRef.current
      if (!list.length) return prev
      const idx = list.findIndex(b => b.date === anchor)
      if (idx < 0) return prev
      const endIdx = tail ? Math.max(idx, list.findIndex(b => b.date === tail)) : idx
      const from = list[Math.max(0, idx - 45)].date
      const to = list[Math.min(list.length - 1, endIdx + 15)].date
      return { from, to, label, trade: mark.trade, signal: mark.signal }
    })
    document.querySelector('.kline-wrap')?.scrollIntoView({ behavior: 'smooth', block: 'center' })
  }, [])

  const handleMarkClick = useCallback((m: { kind: 'signal' | 'trade'; signal?: Signal; trade?: Trade }) => {
    if (m.trade) focusAt(m.trade.buyDate, m.trade.sellDate, `${m.trade.buyDate} 买入`, m)
    else if (m.signal) focusAt(m.signal.date, undefined, `${m.signal.date} 信号`, m)
  }, [focusAt])
  const [bars, setBars] = useState<Candle[]>([])
  const [result, setResult] = useState<BacktestResult | null>(null)
  const [running, setRunning] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>({ text: '', kind: 'info' })

  const run = useCallback(async (sym: string, p: NParams) => {
    const symbol = sym.trim().toUpperCase()
    if (!symbol) {
      setFeedback({ text: '请输入股票代码。', kind: 'error' })
      return
    }
    setRunning(true)
    const days = allHistory ? 0 : Math.max(1, windowDays || 1)
    const scope = days > 0 ? `近 ${days} 个交易日` : '全部历史'
    setFeedback({ text: `正在回测 ${symbol}（${scope}）…`, kind: 'info' })
    try {
      const [bt, barData] = await Promise.all([api.backtest(symbol, p, days), api.bars(symbol)])
      setResult(bt)
      setBars(barData)
      barsRef.current = barData
      setFocus(null)
      setActiveSymbol(symbol)
      const windowNote = bt.windowStart ? `，区间 ${bt.windowStart} ~ ${barData[barData.length - 1]?.date ?? ''}` : ''
      setFeedback({ text: `回测完成（${scope}${windowNote}）：${symbol} 发现 ${(bt.signals ?? []).length} 个 N 字信号。`, kind: 'success' })
    } catch (e) {
      setResult(null)
      setBars([])
      const msg = e instanceof Error ? e.message : '回测失败'
      setFeedback({
        text: msg.includes('not found') ? `${symbol} 尚无日线数据，请先到「个股」或「数据管理」同步。` : msg,
        kind: 'error',
      })
    } finally {
      setRunning(false)
    }
  }, [allHistory, windowDays])

  // 默认参数只在挂载/切换标的时加载一次——绝不能依赖 run（它随
  // allHistory/windowDays 变化，否则切换「全部历史」会重置用户改过的参数）。
  useEffect(() => {
    ;(async () => {
      try {
        const p = await api.defaultParams()
        setParams(p)
        await run(initialSymbol, p)
      } catch {
        setFeedback({ text: '无法连接后端服务。', kind: 'error' })
      }
    })()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialSymbol])

  // 「全部历史」开关切换时用当前参数立即重跑（首跑由上面的初始化完成；
  // 时间范围天数的修改与参数一样，点「运行回测」生效）。
  const scopeBooted = useRef(false)
  useEffect(() => {
    if (!scopeBooted.current) {
      scopeBooted.current = true
      return
    }
    if (params) run(activeSymbol, params)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [allHistory])


  if (!params) {
    return (
      <div className="page">
        <Spinner text="正在初始化策略参数…" />
      </div>
    )
  }

  const m = result?.metrics
  const disabled = running

  const tradesList = result?.trades ?? []
  const focusTradeIdx = focus?.trade ? Math.max(0, tradesList.findIndex(t => t.buyDate === focus.trade!.buyDate)) : -1
  const stepTrade = (dir: number) => {
    const list = result?.trades ?? []
    if (!list.length) return
    const cur = focus?.trade ? list.findIndex(t => t.buyDate === focus.trade!.buyDate) : -1
    const next = list[Math.min(list.length - 1, Math.max(0, (cur < 0 ? 0 : cur) + dir))]
    if (next) focusAt(next.buyDate, next.sellDate, `${next.buyDate} 买入`, { trade: next })
  }


  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>回测分析</h1>
          <p>N 字战法筛选、回测与信号可视化</p>
        </div>
        <div className="symbol-bar">
          <input
            value={symbolInput}
            onChange={e => setSymbolInput(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && !disabled && run(symbolInput, params)}
            placeholder="000021.SZ"
            spellCheck={false}
          />
          <button className="btn primary" onClick={() => run(symbolInput, params)} disabled={disabled}>
            {running ? '回测中…' : '运行回测'}
          </button>
        </div>
      </header>

      <Banner text={feedback.text} kind={feedback.kind} />

      <div className="dashboard-grid">
        <div className="dashboard-main">
          <div className="metrics-row">
            <MetricCard label="总收益" value={m ? pct(m.totalReturnPct) : '—'} tone={m && m.totalReturnPct >= 0 ? 'pos' : 'neg'} />
            <MetricCard label="期末资金" value={m ? money(m.finalCash) : '—'} hint={m ? `初始 ${money(m.initialCash)}` : undefined} />
            <MetricCard label="最大回撤" value={m ? pct(m.maxDrawdownPct) : '—'} tone="neg" />
            <MetricCard label="胜率 / 交易" value={m ? `${pct(m.winRatePct)} / ${m.tradeCount}` : '—'} />
          </div>

          {activeSymbol !== 'DEMO' && <StockProfileCard symbol={activeSymbol} />}

          {bars.length > 0 && (
            <Card>
              <CardHead
                title={`${activeSymbol} 日K · 买卖点`}
                sub="点击图上标注或下方表格行聚焦该笔交易；滚轮缩放，拖动平移"
                right={
                  <span className="focus-nav">
                    {(result?.trades ?? []).length > 1 && (
                      <>
                        <button className="btn ghost small" title="上一笔交易"
                          disabled={!focus || focusTradeIdx <= 0}
                          onClick={() => stepTrade(-1)}>‹</button>
                        <span className="muted focus-idx">{focus ? `${focusTradeIdx + 1} / ${(result?.trades ?? []).length}` : `共 ${(result?.trades ?? []).length} 笔`}</span>
                        <button className="btn ghost small" title="下一笔交易"
                          disabled={!focus || focusTradeIdx >= (result?.trades ?? []).length - 1}
                          onClick={() => stepTrade(1)}>›</button>
                      </>
                    )}
                    {focus && (
                      <span className="focus-chip">
                        <em>{focus.label}</em>
                        <button className="btn ghost small" onClick={() => setFocus(null)}>返回全景</button>
                      </span>
                    )}
                  </span>
                }
              />
              {focus ? (
                <div className="focus-detail">
                  {focus.trade ? (
                    <>
                      <span>买 <b className="pos">{focus.trade.buyPrice.toFixed(2)}</b>（{focus.trade.buyDate}）</span>
                      <span>卖 <b className={focus.trade.returnPct >= 0 ? 'pos' : 'neg'}>{focus.trade.sellPrice.toFixed(2)}</b>（{focus.trade.sellDate}）</span>
                      <span className={focus.trade.returnPct >= 0 ? 'pos' : 'neg'}>收益 {focus.trade.returnPct >= 0 ? '+' : ''}{focus.trade.returnPct.toFixed(2)}%</span>
                      <span>{exitReasonText[focus.trade.exitReason] ?? focus.trade.exitReason}</span>
                    </>
                  ) : focus.signal ? (
                    <>
                      <span>突破 <b>{focus.signal.breakoutPrice.toFixed(2)}</b></span>
                      <span>量比 {focus.signal.volumeRatio.toFixed(1)}×</span>
                      <span>段内涨 {focus.signal.risePct.toFixed(1)}%</span>
                      <span>回调 {focus.signal.pullbackPct.toFixed(1)}%</span>
                      <span className={focus.signal.dayChangePct >= 0 ? 'pos' : 'neg'}>当日 {focus.signal.dayChangePct >= 0 ? '+' : ''}{focus.signal.dayChangePct.toFixed(2)}%</span>
                    </>
                  ) : null}
                </div>
              ) : null}
              <KlineChart bars={bars} signals={result?.signals ?? []} trades={result?.trades ?? []}
                focus={focus ? { from: focus.from, to: focus.to } : null}
                onMarkClick={handleMarkClick} onResetFocus={() => setFocus(null)} />
            </Card>
          )}

          <Card>
            <CardHead
              title="收盘价与策略信号"
              sub="金色圆点为 N 字信号，绿色三角为次日开盘模拟买入"
              right={<span className="legend-dot">● 信号 · ▲ 买入</span>}
            />
            <PriceChart bars={bars} signals={result?.signals ?? []} trades={result?.trades ?? []} />
          </Card>

          <Card>
            <CardHead title="N 字候选信号" right={<span className="count-pill">{(result?.signals ?? []).length} 个</span>} />
            {(result?.signals ?? []).length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>信号日期</th><th className="num">突破价</th><th className="num">涨幅</th><th className="num">当日涨跌</th>
                      <th className="num">回撤</th><th className="num">量比</th><th>说明</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(result?.signals ?? []).map(s => (
                      <tr key={s.date} className="row-clickable" title="点击在K线图中聚焦这个信号"
                          onClick={() => focusAt(s.date, undefined, `${s.date} 信号`, { signal: s })}>
                        <td>{s.date}</td>
                        <td className="num">{fmt(s.breakoutPrice)}</td>
                        <td className="num pos">{pct(s.risePct)}</td>
                        <td className={`num ${s.dayChangePct >= 0 ? 'pos' : 'neg'}`}>{s.dayChangePct >= 0 ? '+' : ''}{pct(s.dayChangePct)}</td>
                        <td className="num">{pct(s.pullbackPct)}</td>
                        <td className="num">{fmt(s.volumeRatio)}×</td>
                        <td>{s.reason}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <>
                <Empty text="当前时间范围内未发现 N 字信号" />
                {!allHistory && (
                  <div className="empty-action">
                    <button className="btn ghost small" onClick={() => setAllHistory(true)}>
                      切到全部历史查看
                    </button>
                  </div>
                )}
              </>
            )}
          </Card>

          <Card>
            <CardHead title="模拟交易" right={<span className="count-pill">{(result?.trades ?? []).length} 笔</span>} />
            {(result?.trades ?? []).length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>买入日</th><th>卖出日</th><th className="num">买入价</th>
                      <th className="num">卖出价</th><th className="num">收益</th><th>退出原因</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(result?.trades ?? []).map(t => (
                      <tr key={t.buyDate} className="row-clickable" title="点击在K线图中聚焦这笔交易"
                          onClick={() => focusAt(t.buyDate, t.sellDate, `${t.buyDate} 买入`, { trade: t })}>
                        <td>{t.buyDate}</td>
                        <td>{t.sellDate}</td>
                        <td className="num">{fmt(t.buyPrice)}</td>
                        <td className="num">{fmt(t.sellPrice)}</td>
                        <td className={`num ${t.returnPct >= 0 ? 'pos' : 'neg'}`}>{pct(t.returnPct)}</td>
                        <td>{exitReasonText[t.exitReason] ?? t.exitReason}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <>
                <Empty text="尚未产生模拟交易" />
                {!allHistory && (
                  <div className="empty-action">
                    <button className="btn ghost small" onClick={() => setAllHistory(true)}>
                      切到全部历史查看
                    </button>
                  </div>
                )}
              </>
            )}
          </Card>
        </div>

        <aside className="params-panel">
          <Card className="sticky">
            <CardHead title="策略参数" sub="修改后点击「运行回测」生效" />
            <h3 className="group-title">信号识别</h3>
            {signalFields.map(f => (
              <label key={f.key} className="param-field">
                <span>{f.label}</span>
                <span className="param-input">
                  <input
                    type="number"
                    min={f.min}
                    step={f.step ?? 1}
                    value={params[f.key] > 0 ? params[f.key] : ''}
                    onChange={e => setParams({ ...params, [f.key]: Number(e.target.value) })}
                  />
                  <em>{f.unit}</em>
                </span>
              </label>
            ))}
            <h3 className="group-title">交易规则</h3>
            {tradeFields.map(f => (
              <label key={f.key} className="param-field">
                <span>{f.label}</span>
                <span className="param-input">
                  <input
                    type="number"
                    min={f.min}
                    step={f.step ?? 1}
                    value={params[f.key] > 0 ? params[f.key] : ''}
                    onChange={e => setParams({ ...params, [f.key]: Number(e.target.value) })}
                  />
                  <em>{f.unit}</em>
                </span>
              </label>
            ))}
            <h3 className="group-title">回测范围</h3>
            <label className="param-field">
              <span>时间范围</span>
              <span className="param-input">
                <input
                  type="number"
                  min={1}
                  step={1}
                  value={allHistory || !windowDays ? '' : windowDays}
                  disabled={allHistory}
                  placeholder="10"
                  onChange={e => setWindowDays(Number(e.target.value))}
                />
                <em>交易日</em>
              </span>
            </label>
            <label className="param-field">
              <span>全部历史</span>
              <span className="param-input">
                <input
                  type="checkbox"
                  className="param-check"
                  checked={allHistory}
                  onChange={e => setAllHistory(e.target.checked)}
                />
                <em>{allHistory ? '全部历史' : `近 ${Math.max(1, windowDays || 1)} 日`}</em>
              </span>
            </label>
            <button className="btn primary block" onClick={() => run(symbolInput, params)} disabled={disabled}>
              {running ? '回测中…' : '运行回测'}
            </button>
            <p className="param-hint">信号收盘后生成，按下一交易日开盘价模拟成交，规避未来函数。</p>
          </Card>
        </aside>
      </div>
    </div>
  )
}
