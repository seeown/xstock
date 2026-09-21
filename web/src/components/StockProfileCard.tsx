import { useCallback, useEffect, useState } from 'react'
import { api, type StockProfile } from '../api'
import { Card, CardHead } from './ui'

type State = 'loading' | 'ready' | 'missing'

// Self-contained stock profile card: loads cached metadata for the symbol,
// offers a sync button when none exists yet. Indices and DEMO have no profile.
export default function StockProfileCard({ symbol }: { symbol: string }) {
  const [profile, setProfile] = useState<StockProfile | null>(null)
  const [state, setState] = useState<State>('loading')
  const [syncing, setSyncing] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async (sym: string) => {
    setState('loading')
    setError('')
    try {
      setProfile(await api.profile(sym))
      setState('ready')
    } catch {
      setProfile(null)
      setState('missing')
    }
  }, [])

  useEffect(() => {
    load(symbol)
  }, [symbol, load])

  const sync = async () => {
    setSyncing(true)
    setError('')
    try {
      setProfile(await api.syncProfile(symbol))
      setState('ready')
    } catch (e) {
      setError(e instanceof Error ? e.message : '档案同步失败')
    } finally {
      setSyncing(false)
    }
  }

  return (
    <Card>
      <CardHead
        title="个股档案"
        sub={state === 'ready' && profile ? `${profile.symbol} · 更新于 ${profile.updatedAt}` : `${symbol} 的基本信息、概念与主营业务`}
        right={
          <button className="btn ghost small" onClick={sync} disabled={syncing}>
            {syncing ? '同步中…' : state === 'missing' ? '同步档案' : '更新档案'}
          </button>
        }
      />
      {state === 'loading' && <div className="chart-empty">正在加载档案…</div>}
      {state === 'missing' && (
        <div className="profile-missing">
          <p>本地还没有 {symbol} 的档案（公司名称、行业、概念、主营业务）。</p>
          {error && <p className="profile-error">{error}</p>}
        </div>
      )}
      {state === 'ready' && profile && (
        <div className="profile-body">
          <div className="profile-head">
            <span className="profile-name">{profile.name || profile.symbol}</span>
            {profile.board && <span className="concept-chip board-chip">{profile.board}</span>}
            <span className="profile-meta">
              {profile.industry && <span>{profile.industry}</span>}
              {profile.listDate && <span>上市 {profile.listDate}</span>}
              <span>{profile.market}</span>
            </span>
          </div>
          {profile.concepts.length > 0 && (
            <div className="concept-chips">
              {profile.concepts.map(c => (
                <span key={c} className="concept-chip">{c}</span>
              ))}
            </div>
          )}
          {profile.business && (
            <details className="profile-business">
              <summary>业务与经营（F10）</summary>
              <pre>{profile.business}</pre>
            </details>
          )}
          {error && <p className="profile-error">{error}</p>}
        </div>
      )}
    </Card>
  )
}
