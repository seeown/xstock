import { useState, type ReactNode } from 'react'
import { Button, BuyPointBadge, StatusBadge, EnvBanner, Chip, SelectPill, SearchPill, FilterBar, TextField, RpsBar, Progress, LogStream, useToasts, ToastStack, ErrorBlock, Skeleton, EmptyState, KpiCard, Card, CardHead, LogLine } from '../components/ui'

// 开发用组件样张路由（/dev/ui）：对照 ~/Desktop/NStock-A/06-组件样张库.html 逐状态核对。
function Spec({ no, title, children }: { no: string; title: string; children: ReactNode }) {
  return (
    <div className="card">
      <div className="h"><span className="dot" /><span className="mono">{no}</span>{title}</div>
      {children}
    </div>
  )
}

const demoLog: LogLine[] = [
  { level: 'i', text: '[17:32:15] INFO 扫描完成 候选=12 (P2=5) 过滤剔除=187 耗时=2m14s' },
  { level: 'w', text: '[17:32:16] WARN 6003xx 日线缺口检测：除权日 09-12 复权因子已应用' },
  { level: 'e', text: '[17:32:17] ERROR 6881xx 数据缺失（停牌 3 日）→ 已跳过并标记' },
]

export default function DevUI() {
  const { toasts, push, dismiss } = useToasts()
  const [chipOn, setChipOn] = useState(true)
  const [tfv, setTfv] = useState('18')

  return (
    <div className="page">
      <div className="page-head">
        <div>
          <h1>组件样张库</h1>
          <p>全部组件 · 全部状态 · 仅开发用（/dev/ui）</p>
        </div>
      </div>

      <Spec no="C/02" title="按钮（默认/禁用/加载/次级/迷你/危险）">
        <div className="fbar">
          <Button>主按钮</Button>
          <Button disabled>禁用</Button>
          <Button loading>扫描中…</Button>
          <Button variant="ghost">次级按钮</Button>
          <Button variant="mini">迷你按钮</Button>
          <Button variant="mini-danger">删除</Button>
        </div>
      </Spec>

      <Spec no="C/03" title="徽章（买点 + 状态，失效/失败带原因）">
        <div className="fbar">
          <BuyPointBadge p={1} />
          <BuyPointBadge p={2} />
          <BuyPointBadge p={3} />
          <BuyPointBadge p={2} short />
          <StatusBadge status="run" />
          <StatusBadge status="ok" />
          <StatusBadge status="fail" reason="行情超时" />
          <StatusBadge status="invalid" reason="回调放量" />
        </div>
      </Spec>

      <Spec no="C/04" title="环境横幅三态">
        <EnvBanner tone="strong" desc="上证 20 日线上方 · 市场宽度 0.66 · 可正常参与" time="09-19 17:32" />
        <EnvBanner tone="neutral" desc="上证 20 日线上方 · 市场宽度 0.58 · 正常评级" time="09-19 17:32" />
        <EnvBanner tone="weak" desc="上证跌破 20 日线 · 市场宽度 0.31 · 候选自动降级" time="09-19 17:32" />
      </Spec>

      <Spec no="C/05" title="筛选条（chip / select / search）">
        <FilterBar>
          <Chip active={chipOn} onClick={() => setChipOn(v => !v)}>全部买点</Chip>
          <Chip>P1 低吸</Chip>
          <Chip>P2 突破</Chip>
          <SelectPill label="参数组" value="normal_v1" />
          <SelectPill label="板块" value="全部" />
          <SearchPill />
        </FilterBar>
      </Spec>

      <Spec no="C/06" title="输入框（默认/错误/禁用/提示）">
        <div style={{ maxWidth: 520 }}>
          <TextField label="止损位" value="0.618" readOnly />
          <TextField label="持有天数" value={tfv} onChange={e => setTfv(e.target.value)} error={Number(tfv) > 15 ? '最大 15 天（需小于止盈天数 20）' : undefined} />
          <TextField label="禁用" value="7" disabled />
          <TextField label="带提示" defaultValue="20" hint="最大持有天数，到期按收盘价离场" readOnly />
        </div>
      </Spec>

      <Spec no="C/07" title="KPI 卡 · RPS 强度条 · 键值行">
        <div className="kpis">
          <KpiCard tone="a" icon={<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden><path d="M12 3l7 9-7 9-7-9z" /></svg>} label="今日候选" value="12" />
          <KpiCard tone="g" icon={<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden><path d="M12 2c.8 5.2 4.8 9.2 10 10-5.2.8-9.2 4.8-10 10-.8-5.2-4.8-9.2-10-10 5.2-.8 9.2-4.8 10-10z" /></svg>} label="主信号 P2" value="5" />
          <KpiCard tone="o" label="弱势降级" value="3" />
        </div>
        <div className="fbar">
          <RpsBar value={92} />
          <RpsBar value={70} />
          <RpsBar value={45} />
        </div>
        <div style={{ maxWidth: 360 }}>
          <div className="kv"><span className="k">第一波涨幅</span><span className="ok">+21.4% ✓</span></div>
          <div className="kv"><span className="k">止损参考</span><span className="mono" style={{ color: 'var(--ink)' }}>11.10（-1.6%）</span></div>
          <div className="kv"><span className="k">环境过滤</span><span className="no">✗ 弱势降级</span></div>
        </div>
      </Spec>

      <Spec no="C/08" title="数据表格（行高 40 · 行悬停 · 失效行 · 行展开）">
        <div className="table-panel">
          <table className="tb">
            <thead><tr><th>代码 / 名称</th><th>买点</th><th className="num">涨幅</th><th>RPS</th><th>状态</th></tr></thead>
            <tbody>
              <tr className="rowlink">
                <td><span className="code">600100</span><span className="name">示例股份</span></td>
                <td><BuyPointBadge p={2} short /></td>
                <td className="num up-text">+21.4%</td>
                <td><RpsBar value={92} /></td>
                <td className="muted-c">正常行 · 悬停蓝 7%</td>
              </tr>
              <tr className="invalid">
                <td><span className="code">300870</span><span className="name">示例样本</span></td>
                <td><BuyPointBadge p={1} short /></td>
                <td className="num up-text">+12.9%</td>
                <td><RpsBar value={71} /></td>
                <td><StatusBadge status="invalid" reason="回调放量" /></td>
              </tr>
              <tr className="expand">
                <td colSpan={5}>
                  <div className="expand-grid">
                    <div className="seg">
                      <div className="t">形态结构</div>
                      <div className="kv"><span className="k">A 段涨幅</span><span className="ok">+21.4% ✓</span></div>
                      <div className="kv"><span className="k">B 段回撤</span><span className="ok">-32% ✓</span></div>
                    </div>
                    <div className="seg">
                      <div className="t">量能结构</div>
                      <div className="kv"><span className="k">突破日量比</span><span className="ok">2.1 ✓</span></div>
                      <div className="kv"><span className="k">缩量回踩</span><span className="no">✗ 放量</span></div>
                    </div>
                    <div className="seg">
                      <div className="t">环境与板块</div>
                      <div className="kv"><span className="k">市场环境</span><span className="ok">强势 ✓</span></div>
                      <div className="kv"><span className="k">板块强度</span><span className="ok">RPS 82 ✓</span></div>
                    </div>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
          <div className="table-foot">
            共 12 条 · P2=5
            <span className="keys"><span><kbd>j</kbd>/<kbd>k</kbd> 行移动</span><span><kbd>Enter</kbd> 详情</span></span>
          </div>
        </div>
      </Spec>

      <Spec no="S/09" title="加载态（骨架屏 / 进度条 / 加载按钮）">
        <div style={{ maxWidth: 640, display: 'grid', gap: 14 }}>
          <Skeleton w="38%" />
          <Skeleton w="86%" />
          <Skeleton w="72%" />
        </div>
        <div className="fbar" style={{ marginTop: 14 }}>
          <Skeleton w={120} h={32} radius={999} />
          <Button loading>运行中…</Button>
          <div style={{ width: 200 }}><Progress value={38} /></div>
        </div>
      </Spec>

      <Spec no="S/10" title="空态（为什么空 + 怎么办）">
        <EmptyState
          title="当前条件下没有新候选"
          desc="常见于弱势环境或参数过紧。可适当放宽回撤位与量比要求，或回看历史信号。"
          actions={<><Button variant="ghost">放宽筛选</Button><Button variant="ghost">查看历史</Button></>}
        />
      </Spec>

      <Spec no="S/11" title="错误态（Toast / 页面级 / 断连点）">
        <div className="fbar">
          <Button variant="ghost" onClick={() => push('同步失败', '688120 行情服务器无响应，已重试 2 次')}>触发 Toast</Button>
          <span className="conn"><span className="dot" />已连接</span>
          <span className="conn off"><span className="dot" />已断开 · 重连中</span>
        </div>
        <div style={{ marginTop: 12 }}>
          <ErrorBlock title="页面加载失败" desc="候选数据请求超时（12s），可稍后重试" onRetry={() => push('已重试', '正在重新加载…')} />
        </div>
      </Spec>

      <Spec no="C/12" title="日志流（INFO/WARN/ERROR 着色）">
        <LogStream lines={demoLog} />
      </Spec>

      <Spec no="C/01" title="卡片（hoverable + 内高光）">
        <div className="split">
          <Card hoverable className="grow" >
            <CardHead title="玻璃卡片" sub="inset 高光 + hover 上浮 1px" />
            <div className="kv"><span className="k">边框</span><span>白 10% + 内高光 8%</span></div>
            <div className="kv"><span className="k">圆角</span><span className="mono">--r-card 16px</span></div>
          </Card>
          <Card hoverable>
            <CardHead title="指标小卡" sub="mcomp" />
            <div className="mcomp">
              <div className="c">胜率<b>58.3%</b></div>
              <div className="c">盈亏比<b>2.14</b></div>
              <div className="c">最大回撤<b>-9.6%</b></div>
            </div>
          </Card>
        </div>
      </Spec>

      <ToastStack toasts={toasts} onClose={dismiss} />
    </div>
  )
}
