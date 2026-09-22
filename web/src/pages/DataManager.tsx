import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api, type Candle, type StockSummary, type SyncState } from '../api'
import { Banner, Card, CardHead, Empty, Spinner } from '../components/ui'

export default function DataManager() {
  const [stocks, setStocks] = useState<StockSummary[] | null>(null)
  const [syncState, setSyncState] = useState<SyncState | null>(null)
  const [syncSymbol, setSyncSymbol] = useState('')
  const [importSymbol, setImportSymbol] = useState('')
  const [importJson, setImportJson] = useState('')
  const [busy, setBusy] = useState(false)
  const [feedback, setFeedback] = useState<{ text: string; kind: 'info' | 'error' | 'success' }>({ text: '', kind: 'info' })

  const refresh = useCallback(async () => {
    try {
      setStocks(await api.stocks())
    } catch (e) {
      setFeedback({ text: e instanceof Error ? e.message : '加载股票列表失败', kind: 'error' })
    }
    api.syncState().then(setSyncState).catch(() => {})
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const handleSync = async () => {
    const symbol = syncSymbol.trim().toUpperCase()
    if (!symbol || symbol === 'DEMO') {
      setFeedback({ text: '请输入真实股票代码，例如 600519.SH。', kind: 'error' })
      return
    }
    setBusy(true)
    setFeedback({ text: `正在拉取 ${symbol} 前复权日线（全量）…`, kind: 'info' })
    try {
      const r = await api.sync(symbol)
      setFeedback({ text: `同步完成：${symbol} 共 ${r.count} 根日线（${r.firstDate} ~ ${r.lastDate}）。`, kind: 'success' })
      setSyncSymbol('')
      await refresh()
    } catch (e) {
      setFeedback({ text: e instanceof Error ? e.message : '同步失败', kind: 'error' })
    } finally {
      setBusy(false)
    }
  }

  const handleImport = async () => {
    const symbol = importSymbol.trim().toUpperCase()
    if (!symbol) {
      setFeedback({ text: '请输入导入目标股票代码。', kind: 'error' })
      return
    }
    let bars: Candle[]
    try {
      bars = JSON.parse(importJson)
      if (!Array.isArray(bars) || !bars.length) throw new Error('empty')
    } catch {
      setFeedback({ text: '导入内容必须是非空 JSON 数组，例如 [{"date":"2025-01-02","open":10,"high":10.2,"low":9.9,"close":10,"volume":100}]', kind: 'error' })
      return
    }
    setBusy(true)
    try {
      const r = await api.importBars(symbol, bars)
      setFeedback({ text: `导入完成：${r.symbol} 共 ${r.count} 根日线。`, kind: 'success' })
      setImportSymbol('')
      setImportJson('')
      await refresh()
    } catch (e) {
      setFeedback({ text: e instanceof Error ? e.message : '导入失败', kind: 'error' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>数据管理</h1>
          <p>同步通达信前复权日线，或导入自定义行情数据</p>
          {syncState && syncState.lastTradingDay && (
            <p className="sync-state-line">
              每日增量更新至 <b>{syncState.lastTradingDay}</b>
              <span>（{syncState.lastRun} · 追加 {syncState.barsAppended} 根 · 除权重拉 {syncState.fullRefetch} 只）</span>
            </p>
          )}
        </div>
        <button className="btn ghost" onClick={() => void refresh()}>刷新列表</button>
      </header>

      <Banner text={feedback.text} kind={feedback.kind} />

      <div className="data-grid">
        <Card>
          <CardHead title="同步真实数据" sub="通过通达信行情协议拉取沪深股票全量前复权日K" />
          <div className="inline-form">
            <input
              value={syncSymbol}
              onChange={e => setSyncSymbol(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && !busy && handleSync()}
              placeholder="600519.SH / 000001.SZ"
              spellCheck={false}
            />
            <button className="btn primary" onClick={handleSync} disabled={busy}>
              {busy ? '处理中…' : '开始同步'}
            </button>
          </div>
          <p className="hint">前复权数据会随分红除权整体重算，每次同步全量替换本地序列。</p>
        </Card>

        <Card>
          <CardHead title="导入日线 JSON" sub="按日期升序；服务端也会自动排序" />
          <input
            className="block-input"
            value={importSymbol}
            onChange={e => setImportSymbol(e.target.value)}
            placeholder="目标股票代码，例如 600519.SH"
            spellCheck={false}
          />
          <textarea
            value={importJson}
            onChange={e => setImportJson(e.target.value)}
            placeholder='[{"date":"2025-01-02","open":10,"high":10.2,"low":9.9,"close":10,"volume":100}]'
            rows={5}
            spellCheck={false}
          />
          <button className="btn primary block" onClick={handleImport} disabled={busy}>
            {busy ? '处理中…' : '导入数据'}
          </button>
        </Card>
      </div>

      <Card>
        <CardHead title="本地股票池" sub={stocks ? `共 ${stocks.length} 个标的` : undefined} />
        {stocks === null ? (
          <Spinner />
        ) : stocks.length ? (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>代码</th><th className="num">K线数</th><th>开始日期</th><th>最新日期</th><th>来源</th><th />
                </tr>
              </thead>
              <tbody>
                {stocks.map(s => (
                  <tr key={s.symbol}>
                    <td className="symbol-cell">{s.symbol}</td>
                    <td className="num">{s.count}</td>
                    <td>{s.firstDate}</td>
                    <td>{s.lastDate}</td>
                    <td>
                      <span className={`source-pill ${s.symbol === 'DEMO' ? 'demo' : 'real'}`}>
                        {s.symbol === 'DEMO' ? '内置演示' : '本地数据'}
                      </span>
                    </td>
                    <td className="num">
                      <Link className="table-link" to={`/?symbol=${encodeURIComponent(s.symbol)}`}>查看回测 →</Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <Empty text="暂无本地数据" />
        )}
      </Card>
    </div>
  )
}
