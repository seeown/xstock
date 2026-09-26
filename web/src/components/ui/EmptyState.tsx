import type { ReactNode } from 'react'

// 空态回答「为什么空 + 怎么办」，两个动作（样张库契约）
export function EmptyState({ title, desc, actions }: { title: string; desc?: string; actions?: ReactNode }) {
  return (
    <div className="empty">
      <div className="glow" />
      <div className="t">{title}</div>
      {desc && <div className="d">{desc}</div>}
      {actions && <div className="acts">{actions}</div>}
    </div>
  )
}
