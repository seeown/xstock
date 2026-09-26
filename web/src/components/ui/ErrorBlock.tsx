// 页面级错误块必须带「重试」动作（样张库契约）
export function ErrorBlock({ title, desc, onRetry }: { title: string; desc?: string; onRetry?: () => void }) {
  return (
    <div className="errblock" role="alert">
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="var(--up-text)" strokeWidth="1.8" strokeLinecap="round" aria-hidden>
        <circle cx="12" cy="12" r="9" />
        <path d="M12 7.5v5.5" />
        <path d="M12 16.5h.01" />
      </svg>
      <div>
        <div className="t">{title}</div>
        {desc && <div className="d">{desc}</div>}
      </div>
      {onRetry && (
        <button className="btn2-ghost" onClick={onRetry}>重试</button>
      )}
    </div>
  )
}
