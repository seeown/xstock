export function Progress({ value }: { value: number }) {
  const w = Math.max(0, Math.min(100, value))
  return (
    <div className="progress" role="progressbar" aria-valuenow={w} aria-valuemin={0} aria-valuemax={100}>
      <i style={{ width: `${w.toFixed(1)}%` }} />
    </div>
  )
}
