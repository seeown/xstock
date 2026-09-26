import { useEffect, useMemo, useState } from 'react'
import { api, type StrategyKind, type StrategyParams } from '../api'
import { Button, Card, CardHead, TextField } from '../components/ui'

const rules = [
  {
    step: '01',
    title: '固定窗口上涨',
    desc: '在设定的上涨周期内，最高价相对起涨价达到最小涨幅，形成明确上升趋势。',
  },
  {
    step: '02',
    title: '缩量回调',
    desc: '回调天数与回撤幅度受限，且不跌破起涨价；回调均量必须小于上涨均量。',
  },
  {
    step: '03',
    title: '放量突破',
    desc: '收盘价突破前高，突破日成交量相对回调均量达到设定量比，确认 N 字结构。',
  },
]

const ztRules = [
  {
    step: '01',
    title: '首板确认',
    desc: '收盘涨停（按板块区分 10%/20%，创业板按注册制改革日期判定）且之前 N 个交易日无涨停；板日量比 2.5~8，天量首板（>8）易炸板，直接否决；默认排除一字板。',
  },
  {
    step: '02',
    title: '缩量回调',
    desc: '回调 2~5 天，逐日成交量不超过首板日的 70%——任一天放量回调即否决；低点不破首板起涨点、幅度 ≤8%；量缩到板日量 1/3 以下标记为洗盘金标准（★）。',
  },
  {
    step: '03',
    title: '放量突破',
    desc: '收盘突破首板高点，突破日标准量比（当日量 ÷ 前 5 日均量）1.5~3 为温和放量；信号收盘生成，次日开盘入场。',
  },
]

const tradeRules: Array<[string, string]> = [
  ['入场', '信号收盘后生成，下一交易日开盘价买入，规避未来函数'],
  ['止损', '盘中触及买入价下方止损比例即卖出'],
  ['止盈', '盘中触及买入价上方止盈比例即卖出'],
  ['持仓', '超过最长持仓天数后按收盘价平仓'],
]

const endpoints: Array<[string, string, string]> = [
  ['GET', '/api/params/default', '默认策略参数'],
  ['GET', '/api/market/indices', '大盘指数列表'],
  ['GET', '/api/stocks', '本地股票池列表'],
  ['GET', '/api/stocks/{symbol}/bars', '获取日线序列'],
  ['GET', '/api/stocks/{symbol}/signals', '默认参数信号'],
  ['POST', '/api/backtests/{symbol}', '执行回测'],
  ['POST', '/api/sync/{symbol}', '同步通达信前复权日线'],
  ['POST', '/api/bars/{symbol}', '导入自定义日线 JSON'],
]

// 参数字段的中文标签（详情表单用；与回测页字段定义一致）
const FIELD_LABELS: Record<string, string> = {
  riseDays: '上涨周期（天）', riseMinPct: '最小涨幅（%）',
  pullbackMinDays: '最短回调（天）', pullbackMaxDays: '最长回调（天）',
  pullbackMaxPct: '最大回撤（%）', pullbackVolRatioMax: '回调量比上限（×）',
  volumeRatioMin: '突破量比下限（×）',
  boardLookbackDays: '首板回看（天）', boardVolRatioMin: '板日量比下限（×）',
  boardVolRatioMax: '板日量比上限（×）', breakoutVolRatioMin: '突破量比下限（×）',
  breakoutVolRatioMax: '突破量比上限（×）',
  stopLossPct: '止损线（%）', takeProfitPct: '止盈线（%）', maxHoldDays: '最长持仓（天）',
  excludeOneWordBoard: '排除一字首板',
}

interface ParamGroup {
  id: string
  name: string
  strategy: StrategyKind
  params: StrategyParams
  builtin?: boolean
  savedAt?: string
}

const LS_KEY = 'xstock.param-groups'
const loadGroups = (): ParamGroup[] => {
  try {
    return JSON.parse(localStorage.getItem(LS_KEY) ?? '[]') as ParamGroup[]
  } catch {
    return []
  }
}

export default function Strategy() {
  const [builtins, setBuiltins] = useState<ParamGroup[]>([])
  const [custom, setCustom] = useState<ParamGroup[]>(loadGroups)
  const [selectedId, setSelectedId] = useState('builtin-n')
  const [copyName, setCopyName] = useState('')
  const [note, setNote] = useState('')

  useEffect(() => {
    Promise.all([api.defaultParams('n'), api.defaultParams('zt')])
      .then(([n, zt]) => {
        setBuiltins([
          { id: 'builtin-n', name: '内置 · 通用 N 字', strategy: 'n', params: n, builtin: true },
          { id: 'builtin-zt', name: '内置 · 首板回调', strategy: 'zt', params: zt, builtin: true },
        ])
      })
      .catch(() => setNote('内置参数加载失败，请检查后端服务。'))
  }, [])

  const persist = (groups: ParamGroup[]) => {
    setCustom(groups)
    localStorage.setItem(LS_KEY, JSON.stringify(groups))
  }

  const groups = useMemo(() => [...builtins, ...custom], [builtins, custom])
  const selected = groups.find(g => g.id === selectedId) ?? groups[0] ?? null
  const fields = selected
    ? Object.entries(selected.params as unknown as Record<string, unknown>).filter(([, v]) => typeof v === 'number')
    : []

  const saveCopy = () => {
    if (!selected) return
    const name = copyName.trim() || `${selected.name} 副本`
    const g: ParamGroup = {
      id: `g-${Date.now()}`,
      name,
      strategy: selected.strategy,
      params: { ...selected.params } as StrategyParams,
      savedAt: new Date().toLocaleString('zh-CN'),
    }
    persist([...custom, g])
    setSelectedId(g.id)
    setCopyName('')
    setNote(`已另存为「${name}」`)
  }

  const updateField = (key: string, value: number) => {
    if (!selected || selected.builtin) return
    const next = custom.map(g => g.id === selected.id ? { ...g, params: { ...g.params, [key]: value } as StrategyParams } : g)
    persist(next)
  }

  const removeGroup = () => {
    if (!selected || selected.builtin) return
    persist(custom.filter(g => g.id !== selected.id))
    setSelectedId('builtin-n')
    setNote(`已删除「${selected.name}」`)
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>策略说明</h1>
          <p>通用 N 字：上涨 → 缩量回调 → 放量突破；首板回调：首板涨停 → 缩量回调 → 放量突破</p>
        </div>
      </header>

      <Card>
        <CardHead title="参数组" sub="内置只读可另存 · 自定义可编辑删除（保存在本机浏览器）" right={note ? <span className="muted-c" style={{ fontSize: 11.5 }}>{note}</span> : undefined} />
        <div className="table-panel" style={{ marginBottom: 14 }}>
          <div className="table-wrap">
            <table className="tb">
              <thead>
                <tr><th>名称</th><th>策略</th><th className="num">参数项</th><th>保存时间</th><th>操作</th></tr>
              </thead>
              <tbody>
                {groups.map(g => (
                  <tr key={g.id} className={`rowlink${selected?.id === g.id ? ' sel' : ''}`} onClick={() => setSelectedId(g.id)}>
                    <td><span className="code" style={{ fontFamily: 'var(--sans)' }}>{g.name}{g.builtin && <span className="badge-run" style={{ marginLeft: 8 }}>内置</span>}</span></td>
                    <td>{g.strategy === 'n' ? '通用 N 字' : '首板回调'}</td>
                    <td className="num">{Object.keys(g.params as object).length}</td>
                    <td className="muted-c mono" style={{ fontSize: 11 }}>{g.savedAt ?? '—'}</td>
                    <td>
                      <Button variant="mini" onClick={e => { e.stopPropagation(); setSelectedId(g.id) }}>查看</Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>

        {selected && (
          <div className="split section-gap">
            <div className="grow">
              <div className="fbar" style={{ marginBottom: 12 }}>
                <input
                  className="input" style={{ flex: '0 0 260px' }}
                  placeholder={`另存为副本，如：${selected.name} 严选`
                  }
                  value={copyName}
                  onChange={e => setCopyName(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && saveCopy()}
                  disabled={selected.builtin ? false : false}
                />
                <Button onClick={saveCopy}>另存为副本</Button>
                {!selected.builtin && (
                  <>
                    <Button variant="ghost" onClick={() => setNote('自定义组修改后即时保存（输入即生效）')}>已自动保存</Button>
                    <Button variant="mini-danger" onClick={removeGroup}>删除此组</Button>
                  </>
                )}
              </div>
              <div style={{ maxWidth: 480 }}>
                {fields.map(([key, value]) => (
                  <TextField
                    key={key}
                    label={FIELD_LABELS[key] ?? key}
                    type="number"
                    step="any"
                    value={String(value)}
                    disabled={selected.builtin}
                    onChange={e => updateField(key, Number(e.target.value))}
                  />
                ))}
                {'excludeOneWordBoard' in (selected.params as object) && (
                  <div className="form-row">
                    <span className="fl">排除一字首板</span>
                    <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12.5 }}>
                      <input
                        type="checkbox"
                        checked={(selected.params as unknown as Record<string, unknown>).excludeOneWordBoard === true}
                        disabled={selected.builtin}
                        onChange={e => updateField('excludeOneWordBoard', e.target.checked ? 1 : 0)}
                      />
                      {(selected.params as unknown as Record<string, unknown>).excludeOneWordBoard === true ? '排除' : '不排除'}
                    </label>
                  </div>
                )}
              </div>
            </div>
            <div className="stack" style={{ flex: '0 0 300px' }}>
              <Card hoverable>
                <div className="h"><span className="dot" />使用规则</div>
                <div className="kv"><span className="k">生效范围</span><span>回测页会读取默认参数</span></div>
                <div className="kv"><span className="k">内置组</span><span className="ok">只读 · 可另存</span></div>
                <div className="kv"><span className="k">自定义组</span><span className="ok">输入即保存</span></div>
                <div className="kv"><span className="k">存储</span><span className="mono">localStorage</span></div>
              </Card>
              <Card hoverable>
                <div className="h"><span className="dot" />调参建议</div>
                <div className="kv"><span className="k">量比</span><span>2~3 温和放量较稳</span></div>
                <div className="kv"><span className="k">止损</span><span>3%~5% 常用区间</span></div>
                <div className="kv"><span className="k">止盈</span><span>10%~20% 配合持仓期</span></div>
              </Card>
            </div>
          </div>
        )}
      </Card>

      <h2 className="strategy-section-title">通用 N 字战法<span>按涨幅识别，不限涨停</span></h2>
      <div className="rule-grid">
        {rules.map(r => (
          <Card key={r.step} className="rule-card" hoverable>
            <span className="rule-step">{r.step}</span>
            <h3>{r.title}</h3>
            <p>{r.desc}</p>
          </Card>
        ))}
      </div>

      <h2 className="strategy-section-title">首板回调战法（N 字涨停）<span>涨停判定 + 标准量比口径</span></h2>
      <div className="rule-grid">
        {ztRules.map(r => (
          <Card key={r.step} className="rule-card" hoverable>
            <span className="rule-step">{r.step}</span>
            <h3>{r.title}</h3>
            <p>{r.desc}</p>
          </Card>
        ))}
      </div>

      <div className="strategy-cols">
        <Card>
          <CardHead title="交易规则" sub="单仓位 MVP，信号平仓前忽略后续信号" />
          <div className="trade-rules">
            {tradeRules.map(([k, v]) => (
              <div key={k} className="trade-rule">
                <span className="rule-key">{k}</span>
                <span>{v}</span>
              </div>
            ))}
          </div>
        </Card>
        <Card>
          <CardHead title="API 一览" sub="前后端同源，可直接调用" />
          <div className="table-wrap">
            <table className="tb">
              <thead>
                <tr><th>方法</th><th>路径</th><th>说明</th></tr>
              </thead>
              <tbody>
                {endpoints.map(([m, p, d]) => (
                  <tr key={p}>
                    <td><span className={`method-pill ${m}`}>{m}</span></td>
                    <td className="code-cell mono">{p}</td>
                    <td>{d}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      </div>

      <Card className="risk-card">
        <CardHead title="风险提示" />
        <p>
          这是研究工具，不构成投资建议。接入真实数据前，应补充交易费用、滑点、涨跌停和停牌逻辑，
          并使用包含退市证券的历史股票池以降低幸存者偏差。
        </p>
      </Card>
    </div>
  )
}
