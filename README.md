# xstock · 战法研究的交易魔方

**xstock 是一个交易系统打造工具**：把「行情数据管道 → 策略引擎 → 回测验证 → 终端可视化」整条链路装进一个可本地运行的应用，用来持续打磨交易战法。

当前围绕 **N 字战法**（首板涨停 → 缩量回调 → 放量突破）深度打磨——这只是第一个核心战法，架构上策略引擎已分层（`internal/market/`），后续会有更多战法进来：首板回调已经在侧，打板接力、趋势突破等都在规划中（见[路线图](#路线图)）。

> 这是研究工具，不构成投资建议。

## 功能一览

| 模块 | 说明 |
|---|---|
| 股票筛查 | 全市场 N 字形态扫描，B1 回调低吸 / B2 放量突破 / B3 回踩确认三阶段，环境横幅 + KPI + 行展开三段明细 + j/k 键盘操作 |
| 回测分析 | 双策略（通用 N 字 / 首板回调）参数回测：K 线买卖点标注、资金曲线（权益阶梯 + 回撤区间）、参数行内校验、模拟交易明细 |
| 个股行情 | A 股全市场浏览：板块/行业/概念筛选、档案抽屉（信号明细逐项打勾 + 板块强度）、一键同步日 K |
| 大盘行情 | 指数 K 线 + MA5/10/20/30/60/年线(250) + 成交量 |
| 情绪指南 | 大盘温度计（五档色带仪表盘）、涨停/炸板/量能双轴走势、情绪温度分历史 |
| 策略说明 | 战法规则、参数组管理（存 PostgreSQL，内置只读可另存；**设为默认后回测页加载即采用**）、API 一览 |
| 数据管理 | 同步状态、序列管理、增量更新 |

## 系统架构

```
通达信行情服务器（gotdx 协议，免费）
        │
        ├── cmd/bulksync      全量入库：基础档案 → F10 → 前复权日K（断点续跑）
        └── cmd/dailyupdate   每日增量：launchd 工作日 17:00 调度（除权重拉、熔断防封）
        ▼
PostgreSQL（日K / 档案 / 概念 / 上市信息；表结构自动创建）
        ▼（内存镜像缓存，lock-free 读）
internal/market 策略引擎层
        ├── npattern   N 字战法引擎（形态判定核心）
        ├── zt         涨停统计          ├── ladder   连板梯队
        ├── sentiment  大盘情绪          └── market   市场宽度
        ▼
cmd/server（REST API + 内嵌前端）
        ▼
React SPA（Vite + klinecharts v10，「极光深空」设计系统）
```

## 环境要求

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | ≥ 1.26 | `go.mod` 要求 1.26.1；本机工具链较旧时用 `GOTOOLCHAIN=auto` |
| Node.js | ≥ 18 | 仅改前端时需要（v22 实测可用） |
| PostgreSQL | ≥ 14 | 本地实例即可 |

## 快速开始

### 1. 准备数据库

表结构由程序自动创建，只需先建库（库名与 `.env.produce` 中 `DB_NAME` 一致）：

```bash
createdb nstock
```

### 2. 配置连接

项目根目录创建 `.env.produce`（不提交到 git）：

```bash
PG_USER=<你的数据库用户名>
PG_PASSWD=<你的数据库密码>
PG_HOST=127.0.0.1
PG_PORT=5432
DB_NAME=nstock

# 可选：HTTP 监听地址（默认 :8080）
#XSTOCK_ADDR=:8080
```

任一必需变量缺失时服务会拒绝启动。

### 3. 启动

```bash
go run ./cmd/server        # 工具链较旧时：GOTOOLCHAIN=auto go run ./cmd/server
```

打开 `http://localhost:8080/`。内置 `DEMO` 演示标的，开箱即可体验回测全流程（参数 → 信号 → K 线买卖点 → 交易明细）。

### 4. 接入真实数据

单票同步（网页「数据管理」页点击，或）：

```bash
curl -X POST http://localhost:8080/api/sync/600519.SH
curl -X POST http://localhost:8080/api/sync/000001.SH        # 上证指数
curl -X POST http://localhost:8080/api/profile/sync/600519.SH # 个股档案(F10/行业/概念)
```

前复权数据会随分红除权整体重算，因此每次同步都全量替换该股票的本地序列，避免新旧复权基准混用。

全市场入库（筛查页需要）：

```bash
go run ./cmd/bulksync           # 基础档案 + 上市信息
go run ./cmd/bulksync -bars     # 全部前复权日K（断点续跑，中断重跑即续）
go run ./cmd/bulksync -f10      # F10 业务文本（慢，断点续跑）
go run ./cmd/bulksync -workers 5 -limit 50   # 并发与限量（测试用）
```

## 前端

源码在 `web/`（React 18 + Vite + klinecharts v10，无 UI 框架，手写「极光深空」设计系统：token 层 + `components/ui/` 组件库）。**构建产物随仓库提交**到 `cmd/server/web/dist`，并经 `go:embed` 编译进二进制——直接 `go run ./cmd/server` 即是完整应用。

```bash
cd web
npm install
npm run dev     # http://localhost:5173，API 自动代理到 :8080
npm run build   # 输出到 cmd/server/web/dist
```

> 改完前端后：`npm run build` 之外还需重新编译并重启 server（`go build -o xstock-server ./cmd/server`），embed 的产物才会更新。组件样张路由 `/dev/ui` 可逐状态核对全部组件。

## 每日数据更新

macOS 下用 launchd 在工作日 17:00 自动增量更新（单票探测、除权重拉、指数同步、档案刷新，防封 pacing + 连续失败熔断）：

```bash
# 先把 plist 里的脚本路径改成你的仓库绝对路径
cp scripts/com.xstock.dailyupdate.plist ~/Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.xstock.dailyupdate.plist
```

手动补跑：`bash scripts/dailyupdate.sh`（日志在 `logs/daily-YYYY-MM-DD.log`）。更新完成后会自动热刷新运行中服务的缓存。

## API 一览（22 个端点）

| 组 | 端点 | 说明 |
|---|---|---|
| 市场 | `GET /api/market/indices·sectors·sentiment·guide` | 指数、板块、情绪、大盘指南 |
| 个股 | `GET /api/stocks`、`/api/stocks/{symbol}/bars·profile·signals`、`GET /api/quotes` | 股票池、日线、档案、信号、实时报价 |
| 筛选 | `GET /api/screen`、`GET /api/concepts·industries` | N 字全市场筛查与概念/行业过滤 |
| 回测 | `POST /api/backtests/{symbol}`、`GET /api/params/default`、`GET/POST /api/params/sets`、`PUT/DELETE /api/params/sets/{id}`、`POST /api/params/sets/{id}/default`、`POST /api/params/default/clear` | 回测执行、默认参数（库默认优先、内置兜底）、参数组管理 |
| 数据 | `POST /api/sync/{symbol}`、`POST /api/bars/{symbol}`、`POST /api/profile/sync/{symbol}`、`GET /api/sync-state` | 同步、导入、状态 |
| 系统 | `GET /health`、`POST /api/admin/reload` | 健康检查、缓存热刷新 |

示例——回测：

```bash
curl -X POST http://localhost:8080/api/backtests/600519.SH \
  -H 'Content-Type: application/json' \
  -d '{"initialCash":100000,"params":{"riseDays":10,"riseMinPct":15,"pullbackMinDays":3,"pullbackMaxDays":10,"pullbackMaxPct":8,"volumeRatioMin":1.5,"breakoutBufferPct":0,"stopLossPct":7,"takeProfitPct":15,"maxHoldDays":20}}'
```

导入自定义日线（按日期升序，服务会自行排序）：

```bash
curl -X POST http://localhost:8080/api/bars/600519.SH \
  -H 'Content-Type: application/json' \
  -d '[{"date":"2025-01-02","open":1500,"high":1520,"low":1490,"close":1510,"volume":123456}]'
```

## 策略：N 字战法

一次信号依次要求：**固定窗口上涨 → 缩量回调**（天数与幅度受限、不跌破起涨价、回调均量小于上涨均量）**→ 放量突破**（收盘突破前高且量比达标）。信号收盘后产生，回测按**下一交易日开盘价**买入，规避未来函数；止损/止盈/最长持仓三规则离场，单仓位 MVP。

网页「策略说明」页有完整的三段式规则拆解与参数组管理。

## 项目结构

```
cmd/
  server/        HTTP 服务 + 内嵌前端（main.go 路由，marketview.go 大盘视图）
  bulksync/      全量入库 CLI（三阶段、断点续跑）
  dailyupdate/   每日增量 CLI（launchd 调度）
internal/
  market/        策略引擎：npattern / zt / ladder / sentiment / market
  store/         PostgreSQL 存储 + 内存镜像（lean 模式）
  tdx/           通达信连接器族（日K / 档案 / 上市 / 指数 / 行情）
  quotes/        实时行情缓存
  config/        .env.produce 配置加载
web/             React SPA（pages / components/ui / hooks）
scripts/         每日更新脚本 + launchd plist
```

## 路线图

xstock 的定位是**交易系统打造工具**，N 字战法是第一块打磨中的核心拼图：

- **更多战法**：首板回调（已内置）之后，规划打板接力、趋势突破、龙头战法等，共用同一条数据管道与回测框架
- **回测可信度**：手续费、滑点、涨跌停无法成交、停牌处理
- **参数批量寻优**：网格扫参 CLI（复用 bulksync 的断点续跑模式）
- **信号快照与归因**：候选入选时的判定明细持久化，回看「当时为什么入选」
- **纸面交易**：模拟持仓跟踪信号收益，接实盘前的全部演练

## 风险提示

这是研究工具，不构成投资建议。接入真实数据前，应补充交易费用、滑点、涨跌停和停牌逻辑，并使用包含退市证券的历史股票池以降低幸存者偏差。
