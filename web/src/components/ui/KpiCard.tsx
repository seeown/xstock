import type { ReactNode } from 'react'

export type KpiTone = 'a' | 'g' | 'o' | 'p'

export function KpiCard({ tone = 'a', icon, label, value }: { tone?: KpiTone; icon?: ReactNode; label: string; value: string }) {
  return (
    <div className="kpi">
      {icon && <span className={`ic ${tone}`}>{icon}</span>}
      <span>
        <span className="l">{label}</span>
        <span className="v">{value}</span>
      </span>
    </div>
  )
}
