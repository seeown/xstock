export function Banner({ text, kind }: { text: string; kind: 'info' | 'error' | 'success' }) {
  if (!text) return null
  return <div className={`banner ${kind}`}>{text}</div>
}
