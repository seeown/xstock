export function Spinner({ text = '加载中…' }: { text?: string }) {
  return (
    <div className="spinner-wrap">
      <span className="spinner" />
      <span>{text}</span>
    </div>
  )
}
