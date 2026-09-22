// Command bulksync loads A-share data from TDX into PostgreSQL in phases:
//
//	go run ./cmd/bulksync            # 列表 + 板块 + 概念 + 上市日期（基础档案）
//	go run ./cmd/bulksync --f10      # 追加 F10 业务文本（慢，可断点续跑）
//	go run ./cmd/bulksync --bars     # 拉取全部股票的前复权日K（跳过已有，断点续跑）
//
// Common flags: --workers N (并行连接数), --limit N (只处理前 N 只，测试用).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nstock/internal/config"
	"nstock/internal/market"
	"nstock/internal/store"
	"nstock/internal/tdx"
)

func first(list []string) string {
	if len(list) == 0 {
		return "(无)"
	}
	return list[0]
}

func main() {
	withF10 := flag.Bool("f10", false, "同时拉取每只股票的 F10 业务文本（慢，可断点续跑）")
	withBars := flag.Bool("bars", false, "拉取全部股票的前复权日K（跳过已有数据的代码，断点续跑）")
	workers := flag.Int("workers", 3, "并行连接通达信的工作协程数")
	limit := flag.Int("limit", 0, "最多处理 N 只（0=全部，测试用）")
	flag.Parse()

	if err := config.LoadEnv(); err != nil {
		log.Fatalf("load %s: %v — create it at the project root with the PostgreSQL settings", config.EnvFile, err)
	}
	dsn, err := config.PGDSN()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	// Lean mode: profiles mirror into memory but bar series do not — the
	// bars phase writes thousands of series and must not hold them all in RAM.
	st, err := store.OpenLean(ctx, dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if *workers < 1 {
		*workers = 1
	}
	// Only rotate over hosts that answer a probe; dead primaries make every
	// reconnect a coin flip into a wedged handshake.
	healthy := tdx.HealthyHosts()
	log.Printf("通达信主机池：%d/%d 台可达", len(healthy), len(tdx.AllHosts()))
	tdx.SetHostPool(healthy)

	clients := make([]*tdx.Client, *workers)
	for i := range clients {
		clients[i] = tdx.NewRotated(i)
	}
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()

	if *withBars {
		runBarsPhase(ctx, st, clients, *limit)
		return
	}

	// ---- Phase 1: full listings + concepts + IPO date ----
	log.Printf("拉取沪深北全量证券列表…")
	listings, err := clients[0].FetchAllStocks()
	if err != nil {
		log.Fatalf("拉取证券列表失败: %v", err)
	}
	conceptIndex, err := clients[0].FetchConceptIndex()
	if err != nil {
		log.Fatalf("拉取概念板块失败: %v", err)
	}
	log.Printf("共 %d 只股票；概念板块索引就绪", len(listings))

	start := time.Now()
	var mu sync.Mutex
	done, failed := 0, 0
	jobs := make(chan tdx.Listing)
	var wg sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(c *tdx.Client) {
			defer wg.Done()
			for l := range jobs {
				p := store.Profile{
					Symbol:   l.Symbol,
					Name:     l.Name,
					Market:   l.Market,
					Board:    l.Board,
					Concepts: conceptIndex[l.Code],
				}
				if d, err := c.FetchIPODate(l.Symbol); err == nil {
					p.ListDate = d
				}
				if err := st.SaveProfile(ctx, p); err != nil {
					mu.Lock()
					failed++
					if failed <= 5 {
						log.Printf("保存 %s 失败: %v", l.Symbol, err)
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				done++
				if done%500 == 0 {
					log.Printf("基础档案 %d/%d（%.0f 只/分钟）", done, len(listings), float64(done)/time.Since(start).Minutes())
				}
				mu.Unlock()
			}
		}(clients[i])
	}
	for _, l := range listings {
		jobs <- l
	}
	close(jobs)
	wg.Wait()
	log.Printf("基础档案完成：成功 %d，失败 %d，用时 %s", done, failed, time.Since(start).Round(time.Second))

	printBoardSummary(st)

	if !*withF10 {
		return
	}

	// ---- Phase 2 (--f10): business text for stocks still missing it ----
	var pending []store.Profile
	for _, p := range st.AllProfiles() {
		if p.Business == "" {
			pending = append(pending, p)
		}
	}
	log.Printf("F10 补全：待拉取 %d 只（已有业务文本的自动跳过）", len(pending))
	start = time.Now()
	done, failed = 0, 0
	jobs2 := make(chan store.Profile)
	var wg2 sync.WaitGroup
	for i := 0; i < *workers; i++ {
		wg2.Add(1)
		go func(c *tdx.Client) {
			defer wg2.Done()
			for p := range jobs2 {
				fp, err := c.FetchProfile(p.Symbol)
				if err != nil {
					mu.Lock()
					failed++
					if failed <= 10 {
						log.Printf("F10 %s 失败: %v", p.Symbol, err)
					}
					mu.Unlock()
					continue
				}
				if fp.Name != "" {
					p.Name = fp.Name
				}
				p.Industry = fp.Industry
				p.Business = fp.Business
				if p.ListDate == "" {
					p.ListDate = fp.ListDate
				}
				if len(fp.Concepts) > 0 {
					p.Concepts = fp.Concepts
				}
				if fp.Board != "" {
					p.Board = fp.Board
				}
				if err := st.SaveProfile(ctx, p); err != nil {
					mu.Lock()
					failed++
					mu.Unlock()
					continue
				}
				mu.Lock()
				done++
				if done%100 == 0 {
					log.Printf("F10 %d/%d（%.0f 只/分钟）", done, len(pending), float64(done)/time.Since(start).Minutes())
				}
				mu.Unlock()
			}
		}(clients[i])
	}
	for _, p := range pending {
		jobs2 <- p
	}
	close(jobs2)
	wg2.Wait()
	log.Printf("F10 补全完成：成功 %d，失败 %d，用时 %s（重跑 --f10 可重试失败项）", done, failed, time.Since(start).Round(time.Second))

	printBoardSummary(st)
}

// runBarsPhase downloads the full QFQ daily history for every profiled stock
// that does not have bars yet (resumable) and persists it without mirroring.
// NSTACK_SEC=秒数 在卡住时打印一次 goroutine 栈用于诊断。
func runBarsPhase(ctx context.Context, st *store.Store, clients []*tdx.Client, limit int) {
	existing, err := st.BarSymbols(ctx)
	if err != nil {
		log.Fatalf("查询已有K线失败: %v", err)
	}
	var pending []string
	for _, p := range st.AllProfiles() {
		if !existing[p.Symbol] {
			pending = append(pending, p.Symbol)
		}
	}
	if limit > 0 && limit < len(pending) {
		pending = pending[:limit]
	}
	log.Printf("日K同步：待拉取 %d 只（已有 %d 只自动跳过）首只=%s", len(pending), len(existing), first(pending))

	if v := os.Getenv("NSTACK_SEC"); v != "" {
		if sec, err := strconv.Atoi(v); err == nil && sec > 0 {
			go func() {
				time.Sleep(time.Duration(sec) * time.Second)
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				fmt.Fprintf(os.Stderr, "===== goroutine dump =====\n%s\n", buf[:min(n, 8000)])
				os.Exit(23)
			}()
		}
	}

	start := time.Now()
	var mu sync.Mutex
	done, failed, bars, timeouts := 0, 0, 0, 0
	// fetchGuard hard-caps one stock's fetch. Two layers:
	//  1. ErrClientWedged (handshake hang, 8s) — replace client, retry once;
	//  2. outer 30s cap — any other wedge, replace client, give up on stock.
	fetchGuard := func(c **tdx.Client, sym string) ([]market.Candle, error) {
		type res struct {
			bars []market.Candle
			err  error
		}
		run := func() <-chan res {
			ch := make(chan res, 1)
			go func() {
				b, e := (*c).FetchDailyQFQ(sym)
				ch <- res{b, e}
			}()
			return ch
		}
		select {
		case r := <-run():
			if errors.Is(r.err, tdx.ErrClientWedged) {
				*c = tdx.NewRotated(int(time.Now().UnixNano()))
				select {
				case r2 := <-run():
					return r2.bars, r2.err
				case <-time.After(30 * time.Second):
					*c = tdx.NewRotated(int(time.Now().UnixNano()))
					mu.Lock()
					timeouts++
					mu.Unlock()
					return nil, fmt.Errorf("重试后仍超时")
				}
			}
			return r.bars, r.err
		case <-time.After(30 * time.Second):
			mu.Lock()
			timeouts++
			mu.Unlock()
			*c = tdx.NewRotated(int(time.Now().UnixNano()))
			return nil, fmt.Errorf("拉取超时(30s)，已更换连接")
		}
	}
	jobs := make(chan string)
	var wg sync.WaitGroup
	for wi, cw := range clients {
		wg.Add(1)
		go func(c *tdx.Client, seed int) {
			defer wg.Done()
			consecutiveTimeouts := 0
			for sym := range jobs {
				candles, err := fetchGuard(&c, sym)
				if err != nil {
					if strings.Contains(err.Error(), "超时") {
						consecutiveTimeouts++
						if consecutiveTimeouts >= 15 {
							log.Printf("worker %d 连续 15 次超时，停止本轮（重跑 --bars 可续）", seed)
							return
						}
					} else {
						consecutiveTimeouts = 0
					}
					mu.Lock()
					failed++
					if failed <= 10 {
						log.Printf("日K %s 失败: %v", sym, err)
					}
					mu.Unlock()
					continue
				}
				consecutiveTimeouts = 0
				if err := st.Replace(ctx, sym, candles); err != nil {
					mu.Lock()
					failed++
					if failed <= 10 {
						log.Printf("保存 %s 失败: %v", sym, err)
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				done++
				bars += len(candles)
				if done%100 == 0 {
					rate := float64(done) / time.Since(start).Minutes()
					remain := time.Duration(float64(len(pending)-done-failed)/rate) * time.Minute
					log.Printf("日K %d/%d（%.0f 只/分钟，预计剩余 %s，累计 %d 根，超时 %d 次）", done, len(pending), rate, remain.Round(time.Minute), bars, timeouts)
				}
				mu.Unlock()
			}
		}(cw, wi)
	}
	for _, sym := range pending {
		jobs <- sym
	}
	close(jobs)
	wg.Wait()
	log.Printf("日K同步完成：成功 %d，失败 %d，共 %d 根，用时 %s（重跑 --bars 可重试失败项）", done, failed, bars, time.Since(start).Round(time.Second))
}

func printBoardSummary(st *store.Store) {
	counts := map[string]int{}
	for _, p := range st.AllProfiles() {
		counts[p.Board]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	total := 0
	for _, k := range keys {
		fmt.Printf("  %-8s %d\n", k, counts[k])
		total += counts[k]
	}
	fmt.Printf("  合计     %d\n", total)
}
