import { useCallback, useEffect, useMemo, useRef, useState, type MouseEvent as ReactMouseEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, type NewsItem, type OvernightCell } from '../api'
import { KlineDetailModal, KlinePopover, useKlinePreview } from '../components/klinePreview'
import { Button, Card, Skeleton, ToastStack, useToasts } from '../components/ui'
import { useIsActive } from '../shell'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'

// 盘前资讯（一期）：双源快讯时间轴 + 隔夜行情带 + 聚焦 Top3 + 开盘倒计时。
// 滚动更新 = 30 秒增量轮询（since= 最后一条时间），新条目带 fresh 光效，
// 重磅新条目弹 toast。设计稿契约：pre- 前缀组件 / tl-item[data-cat] 筛选 /
// j·k·Enter·r 快捷键 / 骨架屏 / 空态说明。

type Filter = 'all' | 'hot' | 'watch' | 'oversea' | 'ann'

const filterTabs: Array<{ key: Filter; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'hot', label: '加红 / 重要' },
  { key: 'watch', label: '自选池命中' },
  { key: 'oversea', label: '海外' },
  { key: 'ann', label: '公告' },
]

const srcName: Record<string, string> = { em: '东财快讯', sina: '新浪 7×24' }

// 时段分组：今日 / 昨夜（15:00 之后到今日开盘前为"隔夜"语境）。
function groupOf(t: string, now: Date): string {
  const d = t.slice(0, 10)
  const today = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`
  if (d === today) return `今日 · ${d.slice(5)}`
  return `更早 · ${d.slice(5)}`
}

// 倒计时：目标 = 最近的集合竞价 09:15（今日未到用今日，否则下一工作日）。
function nextAuction(now: Date): Date {
  const t = new Date(now)
  t.setHours(9, 15, 0, 0)
  if (t.getTime() <= now.getTime()) {
    t.setDate(t.getDate() + 1)
    while (t.getDay() === 0 || t.getDay() === 6) t.setDate(t.getDate() + 1)
  }
  return t
}

function sessionPhase(now: Date): string {
  const hm = now.getHours() * 100 + now.getMinutes()
  const wd = now.getDay()
  if (wd === 0 || wd === 6) return '休市'
  if (hm < 915) return '盘前集结中'
  if (hm < 925) return '集合竞价申报'
  if (hm < 930) return '竞价定格 · 待开盘'
  if (hm <= 1500) return '盘中'
  return '已收盘'
}

function useCountdown() {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    const t = window.setInterval(() => setNow(new Date()), 1000)
    return () => window.clearInterval(t)
  }, [])
  const target = nextAuction(now)
  const diff = Math.max(0, target.getTime() - now.getTime())
  const pad = (n: number) => String(n).padStart(2, '0')
  return {
    hh: pad(Math.floor(diff / 3_600_000)),
    mm: pad(Math.floor((diff % 3_600_000) / 60_000)),
    ss: pad(Math.floor((diff % 60_000) / 1000)),
    target,
    now,
  }
}

export default function News() {
  const navigate = useNavigate()
  const isActiveView = useIsActive()
  const kline = useKlinePreview()
  const { toasts, push: pushToast, dismiss } = useToasts()

  const [items, setItems] = useState<NewsItem[] | null>(null)
  const [err, setErr] = useState('')
  const [overnight, setOvernight] = useState<OvernightCell[] | null>(null)
  const [filter, setFilter] = useState<Filter>('all')
  const [freshIDs, setFreshIDs] = useState<Set<string>>(new Set())
  const lastTimeRef = useRef<string>('')
  const [sel, setSel] = useState(-1)
  const [refreshing, setRefreshing] = useState(false)

  const mergeItems = useCallback((incoming: NewsItem[]) => {
    setItems(prev => {
      if (!prev) return incoming
      const byID = new Map(prev.map(i => [i.id, i]))
      for (const it of incoming) byID.set(it.id, it)
      const merged = [...byID.values()].sort((a, b) => b.time.localeCompare(a.time)).slice(0, 200)
      return merged
    })
    if (incoming.length) {
      setFreshIDs(new Set(incoming.map(i => i.id)))
      const hot = incoming.find(i => i.hot)
      if (hot && lastTimeRef.current) {
        pushToast(`新快讯 · 评分 ${hot.score}`, hot.title)
      }
      lastTimeRef.current = incoming[0].time
    }
  }, [pushToast])

  const load = useCallback((initial: boolean) => {
    const since = initial || !lastTimeRef.current ? '' : `&since=${encodeURIComponent(lastTimeRef.current)}`
    api.news(since)
      .then(r => {
        mergeItems(r.items)
        setErr('')
      })
      .catch(e => setErr(e instanceof Error ? e.message : '快讯加载失败'))
  }, [mergeItems])

  // 激活时首次全量 + 30 秒增量轮询（保活壳门控：切走即停）。
  useEffect(() => {
    if (!isActiveView) return
    load(true)
    api.overnight().then(setOvernight).catch(() => setOvernight([]))
    const t = window.setInterval(() => load(false), 30_000)
    return () => window.clearInterval(t)
  }, [isActiveView, load])

  // fresh 光效 8 秒后褪去
  useEffect(() => {
    if (!freshIDs.size) return
    const t = window.setTimeout(() => setFreshIDs(new Set()), 8000)
    return () => window.clearTimeout(t)
  }, [freshIDs])

  const filtered = useMemo(() => {
    if (!items) return []
    return items.filter(it => {
      if (filter === 'all') return true
      if (filter === 'hot') return it.hot
      if (filter === 'watch') return it.symbols?.some(s => s.watch)
      return it.categories?.includes(filter)
    })
  }, [items, filter])

  const top3 = useMemo(() => {
    if (!items) return []
    return [...items].sort((a, b) => b.score - a.score).slice(0, 3)
  }, [items])

  const counts = useMemo(() => {
    const c: Record<Filter, number> = { all: items?.length ?? 0, hot: 0, watch: 0, oversea: 0, ann: 0 }
    for (const it of items ?? []) {
      if (it.hot) c.hot++
      if (it.symbols?.some(s => s.watch)) c.watch++
      if (it.categories?.includes('oversea')) c.oversea++
      if (it.categories?.includes('ann')) c.ann++
    }
    return c
  }, [items])

  const { hh, mm, ss, target, now } = useCountdown()
  const phase = sessionPhase(now)

  const openStock = (symbol: string, name: string) => kline.openDetail({ symbol, name })

  useKeyboardShortcuts({
    j: () => setSel(i => Math.min(filtered.length - 1, i + 1)),
    k: () => setSel(i => Math.max(0, i - 1)),
    Enter: () => {
      const it = filtered[sel]
      const s = it?.symbols?.[0]
      if (s) openStock(s.symbol, s.name)
      else if (it) window.open(`https://so.eastmoney.com/news/s?keyword=${encodeURIComponent(it.title.slice(0, 20))}`, '_blank')
    },
    r: () => { setRefreshing(true); load(true); window.setTimeout(() => setRefreshing(false), 800) },
  })

  useEffect(() => {
    const el = document.querySelector<HTMLElement>('.tl-item.sel .tl-card')
    el?.scrollIntoView({ block: 'nearest' })
  }, [sel])

  const hotCls = (it: NewsItem) => `${it.hot ? ' hot' : ''}${freshIDs.has(it.id) ? ' fresh' : ''}`
  const catOf = (it: NewsItem) =>
    [it.categories?.includes('policy') ? 'policy' : '', it.categories?.includes('oversea') ? 'oversea' : '',
      it.categories?.includes('ann') ? 'ann' : '', it.symbols?.some(s => s.watch) ? 'watch' : ''].filter(Boolean).join(' ')

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>盘前资讯</h1>
          <p>公告 · 国内快讯 · 隔夜海外 —— 评分排序的双源合流（巨潮公告二期接入）</p>
        </div>
        <span className="pre-conn">
          <span className="dot" /> 双源在线 · 东财 / 新浪 · 30 秒自动滚动
        </span>
      </header>

      {err && <div className="banner err">{err}（自动重试中）</div>}

      {/* ---- 倒计时指挥条 ---- */}
      <section className="pre-hero" aria-label="开盘倒计时">
        <div>
          <div className="pre-count-label">距离集合竞价</div>
          <div className="pre-countdown">
            <span>{hh}</span><span className="sep">:</span><span>{mm}</span><span className="sep">:</span><span>{ss}</span>
          </div>
          <div className="pre-count-sub">集合竞价 09:15 · 连续竞价 09:30 · 目标 {`${target.getMonth() + 1}-${target.getDate()}`}</div>
        </div>
        <div className="pre-mid">
          <div className="pre-day">
            <span className="d">{now.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', weekday: 'long' })}</span>
            <span className="pre-phase">{phase}</span>
          </div>
          <div className="pre-calen">
            <span className="cc">盘前窗口 5 秒一轮拉取源站</span>
            <span className="cc">09:15 竞价异动页同步监测</span>
          </div>
        </div>
        <div className="pre-side">
          <span className={`pre-live${phase === '盘中' ? '' : ' pre-live-warm'}`}>
            <span className="dot" />{phase}
          </span>
          <Button variant="ghost" onClick={() => { setRefreshing(true); load(true); window.setTimeout(() => setRefreshing(false), 800) }}>
            {refreshing ? '已刷新' : '刷新（R）'}
          </Button>
        </div>
      </section>

      {/* ---- 隔夜行情带 ---- */}
      <section className="pre-mkt" aria-label="隔夜行情">
        {overnight === null
          ? Array.from({ length: 6 }).map((_, i) => <div key={i} className="mkt-cell"><Skeleton h={54} w="80%" /></div>)
          : overnight.map(c => (
            <div key={c.key} className="mkt-cell">
              <div className="nm"><span>{c.name}</span><span className="flag">{c.flag}</span></div>
              <div className="px">{c.price >= 10000 ? c.price.toLocaleString('en-US', { maximumFractionDigits: 2 }) : c.price.toFixed(2)}</div>
              <div className={`chg ${c.chgPct >= 0 ? 'up' : 'down'}`}>{c.chgPct >= 0 ? '+' : ''}{c.chgPct.toFixed(2)}%</div>
              <div className="mkt-time">{c.time}</div>
            </div>
          ))}
      </section>

      {/* ---- 聚焦要闻 Top3 ---- */}
      <section aria-label="今日要闻聚焦" style={{ marginBottom: 4 }}>
        <div className="block-head">
          <h2><span className="k">TOP 3</span>今日要闻聚焦 · 按评分</h2>
          <span className="src-from">规则评分 = 加红/关键词 + 自选命中 + 时效</span>
        </div>
        <div className="pre-focus">
          {top3.length === 0 && (items === null
            ? <Card className="skel-card"><Skeleton h={90} count={3} /></Card>
            : <div className="chart-empty">暂无快讯（数据源可能在限流，稍候自动重试）</div>)}
          {top3.map((it, i) => (
            <article key={it.id} className={`focus-card${i === 0 ? ' lead' : ''}`}>
              <div className="focus-meta">
                <span className={`src-badge ${it.categories?.[0] === 'oversea' ? 'oversea' : it.categories?.[0] === 'ann' ? 'ann' : 'policy'}`}>
                  {it.categories?.[0] === 'oversea' ? '海外' : it.categories?.[0] === 'ann' ? '公告' : '要闻'}
                </span>
                <span className="src-from">{srcName[it.source]} · {it.time.slice(11, 16)}</span>
                {it.hot && <span className="hot-mark">加红</span>}
                <span className="rps"><span className="bar"><i style={{ width: `${it.score}%` }} /></span><b>{it.score}</b></span>
              </div>
              <h3 className="focus-title">{it.title}</h3>
              {it.summary && <p className="focus-body">{it.summary}</p>}
              <div className="focus-foot">
                <div className="focus-stocks">
                  {it.symbols?.map(s => (
                    <button key={s.symbol} className="tag-stock" onClick={() => openStock(s.symbol, s.name)}>
                      <span className="wp" />{s.name}{s.watch ? ' · 自选' : ''}
                    </button>
                  ))}
                </div>
                {it.symbols?.some(s => s.watch) && <span className="src-from">自选池命中</span>}
              </div>
            </article>
          ))}
        </div>
      </section>

      {/* ---- 双栏主体：时间轴 + 右栏 ---- */}
      <div className="pre-cols">
        <section aria-label="快讯时间轴">
          <div className="block-head" style={{ marginBottom: 10 }}>
            <h2><span className="k">FEED</span>快讯时间轴</h2>
            <span className="src-from">共 {filtered.length} 条{filter !== 'all' ? `（筛选自 ${items?.length ?? 0} 条）` : ''}</span>
          </div>
          <div className="fbar">
            {filterTabs.map(t => (
              <button key={t.key} className={`chip${filter === t.key ? ' on' : ''}`} onClick={() => { setFilter(t.key); setSel(-1) }}>
                {t.label}<span className="cnt">{counts[t.key]}</span>
              </button>
            ))}
          </div>

          {items === null ? (
            <Card className="skel-card"><div style={{ display: 'grid', gap: 12 }}><Skeleton h={44} count={8} /></div></Card>
          ) : filtered.length === 0 ? (
            <div className="chart-empty">
              {filter === 'hot' ? '暂无加红快讯，今日风平浪静' : filter === 'watch' ? '自选池暂无命中——去个股行情加 ☆ 自选，命中即在此置顶提示' : '暂无快讯'}
            </div>
          ) : (
            <div className="tl-wrap">
              {filtered.map((it, i) => {
                const g = groupOf(it.time, now)
                const prevG = i > 0 ? groupOf(filtered[i - 1].time, now) : ''
                return (
                  <div key={it.id}>
                    {g !== prevG && <div className="tl-group">{g}</div>}
                    <div
                      className={`tl-item${hotCls(it)}${i === sel ? ' sel' : ''}`}
                      data-cat={catOf(it)}
                      onMouseEnter={e => it.symbols?.[0] && kline.enterRow({ symbol: it.symbols[0].symbol, name: it.symbols[0].name }, e.currentTarget)}
                      onMouseLeave={kline.leaveRow}
                      onClick={() => setSel(i === sel ? -1 : i)}
                    >
                      <div className="tl-time">{it.time.slice(11, 16)}</div>
                      <div className="tl-node" />
                      <div className="tl-card">
                        <div className="tl-meta">
                          <span className={`src-badge ${it.categories?.[0] === 'oversea' ? 'oversea' : it.categories?.[0] === 'ann' ? 'ann' : 'policy'}`}>
                            {it.categories?.[0] === 'oversea' ? '海外' : it.categories?.[0] === 'ann' ? '公告' : it.categories?.[0] === 'policy' ? '政策' : '市场'}
                          </span>
                          <span className="src-from">{srcName[it.source]}{it.hot ? '' : ''}</span>
                          {it.hot && <span className="hot-mark">加红</span>}
                        </div>
                        <div className="tl-title">{it.title}</div>
                        {it.summary && <div className="tl-body">{it.summary}</div>}
                        <div className="tl-foot">
                          <span className="rps"><span className="bar"><i style={{ width: `${it.score}%` }} /></span><b>{it.score}</b></span>
                          {it.symbols?.map(s => (
                            <button
                              key={s.symbol}
                              className={`tag-stock${s.watch ? ' watch-hit' : ''}`}
                              onClick={(e: ReactMouseEvent) => { e.stopPropagation(); openStock(s.symbol, s.name) }}
                            >
                              <span className="wp" />{s.name}{s.watch ? '·自选' : ''}
                            </button>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </section>

        {/* 右栏：公告（二期）+ 日历 */}
        <div>
          <section className="side-card" aria-label="重要公告">
            <div className="block-head">
              <h2><span className="k">ANN</span>重要公告</h2>
            </div>
            <div className="chart-empty" style={{ padding: '14px 6px' }}>
              二期接入巨潮全量公告（晚间 30 分钟一扫 · 评分排序）<br />
              <span className="src-from">当前快讯流已含东财/新浪转发的重点公告</span>
            </div>
          </section>
          <section className="side-card" aria-label="盘前节奏">
            <div className="block-head">
              <h2><span className="k">CAL</span>盘前节奏</h2>
            </div>
            <div className="cal-item"><span className="cal-time">09:15</span><div><div className="cal-title">集合竞价开始</div><div className="cal-sub">竞价异动页同步监测梯队承接</div></div></div>
            <div className="cal-item"><span className="cal-time">09:30</span><div><div className="cal-title">连续竞价开盘</div><div className="cal-sub">分时图分钟级滚动</div></div></div>
            <div className="cal-item night"><span className="cal-time">15:05</span><div><div className="cal-title">日线落库 + 情绪统计重算</div><div className="cal-sub">收盘定格 · 形态可归档</div></div></div>
            <div className="cal-item night"><span className="cal-time">21:30</span><div><div className="cal-title">美股开盘（冬令时 22:30）</div><div className="cal-sub">隔夜行情带次日更新</div></div></div>
          </section>
        </div>
      </div>

      <ToastStack toasts={toasts} onClose={dismiss} />
      <KlinePopover state={kline.hover} />
      <KlineDetailModal state={kline.detail} onClose={kline.closeDetail} onOpenBacktest={symbol => navigate(`/?symbol=${symbol}`)} />
    </div>
  )
}
