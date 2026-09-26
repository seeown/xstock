import type { InputHTMLAttributes, ReactNode } from 'react'

export function TextField({
  label,
  error,
  hint,
  className = '',
  ...rest
}: InputHTMLAttributes<HTMLInputElement> & { label: ReactNode; error?: string; hint?: string }) {
  return (
    <>
      <div className="form-row">
        <span className="fl">{label}</span>
        <input className={`input${error ? ' err' : ''} ${className}`} aria-invalid={error ? true : undefined} {...rest} />
      </div>
      {error ? <div className="field-err">{error}</div> : hint ? <div className="field-hint">{hint}</div> : null}
    </>
  )
}
