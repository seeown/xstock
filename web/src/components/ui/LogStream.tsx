import { useEffect, useRef } from 'react'

export type LogLevel = 'i' | 'w' | 'e'
export interface LogLine { level?: LogLevel; text: string }

// 自动滚动并保留最近 500 行（样张库契约）
const CAP = 500

export function LogStream({ lines }: { lines: Array<LogLine | string> }) {
  const ref = useRef<HTMLDivElement>(null)
  const normalized = lines.slice(-CAP).map(l => (typeof l === 'string' ? { level: 'i' as const, text: l } : { level: l.level ?? 'i', text: l.text }))

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60
    if (nearBottom) el.scrollTop = el.scrollHeight
  }, [lines])

  return (
    <div className="log" ref={ref}>
      {normalized.map((l, i) => (
        <div key={i}>
          <span className={l.level}>{l.text}</span>
        </div>
      ))}
    </div>
  )
}
