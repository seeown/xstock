// Command dailyupdate runs the nightly incremental sync: probe every stock's
// newest page once, append new trading days, fully re-fetch stocks whose QFQ
// series was rebased by a dividend, sync the four market-page indices, then
// refresh names / new listings / concepts. Designed to be scheduled (launchd)
// at 17:00 on trading days.
//
// Anti-ban pacing: a single probe request per stock, 2 workers with a short
// sleep between stocks, an early trading-day check that exits on holidays,
// and a circuit breaker that aborts after 40 consecutive failures.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"nstock/internal/config"
	"nstock/internal/market"
	"nstock/internal/store"
	"nstock/internal/tdx"
)

const (
	indexProbeSymbol = "000001.SH" // 上证指数
	workers          = 2
	pausePerStock    = 120 * time.Millisecond // gentle pacing between requests
	maxConsecFails   = 40                     // circuit breaker

	// Manual mid-day runs must not ingest live intraday bars; the scheduled
	// 17:00 job always runs after this cutoff.
	cutoffHour, cutoffMin = 15, 5
)

var shanghai = mustShanghai()

func mustShanghai() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic(err)
	}
	return loc
}

func main() {
	force := flag.Bool("force", false, "跳过交易日判断（测试用；收盘保护仍然生效）")
	flag.Parse()

	if err := config.LoadEnv(); err != nil {
		log.Fatalf("load %s: %v", config.EnvFile, err)
	}
	dsn, err := config.PGDSN()
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now().In(shanghai)
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	if now.Hour() < cutoffHour || (now.Hour() == cutoffHour && now.Minute() < cutoffMin) {
		log.Fatalf("现在是 %s（盘中/未收盘），拒绝更新以避免写入实时半日K线；定时任务于交易日 17:00 运行", now.Format("15:04"))
	}

	ctx := context.Background()
	st, err := store.OpenLean(ctx, dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	healthy := tdx.HealthyHosts()
	log.Printf("通达信主机池：%d 台可达", len(healthy))
	tdx.SetHostPool(healthy)
	clients := []*tdx.Client{tdx.NewRotated(0), tdx.NewRotated(1)}
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()

	// ---- Trading-day gate. Stock probes are the reliable signal; TDX's
	// index kline endpoint intermittently returns byte-misaligned data, so
	// the index is only a last-resort candidate. ----
	lastBars, err := st.LastBars(ctx)
	if err != nil {
		log.Fatalf("读取本地K线状态失败: %v", err)
	}
	tradingDay, gateSymbol := "", ""
	for _, cand := range []string{"600519.SH", "000001.SZ", "300750.SZ", indexProbeSymbol} {
		probe, err := clients[0].FetchRecentDaily(cand)
		if err != nil || len(probe) == 0 {
			continue
		}
		newest := probe[len(probe)-1].Date
		if newest > tomorrow || newest < "2020-01-01" {
			continue // garbage probe
		}
		tradingDay, gateSymbol = newest, cand
		break
	}
	if tradingDay == "" {
		log.Fatalf("无法获得有效的交易日探测结果，本次退出（不写任何数据）")
	}
	// The gate symbol's freshness says nothing about the whole market (a user
	// or the market page may have synced that one symbol); only sync_state
	// records whether this job actually completed for the trading day.
	if !*force {
		if state, err := st.GetSyncState(ctx); err == nil && state.LastTradingDay >= tradingDay {
			// 个股已同步过；指数可能在上次运行时拉取失败，补齐后再退出
			// （已最新的指数会被跳过，通常零请求）。
			updateIndices(ctx, st, clients[0], lastBars, tradingDay, tomorrow)
			log.Printf("%s 的增量更新已完成过（sync_state），本次结束", tradingDay)
			return
		}
	}
	log.Printf("交易日 %s（由 %s 探测）：开始增量同步", tradingDay, gateSymbol)

	// ---- Bars: probe each stock once, append new days, re-fetch rebases.
	// The market-page indices are synced right after (updateIndices): TDX's
	// index kline endpoint has served misaligned garbage on some hosts, so
	// each index fetch is validated before writing and never blocks stocks.
	// ----
	symbols := make([]string, 0, len(st.AllProfiles()))
	for _, p := range st.AllProfiles() {
		symbols = append(symbols, p.Symbol)
	}

	var mu sync.Mutex
	state := store.SyncState{LastTradingDay: tradingDay}
	consecFails := 0
	abort := false
	start := time.Now()

	jobs := make(chan string)
	var wg sync.WaitGroup
	for wi, cw := range clients {
		wg.Add(1)
		go func(c *tdx.Client, seed int) {
			defer wg.Done()
			for sym := range jobs {
				if abortNow(&mu, &abort) {
					return
				}
				n, full, err := updateOne(ctx, st, c, sym, lastBars[sym].Date, lastBars[sym].Close, tomorrow, &mu)
				if err != nil {
					mu.Lock()
					consecFails++
					if consecFails <= 10 {
						log.Printf("%s 更新失败: %v", sym, err)
					}
					if consecFails >= maxConsecFails {
						abort = true
						log.Printf("连续 %d 次失败，熔断退出（明日自动重试）", consecFails)
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				consecFails = 0
				state.StocksProbed++
				state.BarsAppended += n
				if full {
					state.FullRefetch++
				}
				if state.StocksProbed%500 == 0 {
					log.Printf("K线 %d/%d（%.0f 只/分钟）", state.StocksProbed, len(symbols), float64(state.StocksProbed)/time.Since(start).Minutes())
				}
				mu.Unlock()
				time.Sleep(pausePerStock + time.Duration(rand.Intn(80))*time.Millisecond)
				_ = seed
			}
		}(cw, wi)
	}
	for _, sym := range symbols {
		if abortNow(&mu, &abort) {
			break
		}
		jobs <- sym
	}
	close(jobs)
	wg.Wait()
	if abort {
		log.Fatalf("熔断退出，本次不写 sync_state（明日重试）")
	}
	log.Printf("K线增量完成：探测 %d 只，追加 %d 根，全量重建 %d 只，用时 %s",
		state.StocksProbed, state.BarsAppended, state.FullRefetch, time.Since(start).Round(time.Second))

	// ---- Indices: bring the market-page watchlist to the trading day. ----
	updateIndices(ctx, st, clients[0], lastBars, tradingDay, tomorrow)

	// ---- Stock info: names / boards diff, new listings, concepts. ----
	refreshListings(ctx, st, clients[0], &state)

	if err := st.SetSyncState(ctx, state); err != nil {
		log.Printf("写 sync_state 失败: %v", err)
	}
	log.Printf("每日增量同步完成： %+v", state)
	_ = today
}

// updateOne probes one symbol and applies the minimal write:
// nothing (up to date / suspended), append (new days, no rebase), or a full
// rebuild (QFQ rebase, unknown series, or garbage probe). Returns
// (barsAdded, didFull, err).
func updateOne(ctx context.Context, st *store.Store, c *tdx.Client, sym, lastDate string, lastClose float64, tomorrow string, mu *sync.Mutex) (int, bool, error) {
	recent, err := c.FetchRecentDaily(sym)
	if err != nil {
		return 0, false, err
	}
	if len(recent) == 0 {
		return 0, false, nil // 无数据（如次新北交所股），跳过
	}
	newest := recent[len(recent)-1]
	if newest.Date > tomorrow {
		return 0, false, fmt.Errorf("探测日期异常 %s，拒绝写入", newest.Date)
	}

	if lastDate == "" {
		// Unknown series (new listing, or indices wiped after corruption):
		// rebuild the full history rather than seeding from the probe page.
		full, err := c.FetchDailyQFQ(sym)
		if err != nil {
			return 0, false, err
		}
		if full[len(full)-1].Date > tomorrow {
			return 0, false, fmt.Errorf("全量日期异常，拒绝写入")
		}
		return 0, true, st.Replace(ctx, sym, full)
	}
	if newest.Date <= lastDate {
		return 0, false, nil // 已最新（含停牌）
	}
	// Rebase check: the stored last day must appear in the probe page with
	// the same close; a mismatch means a dividend re-based all history.
	rebased := true
	for _, b := range recent {
		if b.Date == lastDate {
			rebased = abs(b.Close-lastClose) > 1e-6
			break
		}
	}
	if rebased {
		full, err := c.FetchDailyQFQ(sym)
		if err != nil {
			return 0, false, err
		}
		if full[len(full)-1].Date > tomorrow {
			return 0, false, fmt.Errorf("全量日期异常，拒绝写入")
		}
		return 0, true, st.Replace(ctx, sym, full)
	}
	var fresh []market.Candle
	for _, b := range recent {
		if b.Date > lastDate && b.Date <= tomorrow {
			fresh = append(fresh, b)
		}
	}
	n, err := st.AppendBars(ctx, sym, fresh)
	return n, false, err
}

// updateIndices syncs the market-page index watchlist to the trading day.
// TDX's index kline endpoint intermittently returns byte-misaligned data, so
// every fetch is validated before writing: dates must fall in 1991~tomorrow
// and be strictly increasing, prices positive, and the stored last day must
// reappear with a matching close (indices carry no adjustment factors). A
// rejected or failed index is skipped with a log line — it never blocks the
// stock sync and the market page can still restore it via /api/sync.
func updateIndices(ctx context.Context, st *store.Store, c *tdx.Client, lastBars map[string]struct {
	Date  string
	Close float64
}, tradingDay, tomorrow string) {
	for _, def := range tdx.IndexDefs {
		last := lastBars[def.Symbol]
		if last.Date >= tradingDay {
			continue // 已最新（上次运行补齐过）
		}
		bars, err := c.FetchDailyIndex(def)
		if err != nil {
			log.Printf("指数 %s 拉取失败（跳过，可在大盘页手动同步）: %v", def.Name, err)
			continue
		}
		if !saneIndexBars(bars, tomorrow) {
			log.Printf("指数 %s 数据异常（日期/价格越界或乱序），拒绝写入", def.Name)
			continue
		}
		if last.Date == "" {
			if err := st.Replace(ctx, def.Symbol, bars); err != nil {
				log.Printf("指数 %s 全量写入失败: %v", def.Name, err)
				continue
			}
			log.Printf("指数 %s 全量写入 %d 根（至 %s）", def.Name, len(bars), bars[len(bars)-1].Date)
			continue
		}
		overlapOK := false
		for _, b := range bars {
			if b.Date == last.Date {
				overlapOK = abs(b.Close-last.Close) <= last.Close*0.005
				break
			}
		}
		if !overlapOK {
			log.Printf("指数 %s 重叠日 %s 收盘与本地不一致，拒绝写入（疑似错位）", def.Name, last.Date)
			continue
		}
		var fresh []market.Candle
		for _, b := range bars {
			if b.Date > last.Date {
				fresh = append(fresh, b)
			}
		}
		n, err := st.AppendBars(ctx, def.Symbol, fresh)
		if err != nil {
			log.Printf("指数 %s 追加失败: %v", def.Name, err)
			continue
		}
		log.Printf("指数 %s 追加 %d 根（至 %s）", def.Name, n, bars[len(bars)-1].Date)
	}
}

// saneIndexBars 防御 TDX 指数接口的字节错位垃圾数据。
func saneIndexBars(bars []market.Candle, tomorrow string) bool {
	prev := ""
	for _, b := range bars {
		if b.Date < "1991-01-01" || b.Date > tomorrow || b.Date <= prev {
			return false
		}
		if b.Open <= 0 || b.High <= 0 || b.Low <= 0 || b.Close <= 0 {
			return false
		}
		prev = b.Date
	}
	return true
}

// refreshListings diffs the exchange security lists against stored profiles:
// renames get updated, new symbols get a full profile (F10 + concepts), and
// concept memberships are rebuilt for symbols whose set changed.
func refreshListings(ctx context.Context, st *store.Store, c *tdx.Client, state *store.SyncState) {
	listings, err := c.FetchAllStocks()
	if err != nil {
		log.Printf("证券列表刷新失败（跳过）: %v", err)
		return
	}
	conceptIdx, err := c.FetchConceptIndex()
	if err != nil {
		log.Printf("概念板块刷新失败（跳过）: %v", err)
		conceptIdx = nil
	}
	profiles := map[string]store.Profile{}
	for _, p := range st.AllProfiles() {
		profiles[p.Symbol] = p
	}
	for _, l := range listings {
		p, known := profiles[l.Symbol]
		if !known {
			fp, err := c.FetchProfile(l.Symbol)
			if err != nil {
				log.Printf("新股 %s 档案拉取失败: %v", l.Symbol, err)
				continue
			}
			concepts := conceptIdx[l.Code]
			if concepts == nil {
				concepts = []string{}
			}
			if err := st.SaveProfile(ctx, store.Profile{
				Symbol: l.Symbol, Name: l.Name, Industry: fp.Industry, Market: l.Market,
				Board: l.Board, ListDate: fp.ListDate, Business: fp.Business, Concepts: concepts,
			}); err != nil {
				log.Printf("保存新股 %s 失败: %v", l.Symbol, err)
				continue
			}
			state.NewListings++
			continue
		}
		changed := p.Name != l.Name || p.Board != l.Board
		var newConcepts []string
		if conceptIdx != nil {
			want := conceptIdx[l.Code]
			if want == nil {
				want = []string{}
			}
			if !sameSet(p.Concepts, want) {
				newConcepts = want
				changed = true
			}
		}
		if changed {
			p.Name = l.Name
			p.Board = l.Board
			if newConcepts != nil {
				p.Concepts = newConcepts
			}
			if err := st.SaveProfile(ctx, p); err != nil {
				log.Printf("更新 %s 档案失败: %v", l.Symbol, err)
			}
		}
	}
	log.Printf("列表刷新完成：新股 %d 只", state.NewListings)
}

func abortNow(mu *sync.Mutex, abort *bool) bool {
	mu.Lock()
	defer mu.Unlock()
	return *abort
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
