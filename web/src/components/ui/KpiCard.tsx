import type { ReactNode } from 'react'

export type KpiTone = 'a' | 'g' | 'o' | 'p'

// 全站唯一的数值卡：大数字在上、标签在下，可选第三行 hint（弱化小字）。
// valueClass 用于语义/均线着色（up-text、down-text、ma5-text…）。
export function KpiCard({
  tone = 'a', icon, label, value, valueClass, hint,
}: {
  tone?: KpiTone
  icon?: ReactNode
  label: string
  value: string
  valueClass?: string
  hint?: ReactNode
}) {
  return (
    <div className="kpi">
      {icon && <span className={`ic ${tone}`}>{icon}</span>}
      <span>
        <span className={`v${valueClass ? ` ${valueClass}` : ''}`}>{value}</span>
        <span className="l">{label}</span>
        {hint && <span className="hint">{hint}</span>}
      </span>
    </div>
  )
}
