const bpLabel = { 1: 'P1 低吸', 2: 'P2 突破', 3: 'P3 回踩' } as const

export function BuyPointBadge({ p, short = false }: { p: 1 | 2 | 3; short?: boolean }) {
  return <span className={`bp bp${p}`}>{short ? `P${p}` : bpLabel[p]}</span>
}

export type StatusKind = 'run' | 'ok' | 'fail' | 'invalid'

const statusCls: Record<StatusKind, string> = {
  run: 'badge-run',
  ok: 'badge-ok',
  fail: 'badge-fail',
  invalid: 'badge-invalid',
}

const statusText: Record<StatusKind, string> = {
  run: '运行中',
  ok: '已完成',
  fail: '失败',
  invalid: '失效',
}

// 失效 / 失败徽章必须带原因（样张库契约）
export function StatusBadge({ status, reason }: { status: StatusKind; reason?: string }) {
  const text = statusText[status] + (reason && (status === 'invalid' || status === 'fail') ? ` · ${reason}` : '')
  return <span className={statusCls[status]}>{text}</span>
}
