// 骨架屏保持最终布局形状；shimmer 动画在 prefers-reduced-motion 下由 CSS 停用
export function Skeleton({ w, h = 12, radius, count = 1, gap = 14 }: { w?: string | number; h?: number; radius?: number; count?: number; gap?: number }) {
  const width = typeof w === 'number' ? `${w}px` : w
  return (
    <div style={{ display: count > 1 ? 'grid' : 'block', gap: count > 1 ? gap : undefined }}>
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="skel" style={{ width, height: h, borderRadius: radius ? `${radius}px` : undefined }} />
      ))}
    </div>
  )
}
