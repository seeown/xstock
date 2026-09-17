# NStock MVP

一个 Go 实现的 N 字战法筛选和日线回测 API。当前使用内存数据，启动时含有 `DEMO` 演示标的；真实项目中可将行情导入接口连接到合规数据源和 PostgreSQL。

## 运行

```bash
go run ./cmd/server
```

打开浏览器访问 `http://localhost:8080/`，即可使用网页仪表盘修改策略参数、运行回测，并查看价格走势、N 字信号和交易明细。

默认参数：

```bash
curl http://localhost:8080/api/params/default
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
