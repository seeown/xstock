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
  pullbackVolRatioMax: number
  volumeRatioMin: number
  breakoutBufferPct: number
  stopLossPct: number
  takeProfitPct: number
  maxHoldDays: number
}

// 首板回调（N 字涨停）策略参数：首板涨停 → 缩量回调 → 放量突破。
export interface FirstBoardParams {
  boardLookbackDays: number
  boardVolRatioMin: number
  boardVolRatioMax: number
  excludeOneWordBoard: boolean
  pullbackMinDays: number
  pullbackMaxDays: number
  pullbackMaxPct: number
  pullbackVolRatioMax: number
  breakoutBufferPct: number
  breakoutVolRatioMin: number
  breakoutVolRatioMax: number
  stopLossPct: number
  takeProfitPct: number
  maxHoldDays: number
}

export type StrategyKind = 'n' | 'zt'
export type StrategyParams = NParams | FirstBoardParams

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
  dayChangePct: number
  // 首板回调策略的扩展字段
  boardDate?: string
  boardVolRatio?: number
  pullbackDays?: number
  pullbackVolRatio?: number
  limitPct?: number
  strongWash?: boolean
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
  windowStart?: string
  windowDays?: number
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

// 市场情绪：一个交易日的收盘口径统计。
export interface SentimentDay {
  date: string
  limitUp: number
  limitDown: number
  broke: number
  breakRate: number
  maxBoards: number
  yesterdayLimit: number
  promoted: number
  promoteRate: number
}

export interface SentimentRealtime {
  asOf: string
  updatedAt: string
  limitUp: number
  limitDown: number
  touched: number
  broke: number
  breakRate: number
  maxBoards: number
  yesterdayLimit: number
  promoted: number
  promoteRate: number
}

// 连板梯队：昨日 N 板一档，全量列晋级/失败选手。
export interface LadderStock {
  symbol: string
  name: string
  height: number
  changePct: number
}

export interface LadderTier {
  height: number
  total: number
  promoted: LadderStock[]
  failed: LadderStock[]
  promoteRate: number
}

export interface Ladder {
  asOf: string
  updatedAt: string
  final: boolean
  tiers: LadderTier[]
  newBoards: LadderStock[]
}

export interface SentimentResult {
  asOf: string
  realtime: SentimentRealtime
  history: SentimentDay[]
  ladder?: Ladder
}

export interface SectorRow {
  name: string
  count: number
  avgChange: number
  limitUp: number
  lastDayLimit: number
  limitUpRecent: number[]
  index: number[]
}

export interface SectorsResult {
  asOf: string
  mainline: string
  mainlineStreak: number
  byIndustry: SectorRow[]
  byConcept: SectorRow[]
}

// 情绪指南：量能 + 涨跌停的温度判定。
export interface GuideHistoryDay {
  date: string
  limitUp: number
  limitDown: number
  broke: number
  breakRate: number
  amount: number
  amountRatio: number
  upCount: number
  downCount: number
  tempScore: number
  stage: string
}

export interface GuideRealtime {
  asOf: string
  updatedAt: string
  final: boolean
  limitUp: number
  limitDown: number
  broke: number
  breakRate: number
  maxBoards: number
  promoteRate: number
  upCount: number
  downCount: number
  amountToday: number
  amountYesterday: number
  amountRatio: number
  avgChange: number
  quadrant: string
  tempScore: number
  stage: string
}

export interface GuideResult {
  realtime: GuideRealtime
  history: GuideHistoryDay[]
}

export interface StockProfile {
  symbol: string
  name: string
  industry: string
  market: string
  board: string
  listDate: string
  business: string
  concepts: string[]
  updatedAt: string
}

export interface ProfileListItem {
  symbol: string
  name: string
  industry: string
  market: string
  board: string
  listDate: string
  concepts: string[]
  barCount: number
}

export interface ProfilePageResult {
  items: ProfileListItem[]
  total: number
  page: number
  pageSize: number
}

export interface ConceptCount {
  concept: string
  count: number
}

export interface Quote {
  symbol: string
  price: number
  preClose: number
  changePct: number
  amount: number
}

// 全市场 N 字筛查（手册口径）：一个存活形态的当前快照。
export interface NPSetup {
  symbol: string
  stage: 'b1' | 'b2' | 'b3'
  keyDate: string
  asOf: string
  aStartDate: string
  aEndDate: string
  aRisePct: number
  aVolRatio: number
  hasLimitUp: boolean
  neckline: number
  aStart: number
  bDays: number
  retrRatio: number
  bLow: number
  bVolRatio: number
  retr382: number
  retr50: number
  ma20: number
  currentClose: number
  b1Triggered: boolean
  b1TriggerDate?: string
  b1Signal?: string
  breakoutDate?: string
  breakoutPrice: number
  breakoutVolRatio: number
  daysSinceBreakout: number
  retestDate?: string
  retestLow: number
  retestConfirm: boolean
  stopLoss: number
  target: number
  chaseBan: boolean
  name?: string
  industry?: string
}

export interface ScreenResult {
  asOf: string
  windowDays: number
  counts: { b1: number; b2: number; b3: number }
  items: NPSetup[]
}

export interface SyncState {
  lastRun: string
  lastTradingDay: string
  stocksProbed: number
  barsAppended: number
  fullRefetch: number
  newListings: number
}

export interface ProfileQuery {
  q?: string
  board?: string
  industry?: string
  concept?: string
  synced?: boolean
  sort?: string
  order?: 'asc' | 'desc'
  page?: number
  pageSize?: number
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(url, init)
  } catch (err) {
    // 网络级失败（后端不可达）：页面各自处理外，再广播给全局 Toast。
    window.dispatchEvent(new CustomEvent('xstock:neterr', { detail: '后端服务不可达，请确认 server 已启动' }))
    throw err instanceof Error ? err : new Error('网络错误')
  }
  const data = await res.json().catch(() => ({}))
  if (!res.ok) {
    const msg = (data as { error?: string }).error || `请求失败 (${res.status})`
    throw new Error(msg)
  }
  return data as T
}

export const api = {
  defaultParams: (strategy: StrategyKind = 'n') =>
    request<StrategyParams>(`/api/params/default?strategy=${strategy}`),
  bars: (symbol: string) => request<Candle[]>(`/api/stocks/${encodeURIComponent(symbol)}/bars`),
  signals: (symbol: string, strategy: StrategyKind = 'n') =>
    request<Signal[]>(`/api/stocks/${encodeURIComponent(symbol)}/signals?strategy=${strategy}`),
  backtest: (symbol: string, params: StrategyParams, days = 0, strategy: StrategyKind = 'n') =>
    request<BacktestResult>(`/api/backtests/${encodeURIComponent(symbol)}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ initialCash: 100000, strategy, params, days }),
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
  marketSentiment: () => request<SentimentResult>('/api/market/sentiment'),
  marketSectors: () => request<SectorsResult>('/api/market/sectors'),
  marketGuide: () => request<GuideResult>('/api/market/guide'),
  profile: (symbol: string) =>
    request<StockProfile>(`/api/stocks/${encodeURIComponent(symbol)}/profile`),
  syncProfile: (symbol: string) =>
    request<StockProfile>(`/api/profile/sync/${encodeURIComponent(symbol)}`, { method: 'POST' }),
  profiles: (query: ProfileQuery) => {
    const params = new URLSearchParams()
    if (query.q) params.set('q', query.q)
    if (query.board) params.set('board', query.board)
    if (query.industry) params.set('industry', query.industry)
    if (query.concept) params.set('concept', query.concept)
    if (query.synced) params.set('synced', '1')
    if (query.sort) params.set('sort', query.sort)
    if (query.order) params.set('order', query.order)
    params.set('page', String(query.page ?? 1))
    params.set('pageSize', String(query.pageSize ?? 50))
    return request<ProfilePageResult>(`/api/profiles?${params.toString()}`)
  },
  concepts: () => request<ConceptCount[]>('/api/concepts'),
  industries: () => request<string[]>('/api/industries'),
  syncState: () => request<SyncState>('/api/sync-state'),
  screen: (days: number) => request<ScreenResult>(`/api/screen?days=${days}`),
  quotes: (symbols: string[]) =>
    request<Quote[]>(`/api/quotes?symbols=${encodeURIComponent(symbols.join(','))}`),
}
