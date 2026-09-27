import { forwardRef } from 'react'
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

export const SearchPill = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(
  function SearchPill({ placeholder = '搜索代码 / 名称…（/）', className = '', ...rest }, ref) {
    // 外部 className（如 push-right）与基础样式合并，不能覆盖
    return <input ref={ref} type="search" className={`search-pill mono ${className}`.trim()} placeholder={placeholder} {...rest} />
  },
)

export function FilterBar({ children }: { children: ReactNode }) {
  return <div className="fbar">{children}</div>
}
