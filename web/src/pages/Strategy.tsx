import { Card, CardHead } from '../components/ui'

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

export default function Strategy() {
  return (
    <div className="page">
      <header className="page-head">
        <div>
          <h1>策略说明</h1>
          <p>N 字战法：上涨 → 缩量回调 → 放量突破</p>
        </div>
      </header>

      <div className="rule-grid">
        {rules.map(r => (
          <Card key={r.step} className="rule-card">
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
            <table>
              <thead>
                <tr><th>方法</th><th>路径</th><th>说明</th></tr>
              </thead>
              <tbody>
                {endpoints.map(([m, p, d]) => (
                  <tr key={p}>
                    <td><span className={`method-pill ${m}`}>{m}</span></td>
                    <td className="code-cell">{p}</td>
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
