export interface Candle {
  date: string
  open: number
  high: number
  low: number
  close: number
  volume: number
}

export interface NParams {
  riseDays: number
  riseMinPct: number
  pullbackMinDays: number
  pullbackMaxDays: number
  pullbackMaxPct: number
  volumeRatioMin: number
  breakoutBufferPct: number
  stopLossPct: number
  takeProfitPct: number
  maxHoldDays: number
}

export interface Signal {
  symbol: string
  date: string
  reason: string
  breakoutPrice: number
  priorHigh: number
  pullbackLow: number
  risePct: number
  pullbackPct: number
  volumeRatio: number
}

export interface Trade {
  symbol: string
  buyDate: string
  sellDate: string
  exitReason: string
  buyPrice: number
  sellPrice: number
  returnPct: number
}

export interface Metrics {
  initialCash: number
  finalCash: number
  totalReturnPct: number
  maxDrawdownPct: number
  winRatePct: number
  tradeCount: number
}

export interface BacktestResult {
  signals: Signal[]
  trades: Trade[]
  metrics: Metrics
}

export interface StockSummary {
  symbol: string
  count: number
  firstDate: string
  lastDate: string
}

export interface SyncResult {
  symbol: string
  count: number
  firstDate: string
  lastDate: string
}

export interface IndexInfo {
  symbol: string
  name: string
  count?: number
  firstDate?: string
  lastDate?: string
}

export interface StockProfile {
  symbol: string
  name: string
  industry: string
  market: string
  listDate: string
  business: string
  concepts: string[]
  updatedAt: string
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const msg = (data as { error?: string }).error || `请求失败 (${res.status})`
    throw new Error(msg)
  }
  return data as T
}

export const api = {
  defaultParams: () => request<NParams>('/api/params/default'),
  bars: (symbol: string) => request<Candle[]>(`/api/stocks/${encodeURIComponent(symbol)}/bars`),
  signals: (symbol: string) => request<Signal[]>(`/api/stocks/${encodeURIComponent(symbol)}/signals`),
  backtest: (symbol: string, params: NParams) =>
    request<BacktestResult>(`/api/backtests/${encodeURIComponent(symbol)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ initialCash: 100000, params }),
    }),
  sync: (symbol: string) =>
    request<SyncResult>(`/api/sync/${encodeURIComponent(symbol)}`, { method: 'POST' }),
  importBars: (symbol: string, bars: Candle[]) =>
    request<{ symbol: string; count: number }>(`/api/bars/${encodeURIComponent(symbol)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(bars),
    }),
  stocks: () => request<StockSummary[]>('/api/stocks'),
  marketIndices: () => request<IndexInfo[]>('/api/market/indices'),
  profile: (symbol: string) =>
    request<StockProfile>(`/api/stocks/${encodeURIComponent(symbol)}/profile`),
  syncProfile: (symbol: string) =>
    request<StockProfile>(`/api/profile/sync/${encodeURIComponent(symbol)}`, { method: 'POST' }),
}
