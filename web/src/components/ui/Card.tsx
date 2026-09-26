import type { ReactNode } from 'react'

export function Card({ children, className = '', hoverable = false }: { children: ReactNode; className?: string; hoverable?: boolean }) {
  return <section className={`card${hoverable ? ' hoverable' : ''} ${className}`}>{children}</section>
}

export function CardHead({ title, sub, right }: { title: string; sub?: string; right?: ReactNode }) {
  return (
    <div className="card-head">
      <div>
        <h2>{title}</h2>
        {sub && <p>{sub}</p>}
      </div>
      {right}
    </div>
  )
}
