// ui.tsx 是组件系统统一出口（页面导入路径不变）。
// 新组件放 components/ui/ 下，一组件一文件，在此 re-export。
export { fmt, pct, money, exitReasonText } from './ui/format'
export { Card, CardHead } from './ui/Card'
export { MetricCard } from './ui/MetricCard'
export { KpiCard } from './ui/KpiCard'
export type { KpiTone } from './ui/KpiCard'
export { Button } from './ui/Button'
export type { ButtonVariant } from './ui/Button'
export { BuyPointBadge, StatusBadge } from './ui/Badge'
export type { StatusKind } from './ui/Badge'
export { EnvBanner } from './ui/EnvBanner'
export type { EnvTone } from './ui/EnvBanner'
export { Chip, SelectPill, SearchPill, FilterBar } from './ui/Filter'
export { TextField } from './ui/TextField'
export { RpsBar } from './ui/RpsBar'
export { Progress } from './ui/Progress'
export { LogStream } from './ui/LogStream'
export type { LogLine, LogLevel } from './ui/LogStream'
export { useToasts, ToastStack } from './ui/Toast'
export type { ToastItem } from './ui/Toast'
export { ErrorBlock } from './ui/ErrorBlock'
export { Skeleton } from './ui/Skeleton'
export { EmptyState } from './ui/EmptyState'
export { Spinner } from './ui/Spinner'
export { Banner } from './ui/Banner'
export { Sparkline } from './ui/Sparkline'

// 兼容旧 Empty 签名（纯文本空态），新页面请用 EmptyState
export function Empty({ text }: { text: string }) {
  return <div className="empty">{text}</div>
}
