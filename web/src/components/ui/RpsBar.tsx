export function RpsBar({ value, max = 100 }: { value: number; max?: number }) {
  const w = Math.max(0, Math.min(100, (value / max) * 100))
  return (
    <span className="rps">
      <span className="bar">
        <i style={{ width: `${w.toFixed(1)}%` }} />
      </span>
      <b>{value.toFixed(0)}</b>
    </span>
  )
}
