import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type ConceptCount, type ProfileListItem, type ProfilePageResult, type Quote, type StockProfile } from '../api'
import { Banner, Card, CardHead, fmt, pct } from '../components/ui'

const BOARDS = ['上证主板', '深证主板', '创业板', '科创板', '北交所']

// Numeric ranking columns start descending; text columns start ascending.
const NUMERIC_SORTS = new Set(['price', 'changePct', 'amount', 'concepts', 'bars'])

const COLUMNS: Array<{ key: string; label: string; numeric?: boolean }> = [
  { key: 'symbol', label: '代码' },
  { key: 'name', label: '名称' },
  { key: 'board', label: '板块' },
  { key: 'industry', label: '行业' },
  { key: 'price', label: '最新价', numeric: true },
  { key: 'changePct', label: '涨跌幅', numeric: true },
  { key: 'amount', label: '成交额', numeric: true },
  { key: 'listDate', label: '上市日期' },
  { key: 'concepts', label: '概念', numeric: true },
  { key: 'bars', label: 'K线', numeric: true },
]

function fmtAmount(v: number): string {
  if (!v) return '—'
  if (v >= 1e8) return `${(v / 1e8).toFixed(1)}亿`
  if (v >= 1e4) return `${(v / 1e4).toFixed(0)}万`
  return v.toFixed(0)
}

interface Filters {
  q: string
  board: string
  industry: string
  concept: string
  syncedOnly: boolean
}

const EMPTY_FILTERS: Filters = { q: '', board: '', industry: '', concept: '', syncedOnly: false }

export default function Stocks() {
  const navigate = useNavigate()
  const [filters, setFilters] = useState<Filters>(EMPTY_FILTERS)
  const [appliedQ, setAppliedQ] = useState('')
  const [sortKey, setSortKey] = useState('symbol')
  const [sortOrder, setSortOrder] = useState<'asc' | 'desc'>('asc')
  const [page, setPage] = useState(1)
  const [result, setResult] = useState<ProfilePageResult>({ items: [], total: 0, page: 1, pageSize: 50 })
  const [loading, setLoading] = useState(true)
  const [industries, setIndustries] = useState<string[]>([])
  const [concepts, setConcepts] = useState<ConceptCount[]>([])
  const [quotes, setQuotes] = useState<Record<string, Quote>>({})
  const [quotesNote, setQuotesNote] = useState('')
  const [syncing, setSyncing] = useState<Record<string, boolean>>({})
  const [drawerItem, setDrawerItem] = useState<ProfileListItem | null>(null)
  const [drawerProfile, setDrawerProfile] = useState<StockProfile | null>(null)
  const [feedback, setFeedback] = useState<{ text: string; kind: 'info' | 'error' | 'success' }>({ text: '', kind: 'info' })
  const loadSeq = useRef(0)

  // debounce the free-text search
  useEffect(() => {
    const t = setTimeout(() => {
      setAppliedQ(filters.q.trim())
      setPage(1)
    }, 300)
    return () => clearTimeout(t)
  }, [filters.q])

  useEffect(() => {
    api.industries().then(setIndustries).catch(() => {})
    api.concepts().then(setConcepts).catch(() => {})
  }, [])

  const load = useCallback(async () => {
    const seq = ++loadSeq.current
    setLoading(true)
    try {
      const r = await api.profiles({
        q: appliedQ, board: filters.board, industry: filters.industry,
        concept: filters.concept, synced: filters.syncedOnly, page,
        sort: sortKey, order: sortOrder,
      })
      if (seq === loadSeq.current) setResult(r)
    } catch (e) {
      if (seq === loadSeq.current) setFeedback({ text: e instanceof Error ? e.message : '加载失败', kind: 'error' })
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [appliedQ, filters.board, filters.industry, filters.concept, filters.syncedOnly, page, sortKey, sortOrder])

  useEffect(() => {
    load()
  }, [load])

  // latest quotes for the visible page, best effort
  useEffect(() => {
    if (!result.items.length) return
    let cancelled = false
    const symbols = result.items.map(i => i.symbol)
    setQuotesNote('行情加载中…')
    api.quotes(symbols)
      .then(qs => {
        if (cancelled) return
        const map: Record<string, Quote> = {}
        for (const q of qs) map[q.symbol] = q
        setQuotes(map)
        setQuotesNote('')
      })
      .catch(() => { if (!cancelled) setQuotesNote('行情暂不可用') })
    return () => { cancelled = true }
  }, [result.items])

  const patchFilters = (patch: Partial<Filters>) => {
    setFilters(prev => ({ ...prev, ...patch }))
    setPage(1)
  }

  const toggleSort = (key: string) => {
    setPage(1)
    if (sortKey === key) {
      setSortOrder(prev => (prev === 'desc' ? 'asc' : 'desc'))
      return
    }
    setSortKey(key)
    setSortOrder(NUMERIC_SORTS.has(key) ? 'desc' : 'asc')
  }

  const openDrawer = async (item: ProfileListItem) => {
    setDrawerItem(item)
    setDrawerProfile(null)
    try {
      setDrawerProfile(await api.profile(item.symbol))
    } catch {
      setDrawerProfile(null)
    }
  }

  const handleSync = async (symbol: string) => {
    setSyncing(prev => ({ ...prev, [symbol]: true }))
    setFeedback({ text: `正在拉取 ${symbol} 前复权日线…`, kind: 'info' })
    try {
      const r = await api.sync(symbol)
      setResult(prev => ({
        ...prev,
        items: prev.items.map(i => i.symbol === symbol ? { ...i, barCount: r.count } : i),
      }))
      setDrawerItem(prev => prev && prev.symbol === symbol ? { ...prev, barCount: r.count } : prev)
      setFeedback({ text: `${symbol} 已同步 ${r.count} 根日线（${r.firstDate} ~ ${r.lastDate}）。`, kind: 'success' })
    } catch (e) {
      setFeedback({ text: `${symbol} 同步失败：${e instanceof Error ? e.message : '未知错误'}`, kind: 'error' })
    } finally {
      setSyncing(prev => ({ ...prev, [symbol]: false }))
    }
  }

  const totalPages = Math.max(1, Math.ceil(result.total / result.pageSize))
  const activeConceptCount = useMemo(() => concepts.find(c => c.concept === filters.concept)?.count, [concepts, filters.concept])
  const sortIndicator = (key: string) => (sortKey === key ? (sortOrder === 'desc' ? ' ▼' : ' ▲') : '')

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>个股</h1>
          <p>A股全市场浏览 · 共 {result.total} 只符合条件 · 点击列头排序</p>
        </div>
      </header>

      <Banner text={feedback.text} kind={feedback.kind} />

      <Card>
        <div className="filter-bar">
          <input
            className="filter-search"
            placeholder="搜索代码 / 名称"
            value={filters.q}
            onChange={e => patchFilters({ q: e.target.value })}
            spellCheck={false}
          />
          <select value={filters.board} onChange={e => patchFilters({ board: e.target.value })}>
            <option value="">全部板块</option>
            {BOARDS.map(b => <option key={b} value={b}>{b}</option>)}
          </select>
          <select value={filters.industry} onChange={e => patchFilters({ industry: e.target.value })}>
            <option value="">全部行业</option>
            {industries.map(i => <option key={i} value={i}>{i}</option>)}
          </select>
          <select value={filters.concept} onChange={e => patchFilters({ concept: e.target.value })}>
            <option value="">全部概念</option>
            {concepts.map(c => <option key={c.concept} value={c.concept}>{c.concept}（{c.count}）</option>)}
          </select>
          <label className="filter-check">
            <input type="checkbox" checked={filters.syncedOnly} onChange={e => patchFilters({ syncedOnly: e.target.checked })} />
            仅看已同步K线
          </label>
          {(filters.q || filters.board || filters.industry || filters.concept || filters.syncedOnly) && (
            <button className="btn ghost small" onClick={() => { setFilters(EMPTY_FILTERS); setAppliedQ(''); setPage(1) }}>重置</button>
          )}
        </div>
      </Card>

      {concepts.length > 0 && (
        <details className="concept-cloud">
          <summary>
            按概念浏览{filters.concept ? `（当前：${filters.concept} · ${activeConceptCount ?? 0} 只）` : `（${concepts.length} 个概念）`}
          </summary>
          <div className="concept-cloud-body">
            {concepts.slice(0, 120).map(c => (
              <button
                key={c.concept}
                className={`concept-chip cloud-chip${filters.concept === c.concept ? ' active' : ''}`}
                onClick={() => patchFilters({ concept: filters.concept === c.concept ? '' : c.concept })}
              >
                {c.concept} <em>{c.count}</em>
              </button>
            ))}
          </div>
        </details>
      )}

      <Card>
        <CardHead
          title="股票列表"
          sub={`第 ${result.page} / ${totalPages} 页 · 每页 ${result.pageSize} 只${quotesNote ? ` · ${quotesNote}` : ''}`}
        />
        {loading ? (
          <div className="chart-empty">正在加载…</div>
        ) : (
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  {COLUMNS.map(col => (
                    <th
                      key={col.key}
                      className={`sortable${col.numeric ? ' num' : ''}${sortKey === col.key ? ' sorted' : ''}`}
                      onClick={() => toggleSort(col.key)}
                    >
                      {col.label}{sortIndicator(col.key)}
                    </th>
                  ))}
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {result.items.map(item => {
                  const q = quotes[item.symbol]
                  return (
                    <tr key={item.symbol}>
                      <td className="mono">{item.symbol}</td>
                      <td>
                        <button className="link-btn" onClick={() => openDrawer(item)}>
                          {item.name || item.symbol}
                        </button>
                      </td>
                      <td>{item.board && <span className="concept-chip board-chip">{item.board}</span>}</td>
                      <td className="muted">{item.industry || '—'}</td>
                      <td className="num">{q && q.price > 0 ? fmt(q.price) : '—'}</td>
                      <td className={`num ${q && q.price > 0 ? (q.changePct >= 0 ? 'pos' : 'neg') : ''}`}>
                        {q && q.price > 0 && q.preClose > 0 ? `${q.changePct >= 0 ? '+' : ''}${pct(q.changePct)}` : '—'}
                      </td>
                      <td className="num">{q ? fmtAmount(q.amount) : '—'}</td>
                      <td className="muted">{item.listDate || '—'}</td>
                      <td className="num" title={item.concepts.join('、')}>{item.concepts.length || '—'}</td>
                      <td>
                        {item.barCount > 0 ? (
                          <span className="muted">{item.barCount} 根</span>
                        ) : (
                          <button className="btn ghost small" onClick={() => handleSync(item.symbol)} disabled={!!syncing[item.symbol]}>
                            {syncing[item.symbol] ? '同步中…' : '同步日K'}
                          </button>
                        )}
                      </td>
                      <td>
                        <button className="link-btn" onClick={() => navigate(`/?symbol=${item.symbol}`)}>看K线</button>
                      </td>
                    </tr>
                  )
                })}
                {!result.items.length && (
                  <tr><td colSpan={11} className="table-empty">没有符合条件的股票</td></tr>
                )}
              </tbody>
            </table>
          </div>
        )}
        <div className="pagination-row">
          <button className="btn ghost small" onClick={() => setPage(p => Math.max(1, p - 1))} disabled={page <= 1 || loading}>上一页</button>
          <span className="muted">第 {result.page} / {totalPages} 页 · 共 {result.total} 只</span>
          <button className="btn ghost small" onClick={() => setPage(p => Math.min(totalPages, p + 1))} disabled={page >= totalPages || loading}>下一页</button>
        </div>
      </Card>

      {drawerItem && (
        <>
          <div className="drawer-overlay" onClick={() => setDrawerItem(null)} />
          <aside className="drawer-panel">
            <div className="drawer-head">
              <div>
                <div className="profile-name">
                  {drawerItem.name || drawerItem.symbol}
                  {drawerItem.board && <span className="concept-chip board-chip">{drawerItem.board}</span>}
                </div>
                <div className="muted mono">{drawerItem.symbol}</div>
              </div>
              <button className="btn ghost small" onClick={() => setDrawerItem(null)}>关闭</button>
            </div>
            <div className="drawer-body">
              <div className="profile-meta">
                {drawerItem.industry && <span>{drawerItem.industry}</span>}
                {drawerItem.listDate && <span>上市 {drawerItem.listDate}</span>}
                <span>{drawerItem.market}</span>
                <span>K线 {drawerItem.barCount > 0 ? `${drawerItem.barCount} 根` : '未同步'}</span>
              </div>
              {drawerItem.concepts.length > 0 && (
                <div className="concept-chips">
                  {drawerItem.concepts.map(c => <span key={c} className="concept-chip">{c}</span>)}
                </div>
              )}
              {drawerProfile === null
                ? <p className="muted">正在加载档案…</p>
                : drawerProfile.business
                  ? <pre className="drawer-business">{drawerProfile.business}</pre>
                  : <p className="muted">暂无业务描述（北交所股票暂无 F10 数据）。</p>}
              <div className="drawer-actions">
                <button className="btn primary small" onClick={() => navigate(`/?symbol=${drawerItem.symbol}`)}>看K线 · 回测</button>
                <button className="btn ghost small" onClick={() => handleSync(drawerItem.symbol)} disabled={!!syncing[drawerItem.symbol]}>
                  {syncing[drawerItem.symbol] ? '同步中…' : '同步日K'}
                </button>
              </div>
            </div>
          </aside>
        </>
      )}
    </div>
  )
}
