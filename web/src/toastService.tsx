import { useEffect, useState } from 'react'

// ─── 全局 toast 服务 ─────────────────────────────────────────
// 模块级状态 + 订阅：任何模块 showToast() 一行弹通知，
// <GlobalToastStack /> 渲染一次挂在应用壳层（雷达新票 / 持仓止损
// 止盈提醒共用同一个右上通知栈，最多同时 3 条，9 秒自动消失）。

export interface ToastInput {
  text: string
  /** 副标题（如"点击前往雷达页"），点击整条回调 onClick */
  hint?: string
  onClick?: () => void
  icon?: 'bell' | 'warn'
}

interface ToastItem extends ToastInput { id: number }

const DEFAULT_MS = 9_000
let toasts: ToastItem[] = []
let seq = 0
const listeners = new Set<() => void>()
const emit = () => listeners.forEach(l => l())

export function showToast(input: ToastInput, timeoutMs = DEFAULT_MS) {
  const id = ++seq
  toasts = [...toasts.slice(-2), { ...input, id }]
  emit()
  window.setTimeout(() => {
    toasts = toasts.filter(t => t.id !== id)
    emit()
  }, timeoutMs)
}

export function dismissToast(id: number) {
  toasts = toasts.filter(t => t.id !== id)
  emit()
}

function ToastIcon({ kind }: { kind: ToastInput['icon'] }) {
  if (kind === 'warn') {
    return (
      <svg className="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
        <path d="M10.3 3.9L1.8 18a2 2 0 001.7 3h17a2 2 0 001.7-3L13.7 3.9a2 2 0 00-3.4 0z" />
        <path d="M12 9v4" />
        <path d="M12 17h.01" />
      </svg>
    )
  }
  return (
    <svg className="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
      <path d="M18 8a6 6 0 10-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 01-3.4 0" />
    </svg>
  )
}

export function GlobalToastStack() {
  const [, bump] = useState(0)
  useEffect(() => {
    const l = () => bump(n => n + 1)
    listeners.add(l)
    return () => { listeners.delete(l) }
  }, [])
  return (
    <div className="toast-stack" role="status" aria-live="polite">
      {toasts.map(t => (
        <button
          key={t.id} className="toast static toast-radar"
          onClick={() => { dismissToast(t.id); t.onClick?.() }}
        >
          <ToastIcon kind={t.icon} />
          <span>
            <span className="tt">{t.text}</span>
            {t.hint && <span className="td">{t.hint}</span>}
          </span>
        </button>
      ))}
    </div>
  )
}
