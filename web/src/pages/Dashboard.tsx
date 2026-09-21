import { useCallback, useEffect, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { api, type BacktestResult, type Candle, type NParams } from '../api'
import PriceChart from '../components/PriceChart'
import {
  Banner, Card, CardHead, Empty, MetricCard, Spinner, exitReasonText, fmt, money, pct,
} from '../components/ui'

const signalFields: Array<{ key: keyof NParams; label: string; unit: string; step?: number; min: number }> = [
  { key: 'riseDays', label: '上涨周期', unit: '天', min: 2 },
  { key: 'riseMinPct', label: '最小涨幅', unit: '%', step: 0.1, min: 0.1 },
  { key: 'pullbackMinDays', label: '最短回调', unit: '天', min: 1 },
  { key: 'pullbackMaxDays', label: '最长回调', unit: '天', min: 1 },
  { key: 'pullbackMaxPct', label: '最大回撤', unit: '%', step: 0.1, min: 0.1 },
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
  const initialSymbol = (searchParams.get('symbol') || 'DEMO').toUpperCase()

  const [symbolInput, setSymbolInput] = useState(initialSymbol)
  const [params, setParams] = useState<NParams | null>(null)
  const [bars, setBars] = useState<Candle[]>([])
  const [result, setResult] = useState<BacktestResult | null>(null)
  const [running, setRunning] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [feedback, setFeedback] = useState<Feedback>({ text: '', kind: 'info' })

  const run = useCallback(async (sym: string, p: NParams) => {
    const symbol = sym.trim().toUpperCase()
    if (!symbol) {
      setFeedback({ text: '请输入股票代码。', kind: 'error' })
      return
    }
    setRunning(true)
    setFeedback({ text: `正在回测 ${symbol}…`, kind: 'info' })
    try {
      const [bt, barData] = await Promise.all([api.backtest(symbol, p), api.bars(symbol)])
      setResult(bt)
      setBars(barData)
      setFeedback({ text: `回测完成：${symbol} 发现 ${bt.signals.length} 个 N 字信号。`, kind: 'success' })
    } catch (e) {
      setResult(null)
      setBars([])
      const msg = e instanceof Error ? e.message : '回测失败'
      setFeedback({
        text: msg.includes('not found') ? `${symbol} 尚无日线数据，请先到「数据管理」同步或导入。` : msg,
        kind: 'error',
      })
    } finally {
      setRunning(false)
    }
  }, [])

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
  }, [initialSymbol, run])

  const handleSync = async () => {
    const symbol = symbolInput.trim().toUpperCase()
    if (!symbol) {
      setFeedback({ text: '请输入股票代码。', kind: 'error' })
      return
    }
    if (symbol === 'DEMO') {
      setFeedback({ text: 'DEMO 是内置演示数据；请输入真实代码，例如 600519.SH。', kind: 'error' })
      return
    }
    setSyncing(true)
    setFeedback({ text: `正在从通达信拉取 ${symbol} 前复权日线…`, kind: 'info' })
    try {
      const r = await api.sync(symbol)
      setFeedback({
        text: `同步完成：${symbol} 共 ${r.count} 根日线（${r.firstDate} ~ ${r.lastDate}），正在回测…`,
        kind: 'success',
      })
      if (params) await run(symbol, params)
    } catch (e) {
      setFeedback({ text: e instanceof Error ? e.message : '同步失败', kind: 'error' })
    } finally {
      setSyncing(false)
    }
  }

  if (!params) {
    return (
      <div className="page">
        <Spinner text="正在初始化策略参数…" />
      </div>
    )
  }

  const m = result?.metrics
  const disabled = running || syncing

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>仪表盘</h1>
          <p>N 字战法筛选、回测与信号可视化</p>
        </div>
        <div className="symbol-bar">
          <input
            value={symbolInput}
            onChange={e => setSymbolInput(e.target.value)}
            onKeyDown={e => e.key === 'Enter' && !disabled && run(symbolInput, params)}
            placeholder="600519.SH"
            spellCheck={false}
          />
          <button className="btn ghost" onClick={handleSync} disabled={disabled}>
            {syncing ? '同步中…' : '同步真实数据'}
          </button>
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

          <Card>
            <CardHead
              title="收盘价与策略信号"
              sub="金色圆点为 N 字信号，绿色三角为次日开盘模拟买入"
              right={<span className="legend-dot">● 信号 · ▲ 买入</span>}
            />
            <PriceChart bars={bars} signals={result?.signals ?? []} trades={result?.trades ?? []} />
          </Card>

          <Card>
            <CardHead title="N 字候选信号" right={<span className="count-pill">{result?.signals.length ?? 0} 个</span>} />
            {result?.signals.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>信号日期</th><th className="num">突破价</th><th className="num">涨幅</th>
                      <th className="num">回撤</th><th className="num">量比</th><th>说明</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.signals.map(s => (
                      <tr key={s.date}>
                        <td>{s.date}</td>
                        <td className="num">{fmt(s.breakoutPrice)}</td>
                        <td className="num pos">{pct(s.risePct)}</td>
                        <td className="num">{pct(s.pullbackPct)}</td>
                        <td className="num">{fmt(s.volumeRatio)}×</td>
                        <td>{s.reason}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty text="当前参数下未发现 N 字信号" />
            )}
          </Card>

          <Card>
            <CardHead title="模拟交易" right={<span className="count-pill">{result?.trades.length ?? 0} 笔</span>} />
            {result?.trades.length ? (
              <div className="table-wrap">
                <table>
                  <thead>
                    <tr>
                      <th>买入日</th><th>卖出日</th><th className="num">买入价</th>
                      <th className="num">卖出价</th><th className="num">收益</th><th>退出原因</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.trades.map(t => (
                      <tr key={t.buyDate}>
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
              <Empty text="尚未产生模拟交易" />
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
                    value={params[f.key]}
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
                    value={params[f.key]}
                    onChange={e => setParams({ ...params, [f.key]: Number(e.target.value) })}
                  />
                  <em>{f.unit}</em>
                </span>
              </label>
            ))}
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
