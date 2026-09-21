// Command bulksync loads every A-share stock (SH main board, STAR, SZ main
// board, ChiNext and BSE) from TDX into PostgreSQL: name, board, concepts
// and IPO date first (fast), then optionally F10 business text via --f10
// (slow, resumable — stocks that already have business text are skipped).
//
// Usage:
//
//	go run ./cmd/bulksync            # 列表 + 板块 + 概念 + 上市日期
//	go run ./cmd/bulksync --f10      # 追加 F10 业务文本（可断点续跑）
//	go run ./cmd/bulksync --f10 --workers 4
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"nstock/internal/config"
	"nstock/internal/store"
	"nstock/internal/tdx"
)

func main() {
	withF10 := flag.Bool("f10", false, "同时拉取每只股票的 F10 业务文本（慢，可断点续跑）")
	workers := flag.Int("workers", 3, "并行连接通达信的工作协程数")
	flag.Parse()

	if err := config.LoadEnv(); err != nil {
		log.Fatalf("load %s: %v — create it at the project root with the PostgreSQL settings", config.EnvFile, err)
	}
	dsn, err := config.PGDSN()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	if *workers < 1 {
		*workers = 1
	}
	clients := make([]*tdx.Client, *workers)
	for i := range clients {
		clients[i] = tdx.New()
	}
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()

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
