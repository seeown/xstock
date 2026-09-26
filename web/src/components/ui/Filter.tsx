import type { ButtonHTMLAttributes, InputHTMLAttributes, ReactNode } from 'react'

export function Chip({ active = false, children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { active?: boolean }) {
  return (
    <button type="button" className={`chip${active ? ' on' : ''}`} {...rest}>
      {children}
    </button>
  )
}

export function SelectPill({ label, value, ...rest }: ButtonHTMLAttributes<HTMLButtonElement> & { label: string; value: string }) {
  return (
    <button type="button" className="select-pill" {...rest}>
      {label}：{value} <span className="caret">▼</span>
    </button>
  )
}

export function SearchPill({ placeholder = '搜索代码 / 名称…（/）', ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return <input type="search" className="search-pill mono" placeholder={placeholder} {...rest} />
}

export function FilterBar({ children }: { children: ReactNode }) {
  return <div className="fbar">{children}</div>
}
