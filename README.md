# NStock MVP

一个 Go 实现的 N 字战法筛选和日线回测 API。内置 `DEMO` 演示标的；支持通过通达信行情协议拉取沪深股票的**前复权日线**并持久化到本地 PostgreSQL，重启后自动加载。

## 运行

```bash
go run ./cmd/server
```

打开浏览器访问 `http://localhost:8080/`，即可使用网页仪表盘修改策略参数、运行回测，并查看价格走势、N 字信号和交易明细。

### 前端

网页使用 React + Vite 构建（源码在 `web/`），构建产物已提交到 `cmd/server/web/dist`，因此直接 `go run ./cmd/server` 即可运行完整应用。

前端开发热更新：

```bash
cd web
npm install
npm run dev   # http://localhost:5173，API 自动代理到 :8080
```

重新构建生产产物：

```bash
cd web
npm run build   # 输出到 cmd/server/web/dist
```

### 真实数据

在仪表盘输入股票代码（如 `600519.SH`、`000001.SZ`），点击「同步真实数据」；或调用同步接口：

```bash
curl -X POST http://localhost:8080/api/sync/600519.SH
```

数据通过 `gotdx` 连接通达信行情服务器获取全量前复权日K，写入本地 PostgreSQL（建表自动完成）。前复权数据会随分红除权整体重算，因此每次同步都全量替换该股票的本地序列，避免新旧复权基准混用。服务地址可用 `NSTOCK_ADDR` 修改（默认 `:8080`）。

数据库连接配置在项目根目录的 `.env.produce` 文件中（不提交到 git），程序启动时读取；已在进程环境中设置的变量优先于该文件：

```bash
PG_USER=wyf
PG_PASSWD=password
PG_HOST=127.0.0.1
PG_PORT=5432
DB_NAME=nstock
```

任一变量缺失时服务会拒绝启动。

默认参数：

```bash
curl http://localhost:8080/api/params/default
```

查看大盘指数（网页「大盘行情」页会自动同步并展示 K 线、MA5、MA10）：

```bash
curl http://localhost:8080/api/market/indices
curl -X POST http://localhost:8080/api/sync/000001.SH   # 上证指数
```

筛选演示信号：

```bash
curl http://localhost:8080/api/stocks/DEMO/signals
```

获取仪表盘使用的日线数据：

```bash
curl http://localhost:8080/api/stocks/DEMO/bars
```

执行回测：

```bash
curl -X POST http://localhost:8080/api/backtests/DEMO \
  -H 'Content-Type: application/json' \
  -d '{"initialCash":100000,"params":{"riseDays":10,"riseMinPct":15,"pullbackMinDays":3,"pullbackMaxDays":10,"pullbackMaxPct":8,"volumeRatioMin":1.5,"breakoutBufferPct":0,"stopLossPct":7,"takeProfitPct":15,"maxHoldDays":20}}'
```

导入某一股票的复权日线（按日期升序；服务也会自行排序）：

```bash
curl -X POST http://localhost:8080/api/bars/600519.SH \
  -H 'Content-Type: application/json' \
  -d '[{"date":"2025-01-02","open":1500,"high":1520,"low":1490,"close":1510,"volume":123456}]'
```

## 策略定义

一次信号依次要求：固定窗口上涨、回调不超过最大幅度且不跌破起涨价、回调均量小于上涨均量、收盘突破前高并且突破日相对回调均量放大。信号收盘后产生，回测使用**下一交易日开盘价**买入，规避未来函数。

这是研究工具，不构成投资建议。接入真实数据前，应补充交易费用、滑点、涨跌停和停牌逻辑，并使用包含退市证券的历史股票池以降低幸存者偏差。
