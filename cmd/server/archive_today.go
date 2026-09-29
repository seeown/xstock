package main

// archiveToday：收盘落库任务（替代 dailyupdate 的核心职能）。
//
// 交易日 15:05 后触发一次（服务内置定时 + 失败重试）：
//   1. 叠加层里的今日bar（锚定OK的全市场票）批量 upsert 进 PG；
//   2. 锚定失败（今日除权）的票走 FetchDailyQFQ 全量重拉替换——
//      QFQ 基准变了，历史必须整条重算；
//   3. 四个大盘指数走 FetchDailyIndex 增量补齐（快照通道对指数的
//      覆盖有限，指数日线保持原通道更稳）；
//   4. 落库后 mv 缓存失效 + 叠加层自然退役（明日开盘重建）。
//
// 幂等：ON CONFLICT DO NOTHING + Replace 全量替换，重复跑安全。

import (
	"context"
	"fmt"
	"log"
	"time"

	"xstock/internal/market"
	"xstock/internal/store"
	"xstock/internal/tdx"
)

type archiver struct {
	s      *store.Store
	ov     *overlay
	mv     *marketView
	source *tdx.Client
	done   string // 已完成落库的交易日（防重复）
}

func newArchiver(s *store.Store, ov *overlay, mv *marketView, source *tdx.Client) *archiver {
	return &archiver{s: s, ov: ov, mv: mv, source: source}
}

// run 常驻：交易时段结束后自动落库一次，失败每 10 分钟重试。
func (a *archiver) run(ctx context.Context) {
	for {
		now := time.Now()
		// 下一个检查点：还没到 15:05 等到 15:05；已过则等 10 分钟
		// （已落库的会因 done 标记跳过）。
		var wait time.Duration
		hm := now.Hour()*100 + now.Minute()
		weekday := now.Weekday() != time.Saturday && now.Weekday() != time.Sunday
		if weekday && hm < 1505 {
			target := time.Date(now.Year(), now.Month(), now.Day(), 15, 5, 0, 0, now.Location())
			wait = time.Until(target)
		} else {
			wait = 10 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if !weekday || time.Now().Hour()*100+time.Now().Minute() < 1505 {
			continue
		}
		day := time.Now().Format("2006-01-02")
		if a.done == day {
			continue
		}
		if err := a.archive(day); err != nil {
			log.Printf("收盘落库失败（10分钟后重试）: %v", err)
			continue
		}
		a.done = day
		log.Printf("收盘落库完成：%s", day)
	}
}

// archive 执行一次落库（幂等）。
func (a *archiver) archive(day string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1) 今日bar批量入库（叠加层只含锚定OK的票）。
	dayOf, count := a.ov.snapshotToday()
	if dayOf != day {
		return fmt.Errorf("叠加层交易日 %s 与今日 %s 不符（快照未刷新？）", dayOf, day)
	}
	if count == 0 {
		return fmt.Errorf("叠加层为空（快照未就绪）")
	}
	pairs := a.ov.exportToday()
	written := 0
	for _, p := range pairs {
		if p.bar.Date != day {
			continue
		}
		if _, err := a.s.AppendBars(ctx, p.sym, []market.Candle{p.bar}); err != nil {
			log.Printf("落库 %s: %v（跳过）", p.sym, err)
			continue
		}
		written++
	}

	// 2) 除权票全量QFQ重拉。
	rebased := 0
	for _, sym := range a.ov.pendingRebase() {
		fresh, err := a.source.FetchDailyQFQ(sym)
		if err != nil {
			log.Printf("除权重拉 %s 失败（跳过，明日 dailyupdate 兜底）: %v", sym, err)
			continue
		}
		if err := a.s.Replace(ctx, sym, fresh); err != nil {
			log.Printf("除权重拉 %s 写入失败: %v", sym, err)
			continue
		}
		rebased++
	}

	// 3) 大盘指数增量补齐（原通道，验证后写入）。
	for _, def := range tdx.IndexDefs {
		if bars := a.s.Bars(def.Symbol); len(bars) > 0 && bars[len(bars)-1].Date >= day {
			continue // 已最新
		}
		fresh, err := a.source.FetchDailyIndex(def)
		if err != nil {
			log.Printf("指数 %s 补齐失败（可在大盘页手动同步）: %v", def.Name, err)
			continue
		}
		if err := a.s.Replace(ctx, def.Symbol, fresh); err != nil {
			log.Printf("指数 %s 写入失败: %v", def.Name, err)
		}
	}

	log.Printf("收盘落库：%d 只今日bar，%d 只除权全量重拉", written, rebased)
	if a.mv != nil {
		a.mv.invalidate() // 日线已含今日，情绪/板块缓存失效重算
	}
	return nil
}
