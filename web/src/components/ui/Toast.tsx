import { useCallback, useEffect, useRef, useState } from 'react'

export interface ToastItem { id: number; title: string; desc?: string }

let toastSeq = 1

// Toast 右上 5s 自动消失，可手动关闭（样张库契约）
export function useToasts(timeoutMs = 5000) {
  const [toasts, setToasts] = useState<ToastItem[]>([])
  const timers = useRef(new Map<number, ReturnType<typeof setTimeout>>())

  const dismiss = useCallback((id: number) => {
    setToasts(ts => ts.filter(t => t.id !== id))
    const timer = timers.current.get(id)
    if (timer) { clearTimeout(timer); timers.current.delete(id) }
  }, [])

  const push = useCallback((title: string, desc?: string) => {
    const id = toastSeq++
    setToasts(ts => [...ts, { id, title, desc }])
    timers.current.set(id, setTimeout(() => dismiss(id), timeoutMs))
  }, [dismiss, timeoutMs])

  useEffect(() => () => { timers.current.forEach(clearTimeout); timers.current.clear() }, [])

  return { toasts, push, dismiss }
}

export function ToastStack({ toasts, onClose }: { toasts: ToastItem[]; onClose: (id: number) => void }) {
  return (
    <>
      {toasts.map(t => (
        <div key={t.id} className="toast" role="status">
          <svg className="ic" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
            <circle cx="12" cy="12" r="9" />
            <path d="M12 7.5v5.5" />
            <path d="M12 16.5h.01" />
          </svg>
          <div>
            <div className="tt">{t.title}</div>
            {t.desc && <div className="td">{t.desc}</div>}
          </div>
          <button className="x" onClick={() => onClose(t.id)} aria-label="关闭">×</button>
        </div>
      ))}
    </>
  )
}
