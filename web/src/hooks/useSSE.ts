import { useEffect, useRef, useState } from 'react'

export type SSEStatus = 'connecting' | 'open' | 'closed'

// SSE 订阅：断连自动重连（指数退避，上限 15s），状态供顶栏连接点使用。
// 服务端任务进度 / 日志流就绪后在 tasks 等页面接入。
export function useSSE(url: string | null, onMessage: (data: string) => void) {
  const [status, setStatus] = useState<SSEStatus>('closed')
  const retry = useRef(0)
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const handler = useRef(onMessage)
  handler.current = onMessage

  useEffect(() => {
    if (!url) { setStatus('closed'); return }
    let es: EventSource | undefined
    let stopped = false

    const connect = () => {
      if (stopped) return
      setStatus('connecting')
      es = new EventSource(url)
      es.onopen = () => { retry.current = 0; setStatus('open') }
      es.onmessage = ev => handler.current(ev.data)
      es.onerror = () => {
        es?.close()
        setStatus('closed')
        if (stopped) return
        const delay = Math.min(15000, 1000 * 2 ** retry.current)
        retry.current += 1
        timer.current = setTimeout(connect, delay)
      }
    }
    connect()
    return () => {
      stopped = true
      if (timer.current) clearTimeout(timer.current)
      es?.close()
    }
  }, [url])

  return status
}
