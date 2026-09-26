import type { ButtonHTMLAttributes } from 'react'

export type ButtonVariant = 'primary' | 'ghost' | 'mini' | 'mini-danger'

export function Button({
  variant = 'primary',
  loading = false,
  disabled = false,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; loading?: boolean }) {
  const cls =
    variant === 'primary' ? 'btn2'
    : variant === 'ghost' ? 'btn2-ghost'
    : variant === 'mini-danger' ? 'btn2-mini danger'
    : 'btn2-mini'
  return (
    <button className={cls} disabled={disabled || loading} {...rest}>
      {loading && <span className={variant === 'primary' ? 'spin-aurora' : 'spin-line'} />}
      {children}
    </button>
  )
}
