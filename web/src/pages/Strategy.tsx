import { useEffect, useMemo, useState } from 'react'
import { api, type ParamSet, type StrategyKind, type StrategyParams } from '../api'
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
  ['GET', '/api/params/default', '默认策略参数（库默认优先，内置兜底）'],
  ['GET', '/api/params/sets', '参数组列表'],
  ['POST', '/api/params/sets', '新建参数组'],
  ['PUT', '/api/params/sets/{id}', '更新参数组'],
  ['DELETE', '/api/params/sets/{id}', '删除参数组'],
  ['POST', '/api/params/sets/{id}/default', '设为该策略回测默认'],
  ['POST', '/api/params/default/clear', '恢复内置默认'],
  ['GET', '/api/market/indices', '大盘指数列表'],
  ['POST', '/api/backtests/{symbol}', '执行回测'],
  ['POST', '/api/sync/{symbol}', '同步通达信前复权日线'],
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
}

// 旧版把自定义组存在浏览器 localStorage；检测到就提供一次性迁移。
const LS_KEY = 'xstock.param-groups'

interface Row {
  id: string
  name: string
  strategy: StrategyKind
  params: StrategyParams
  builtin: boolean
  isDefault: boolean
}

export default function Strategy() {
  const [sets, setSets] = useState<ParamSet[] | null>(null)
  const [builtins, setBuiltins] = useState<Array<{ strategy: StrategyKind; params: StrategyParams }>>([])
  const [selectedId, setSelectedId] = useState('builtin-n')
  const [copyName, setCopyName] = useState('')
  const [note, setNote] = useState('')
  const [busy, setBusy] = useState(false)
  // 自定义组的本地草稿：改参数先落这里，「保存修改」才 PUT。
  const [draft, setDraft] = useState<Record<string, number> | null>(null)

  const reload = async () => {
    try {
      setSets(await api.paramSets())
    } catch (e) {
      setNote(e instanceof Error ? e.message : '参数组加载失败')
      setSets([])
    }
  }

  useEffect(() => {
    reload()
    Promise.all([api.defaultParams('n'), api.defaultParams('zt')])
      .then(([n, zt]) => setBuiltins([{ strategy: 'n', params: n }, { strategy: 'zt', params: zt }]))
      .catch(() => {})
  }, [])

  const rows: Row[] = useMemo(() => {
    const defOf = (st: StrategyKind) => sets?.some(g => g.strategy === st && g.isDefault) ?? false
    return [
      ...builtins.map(b => ({ id: `builtin-${b.strategy}`, name: `内置 · ${b.strategy === 'n' ? '通用 N 字' : '首板回调'}`, strategy: b.strategy, params: b.params, builtin: true, isDefault: !defOf(b.strategy) })),
      ...(sets ?? []).map(g => ({ id: String(g.id), name: g.name, strategy: g.strategy, params: g.params, builtin: false, isDefault: g.isDefault })),
    ]
  }, [builtins, sets])

  const selected = rows.find(r => r.id === selectedId) ?? rows[0] ?? null
  const fields = selected
    ? Object.entries(selected.params as unknown as Record<string, unknown>).filter(([, v]) => typeof v === 'number')
    : []
  const dirty = !!selected && !selected.builtin && draft != null

  const numAt = (key: string, fallback: number) => (draft && draft[key] != null ? draft[key] : fallback)

  const refresh = async (msg?: string) => {
    setBusy(true)
    try {
      await reload()
      if (msg) setNote(msg)
    } finally {
      setBusy(false)
    }
  }

  const saveCopy = async () => {
    if (!selected) return
    const name = copyName.trim() || `${selected.name} 副本`
    setBusy(true)
    try {
      const created = await api.saveParamSet({ name, strategy: selected.strategy, params: selected.params })
      setCopyName('')
      setDraft(null)
      await reload()
      setSelectedId(String(created.id))
      setNote(`已保存「${name}」`)
    } catch (e) {
      setNote(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const saveDraft = async () => {
    if (!selected || selected.builtin || !draft) return
    setBusy(true)
    try {
      await api.updateParamSet(Number(selected.id), {
        name: selected.name,
        params: { ...selected.params, ...draft } as StrategyParams,
      })
      setDraft(null)
      await refresh('修改已保存')
    } catch (e) {
      setNote(e instanceof Error ? e.message : '保存失败')
    } finally {
      setBusy(false)
    }
  }

  const markDefault = async (row: Row) => {
    setBusy(true)
    try {
      if (row.builtin) {
        await api.clearDefaultParamSet(row.strategy)
        await refresh(`已恢复内置默认（${row.name}）`)
      } else {
        await api.setDefaultParamSet(Number(row.id))
        await refresh(`「${row.name}」已设为回测默认，回测页加载即采用`)
      }
    } catch (e) {
      setNote(e instanceof Error ? e.message : '操作失败')
    } finally {
      setBusy(false)
    }
  }

  const removeGroup = async () => {
    if (!selected || selected.builtin) return
    setBusy(true)
    try {
      await api.deleteParamSet(Number(selected.id))
      setSelectedId('builtin-n')
      setDraft(null)
      await refresh(`已删除「${selected.name}」`)
    } catch (e) {
      setNote(e instanceof Error ? e.message : '删除失败')
    } finally {
      setBusy(false)
    }
  }

  // localStorage 旧组一次性迁移
  const [legacy, setLegacy] = useState<Array<{ name: string; strategy: StrategyKind; params: StrategyParams }>>([])
  useEffect(() => {
    try {
      const raw = JSON.parse(localStorage.getItem(LS_KEY) ?? '[]')
      if (Array.isArray(raw) && raw.length) setLegacy(raw)
    } catch { /* ignore */ }
  }, [])
  const importLegacy = async () => {
    setBusy(true)
    try {
      for (const g of legacy) {
        await api.saveParamSet({ name: g.name, strategy: g.strategy, params: g.params })
      }
      localStorage.removeItem(LS_KEY)
      setLegacy([])
      await refresh(`已导入 ${legacy.length} 个本机参数组`)
    } catch (e) {
      setNote(e instanceof Error ? e.message : '导入失败')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>策略说明</h1>
          <p>通用 N 字：上涨 → 缩量回调 → 放量突破；首板回调：首板涨停 → 缩量回调 → 放量突破</p>
        </div>
      </header>

      {legacy.length > 0 && (
        <div className="env neutral">
          <span className="tag">迁移</span>
          <span className="desc">检测到 {legacy.length} 个旧版本保存在本机浏览器的参数组，导入后可在所有设备使用。</span>
          <Button variant="mini" style={{ marginLeft: 'auto' }} disabled={busy} onClick={importLegacy}>导入到数据库</Button>
        </div>
      )}

      <Card>
        <CardHead
          title="参数组 · 回测默认"
          sub="设为默认后，「回测分析」页加载参数即采用该组；未设置时使用内置值。内置只读可另存，自定义可编辑删除（存 PostgreSQL）。"
          right={note ? <span className="muted-c" style={{ fontSize: 11.5, whiteSpace: 'nowrap' }}>{note}</span> : undefined}
        />
        <div className="table-panel" style={{ marginBottom: 14 }}>
          <div className="table-wrap">
            <table className="tb">
              <thead>
                <tr><th>名称</th><th>策略</th><th className="num">参数项</th><th>回测默认</th><th>保存时间</th><th>操作</th></tr>
              </thead>
              <tbody>
                {rows.map(row => (
                  <tr key={row.id} className={`rowlink${selected?.id === row.id ? ' sel' : ''}`} onClick={() => { setSelectedId(row.id); setDraft(null) }}>
                    <td>
                      <span style={{ fontFamily: 'var(--sans)', fontWeight: 600, color: 'var(--ink)' }}>{row.name}</span>
                      {row.builtin && <span className="badge-run" style={{ marginLeft: 8 }}>内置</span>}
                    </td>
                    <td>{row.strategy === 'n' ? '通用 N 字' : '首板回调'}</td>
                    <td className="num">{Object.keys(row.params as object).length}</td>
                    <td>{row.isDefault && <span className="badge-ok">当前默认</span>}</td>
                    <td className="muted-c mono" style={{ fontSize: 11 }}>{row.builtin ? '—' : (sets?.find(g => String(g.id) === row.id)?.updatedAt ?? '').slice(0, 19).replace('T', ' ')}</td>
                    <td>
                      {row.isDefault
                        ? (row.builtin ? <span className="muted-c" style={{ fontSize: 11 }}>未设置自定义默认</span> : (
                            <Button variant="mini" disabled={busy} onClick={e => { e.stopPropagation(); markDefault(rows.find(r => r.builtin && r.strategy === row.strategy) ?? row) }}>恢复内置</Button>
                          ))
                        : <Button variant="mini" disabled={busy} onClick={e => { e.stopPropagation(); markDefault(row) }}>设为默认</Button>}
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
                  placeholder={`另存为新组，如：${selected.name} 严选`}
                  value={copyName}
                  onChange={e => setCopyName(e.target.value)}
                  onKeyDown={e => e.key === 'Enter' && saveCopy()}
                />
                <Button onClick={saveCopy} loading={busy && !dirty}>另存为新组</Button>
                {!selected.builtin && (
                  <>
                    <Button variant="ghost" disabled={!dirty || busy} onClick={saveDraft}>保存修改</Button>
                    {dirty && <Button variant="mini" onClick={() => setDraft(null)}>放弃</Button>}
                    <Button variant="mini-danger" disabled={busy} onClick={removeGroup}>删除此组</Button>
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
                    value={String(numAt(key, Number(value)))}
                    disabled={selected.builtin}
                    onChange={e => setDraft(prev => ({ ...(prev ?? {}), [key]: Number(e.target.value) }))}
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
                <div className="kv"><span className="k">生效范围</span><span>设为默认 → 回测页加载即用</span></div>
                <div className="kv"><span className="k">内置组</span><span className="ok">只读 · 可另存</span></div>
                <div className="kv"><span className="k">自定义组</span><span className="ok">改后点「保存修改」</span></div>
                <div className="kv"><span className="k">存储</span><span className="mono">PostgreSQL</span></div>
                <div className="kv"><span className="k">每策略默认</span><span>通用 N 字 / 首板回调 各一个</span></div>
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
