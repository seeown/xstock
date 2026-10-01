import { useState } from 'react'
import { api } from '../api'
import { fmt } from './ui'

// 行内快捷模拟买入（雷达/自选列表用）：点「买」展开数量+确认，
// 市价即时成交，自动带上调用方给的信号上下文。限价请走 K 线弹窗。
export function PaperQuickBuy({ symbol, name, signal, onDone }: {
  symbol: string
  name?: string
  signal?: unknown
  onDone?: (msg: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [qty, setQty] = useState('100')
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')

  const q = Math.max(0, Math.floor(Number(qty) || 0))

  const submit = () => {
    setBusy(true); setMsg('')
    api.paperOrder({ symbol, name: name ?? '', side: 'buy', qty: q, signal })
      .then(res => {
        const m = 'filled' in res
          ? `✓ 已成交 ${res.filled.qty} 股 @ ${fmt(res.filled.price)}`
          : `⏳ 已挂限价单 @ ${fmt(res.placed.limitPrice)}`
        setMsg(m)
        onDone?.(m)
        window.setTimeout(() => setOpen(false), 1800)
      })
      .catch(e => setMsg('✗ ' + (e instanceof Error ? e.message : '下单失败')))
      .finally(() => setBusy(false))
  }

  if (!open) {
    return (
      <button className="btn2-mini" onClick={e => { e.stopPropagation(); setOpen(true) }} title="模拟买入">买</button>
    )
  }
  return (
    <span className="paper-quick" onClick={e => e.stopPropagation()}>
      <input
        className="input" type="number" min={100} step={100} value={qty}
        onChange={e => setQty(e.target.value)} aria-label="买入数量" style={{ width: 84 }}
      />
      <button className="btn2-mini primary" disabled={busy || q < 100 || q % 100 !== 0} onClick={submit}>
        {busy ? '…' : '确认'}
      </button>
      <button className="btn2-mini" onClick={() => { setOpen(false); setMsg('') }}>✕</button>
      {msg && <span className="pq-msg" role="status">{msg}</span>}
    </span>
  )
}
