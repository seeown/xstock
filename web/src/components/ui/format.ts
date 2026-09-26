export const fmt = (n: number) =>
  Number(n).toLocaleString('zh-CN', { maximumFractionDigits: 2 })

export const pct = (n: number) => `${Number(n).toFixed(2)}%`

export const money = (n: number) =>
  `¥${Number(n).toLocaleString('zh-CN', { maximumFractionDigits: 0 })}`

export const exitReasonText: Record<string, string> = {
  stop_loss: '止损',
  take_profit: '止盈',
  max_hold: '到期平仓',
}
