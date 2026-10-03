import { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { api, type ScreenResult, type NPSetup } from './api'
import { fmt } from './components/ui'
import { useView } from './shell'
import { PetSpeaker, isAwake, queueSpeech, dingForPick } from './petSpeaker'

// ─── 全局雷达监控：人在任何页面都不断线 ────────────────────────
// 轮询挂在应用壳层（不随雷达页切走而停）：交易时段 60s 一轮，
// 新票三通道提示——雷达页行内闪光(页面订阅)、全局 toast、标签栏
// 标题闪烁。窗口外快照定格、扫不出新票，轮询自动静默省 CPU。

const POLL_MS = 60_000
const FLASH_MS = 8_000
const TOAST_MS = 9_000
const TITLE_TICK_MS = 1_500
const TITLE_MAX_MS = 10 * 60_000 // 闪烁最多 10 分钟，防永久打扰
const TITLE_BASE = document.title

// 信号键带 stage：同一票从 B2 演进到 B3 算新信号（与雷达页口径一致）。
export const sigKey = (s: NPSetup) => `${s.symbol}|${s.stage}|${s.keyDate}`

// 交易时段窗口：周一~周五 09:10–15:10（本地时间）。法定节假日判不了
// ——假期快照定格、diff 出不了新票，空转几轮无害。
export function inRadarWindow(now = new Date()): boolean {
  const dow = now.getDay()
  if (dow === 0 || dow === 6) return false
  const hm = now.getHours() * 100 + now.getMinutes()
  return hm >= 910 && hm <= 1510
}

interface RadarToast { id: number; text: string }

interface RadarWatch {
  result: ScreenResult | null
  running: boolean
  error: string
  days: number
  flashKeys: ReadonlySet<string>
  freshNote: string
  /** 手动/首扫/改窗口天数：重建基线（这一轮不算新票） */
  scan: (d: number) => void
}

const empty: RadarWatch = {
  result: null, running: false, error: '', days: 10,
  flashKeys: new Set(), freshNote: '', scan: () => {},
}

const Ctx = createContext<RadarWatch>(empty)
export const useRadarWatch = () => useContext(Ctx)

export function RadarWatchProvider({ children }: { children: ReactNode }) {
  const { view, switchTo } = useView()
  const [result, setResult] = useState<ScreenResult | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [days, setDays] = useState(10)
  const [flashKeys, setFlashKeys] = useState<Set<string>>(new Set())
  const [freshNote, setFreshNote] = useState('')
  const [toasts, setToasts] = useState<RadarToast[]>([])

  const baselineRef = useRef<Set<string> | null>(null)
  const busyRef = useRef(false)
  const toastSeq = useRef(0)
  const daysRef = useRef(10)
  const viewRef = useRef(view)
  viewRef.current = view

  // ---- 标签栏标题闪烁：轮换标题吸引注意，回雷达页/点 toast/超时即恢复 ----
  const flashTimer = useRef<number | null>(null)
  const flashStop = useRef<() => void>(() => {})
  const stopTitleFlash = useCallback(() => { flashStop.current() }, [])
  const startTitleFlash = (count: number) => {
    flashStop.current()
    const msg = `🔔 雷达新增 ${count} 只`
    let on = false
    flashTimer.current = window.setInterval(() => {
      on = !on
      document.title = on ? `${msg} — 快回雷达页` : TITLE_BASE
    }, TITLE_TICK_MS)
    const expire = window.setTimeout(stopTitleFlash, TITLE_MAX_MS)
    flashStop.current = () => {
      if (flashTimer.current != null) { window.clearInterval(flashTimer.current); flashTimer.current = null }
      window.clearTimeout(expire)
      document.title = TITLE_BASE
      flashStop.current = () => {}
    }
  }

  // 回到雷达视图 = 已读：停止闪烁
  useEffect(() => { if (view === 'screen') stopTitleFlash() }, [view, stopTitleFlash])

  // ---- 新票通知：toast + 标题（雷达页内只用行内闪光，不重复打扰）----
  const notify = (fresh: NPSetup[]) => {
    const names = fresh.slice(0, 3).map(f => f.name || f.symbol).join('、')
    setFreshNote(`本轮新增 ${fresh.length} 只${names ? `：${names}${fresh.length > 3 ? ' 等' : ''}` : ''}`)
    if (viewRef.current === 'screen') return
    const id = ++toastSeq.current
    setToasts(ts => [...ts.slice(-2), { id, text: `雷达新增 ${fresh.length} 只：${names}${fresh.length > 3 ? ' 等' : ''}` }])
    window.setTimeout(() => setToasts(ts => ts.filter(t => t.id !== id)), TOAST_MS)
    startTitleFlash(fresh.length)
  }

  // ---- 宠物播报（第四通道）：取现价后按阶段组文案，逐句入队 ----
  // 与 toast 不同，语音不受"在雷达页"抑制——宠物是用户主动唤醒的。
  const speakRadarPicks = async (fresh: NPSetup[]) => {
    if (!isAwake()) return
    dingForPick()
    let priceOf = new Map<string, number>()
    try {
      const qs = await api.quotes([...new Set(fresh.map(f => f.symbol))])
      priceOf = new Map(qs.filter(q => q.price > 0).map(q => [q.symbol, q.price]))
    } catch { /* 行情暂缺：播报降级为无价版 */ }
    for (const f of fresh.slice(0, 3)) {
      const nm = f.name || f.symbol
      const p = priceOf.get(f.symbol)
      const seg = p != null ? `，现价 ${fmt(p)}` : ''
      queueSpeech(
        f.stage === 'b1' ? `${nm}${seg}，回调进入黄金区，符合 B1 低吸买点`
          : f.stage === 'b2' ? `${nm}${seg}，放量突破颈线，B2 买点`
            : f.stage === 'b3' ? `${nm}${seg}，回踩颈线缩量企稳，B3 买点`
              : `${nm}${seg}，出现新信号`,
      )
    }
    if (fresh.length > 3) queueSpeech(`另有 ${fresh.length - 3} 只新信号`)
  }

  // ---- 轮询轮：diff 基线出新票；失败静默（下一轮再试）----
  const pollRound = useCallback(async () => {
    if (busyRef.current) return
    busyRef.current = true
    try {
      const res = await api.screen(daysRef.current)
      const base = baselineRef.current
      const keys = new Set(res.items.map(sigKey))
      setResult(res)
      baselineRef.current = keys
      if (base) {
        const fresh = res.items.filter(i => !base.has(sigKey(i)))
        if (fresh.length) {
          setFlashKeys(new Set(fresh.map(sigKey)))
          notify(fresh)
          void speakRadarPicks(fresh)
        } else {
          setFlashKeys(new Set())
        }
      }
    } catch { /* 静默重试 */ }
    finally { busyRef.current = false }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 手动扫描（首扫/R键/按钮/改天数/保存雷达参数后）：重建基线，不算新票。
  const scan = useCallback((d: number) => {
    daysRef.current = d
    setDays(d)
    setRunning(true); busyRef.current = true; setError('')
    api.screen(d)
      .then(res => {
        setResult(res)
        baselineRef.current = new Set(res.items.map(sigKey))
        setFlashKeys(new Set()); setFreshNote('')
      })
      .catch(e => setError(e instanceof Error ? e.message : '筛查失败'))
      .finally(() => { setRunning(false); busyRef.current = false })
  }, [])

  // 挂载即扫建基线；此后 60s 一轮，仅交易时段内执行。
  useEffect(() => {
    scan(daysRef.current)
    const t = window.setInterval(() => {
      if (!busyRef.current && inRadarWindow()) pollRound()
    }, POLL_MS)
    return () => { window.clearInterval(t); flashStop.current() }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 新票光效/提示 FLASH_MS 后褪去
  useEffect(() => {
    if (!flashKeys.size) return
    const t = window.setTimeout(() => { setFlashKeys(new Set()); setFreshNote('') }, FLASH_MS)
    return () => window.clearTimeout(t)
  }, [flashKeys])

  // 调试钩子：控制台验证全链路用（假期/收盘也能测）。
  // poke 从基线摘一个 key，下一轮真扫描会把它当"新票"走完整通知链路。
  useEffect(() => {
    const w = window as unknown as Record<string, unknown>
    w.__radarWatch = {
      pollNow: () => pollRound(),
      poke: () => {
        const b = baselineRef.current
        if (!b?.size) return null
        const k = b.values().next().value as string
        b.delete(k)
        return k
      },
      inWindow: () => inRadarWindow(),
      baseline: () => Array.from(baselineRef.current ?? []),
    }
    return () => { delete w.__radarWatch }
  }, [pollRound])

  const dismiss = (id: number) => setToasts(ts => ts.filter(t => t.id !== id))

  return (
    <Ctx.Provider value={{ result, running, error, days, flashKeys, freshNote, scan }}>
      {children}
      <PetSpeaker />
      <div className="toast-stack" role="status" aria-live="polite">
        {toasts.map(t => (
          <button
            key={t.id} className="toast static toast-radar"
            onClick={() => { dismiss(t.id); stopTitleFlash(); switchTo('screen') }}
          >
            <svg className="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
              <path d="M18 8a6 6 0 10-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
              <path d="M13.7 21a2 2 0 01-3.4 0" />
            </svg>
            <span>
              <span className="tt">{t.text}</span>
              <span className="td">点击前往雷达页</span>
            </span>
          </button>
        ))}
      </div>
    </Ctx.Provider>
  )
}
