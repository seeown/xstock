import { useEffect, useRef } from 'react'
import type { ReactNode } from 'react'
import { api, type PaperPosition } from './api'
import { useView } from './shell'
import { inRadarWindow } from './radarWatch'
import { showToast } from './toastService'
import { isAwake, queueSpeech, dingForPick } from './petSpeaker'
import { fmt } from './components/ui'

// ─── 持仓止损/止盈到价监控（全局，人在任何页面都不断线）──────────
// 交易时段 60s 轮询模拟仓持仓估值：现价 ≤ 信号止损价 → 止损提醒；
// ≥ 目标价 → 止盈提醒。价位锚定买入时快照的战法信号（改参不动）。
// 同一票同一侧只提醒一次；价位回升/卖出离场后解除，允许再次触发。

const POLL_MS = 60_000

export interface PaperAlert {
  symbol: string
  name: string
  side: 'stop' | 'target'
  price: number // 现价
  line: number  // 触发的价位线
}

const alertKey = (a: Pick<PaperAlert, 'symbol' | 'side' | 'line'>) => `${a.symbol}|${a.side}|${a.line}`

// 触发判定（Paper 页徽标与全局监控共用）。
export function alertsOf(positions: PaperPosition[]): PaperAlert[] {
  const out: PaperAlert[] = []
  for (const p of positions) {
    if (!(p.lastPrice > 0)) continue
    if (p.stopLoss > 0 && p.lastPrice <= p.stopLoss) {
      out.push({ symbol: p.symbol, name: p.name || p.symbol, side: 'stop', price: p.lastPrice, line: p.stopLoss })
    } else if (p.target > 0 && p.lastPrice >= p.target) {
      out.push({ symbol: p.symbol, name: p.name || p.symbol, side: 'target', price: p.lastPrice, line: p.target })
    }
  }
  return out
}

function announce(a: PaperAlert, goPaper: () => void) {
  dingForPick()
  const nm = a.name
  if (a.side === 'stop') {
    showToast({
      icon: 'warn',
      text: `止损警报：${nm} 现价 ${fmt(a.price)} ≤ 止损 ${fmt(a.line)}`,
      hint: '点击前往模拟仓处理',
      onClick: goPaper,
    })
    queueSpeech(`${nm}，现价 ${fmt(a.price)}，已跌破止损价 ${fmt(a.line)}，注意纪律`)
  } else {
    showToast({
      text: `达到目标：${nm} 现价 ${fmt(a.price)} ≥ 目标 ${fmt(a.line)}`,
      hint: '点击前往模拟仓处理',
      onClick: goPaper,
    })
    queueSpeech(`${nm}，现价 ${fmt(a.price)}，达到目标价 ${fmt(a.line)}，考虑止盈`)
  }
}

export function PaperWatchProvider({ children }: { children: ReactNode }) {
  const { switchTo } = useView()
  // 已提醒集合：key 在集合里就不再吵，直到解除（回升/离场）后移除
  const alertedRef = useRef<Set<string> | null>(null)
  const busyRef = useRef(false)

  const round = async () => {
    if (busyRef.current) return
    busyRef.current = true
    try {
      const res = await api.paperOverview()
      const alerts = alertsOf(res.positions ?? [])
      const keys = new Set(alerts.map(alertKey))
      const alerted = alertedRef.current
      // 首轮只建基线不提醒（打开页面时已处于触发态的，页面徽标可见）
      if (alerted == null) {
        alertedRef.current = keys
        return
      }
      for (const a of alerts) {
        const k = alertKey(a)
        if (!alerted.has(k)) announce(a, () => switchTo('paper'))
      }
      alertedRef.current = keys
    } catch { /* 静默：下一轮再试 */ }
    finally { busyRef.current = false }
  }

  useEffect(() => {
    void round()
    const t = window.setInterval(() => {
      if (!busyRef.current && inRadarWindow()) void round()
    }, POLL_MS)
    return () => window.clearInterval(t)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 调试钩子：注入一次假触发走完整通知链路（假期/收盘也能测）。
  useEffect(() => {
    const w = window as unknown as Record<string, unknown>
    w.__paperWatch = {
      pollNow: () => round(),
      poke: () => {
        const nm = '测试股份', sym = 'TEST.TEST'
        announce({ symbol: sym, name: nm, side: 'stop', price: 47.2, line: 48.5 }, () => switchTo('paper'))
        return `${nm} stop`
      },
      state: () => Array.from(alertedRef.current ?? []),
    }
    return () => { delete w.__paperWatch }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return <>{children}</>
}
