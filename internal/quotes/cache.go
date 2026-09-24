// Package quotes keeps a periodically refreshed snapshot of the latest
// quotes for every known stock, so the stock browser can rank the whole
// market by price / change / turnover instead of a single page.
package quotes

import (
	"context"
	"log"
	"sync"
	"time"

	"xstock/internal/tdx"
)

// batch size matches tdx.Client.FetchQuotes cap.
const batch = 60

// maxFrames 保留的当日快照帧数（1 分钟一帧 ≈ 一个交易日的长度）。
const maxFrames = 480

// Frame 是某一时刻的全市场行情快照；当日滚动保留，跨日清零。
type Frame struct {
	At time.Time
	Q  map[string]tdx.Quote
}

type Cache struct {
	workers int
	clients []*tdx.Client

	mu      sync.RWMutex
	by      map[string]tdx.Quote
	updated time.Time
	frames  []Frame
	day     string
}

func New(workers int) *Cache {
	if workers < 1 {
		workers = 1
	}
	c := &Cache{workers: workers, by: map[string]tdx.Quote{}}
	c.clients = make([]*tdx.Client, workers)
	for i := range c.clients {
		c.clients[i] = tdx.New()
	}
	return c
}

// Run refreshes the whole market immediately and then on every tick until
// the context is cancelled. symbols is re-evaluated on each refresh so newly
// synced stocks are picked up automatically.
func (c *Cache) Run(ctx context.Context, symbols func() []string, every time.Duration) {
	c.refresh(symbols())
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh(symbols())
		}
	}
}

func (c *Cache) Close() {
	for _, cl := range c.clients {
		_ = cl.Close()
	}
}

func (c *Cache) refresh(symbols []string) {
	if len(symbols) == 0 {
		return
	}
	start := time.Now()
	merged := make(map[string]tdx.Quote, len(symbols))
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan []string)
	for i := 0; i < c.workers; i++ {
		wg.Add(1)
		go func(client *tdx.Client) {
			defer wg.Done()
			for batchSyms := range jobs {
				qs, err := client.FetchQuotes(batchSyms)
				if err != nil {
					continue // stale rows simply keep old values
				}
				mu.Lock()
				for _, q := range qs {
					merged[q.Symbol] = q
				}
				mu.Unlock()
			}
		}(c.clients[i])
	}
	for i := 0; i+batch <= len(symbols); i += batch {
		jobs <- symbols[i : i+batch]
	}
	if rest := len(symbols) % batch; rest != 0 {
		jobs <- symbols[len(symbols)-rest:]
	}
	close(jobs)
	wg.Wait()

	now := time.Now()
	c.mu.Lock()
	c.by = merged
	c.updated = now
	// 当日滚动帧：涨速榜、盘中触板等需要「N 分钟前」的数据。
	if day := now.Format("2006-01-02"); day != c.day {
		c.day, c.frames = day, nil
	}
	c.frames = append(c.frames, Frame{At: now, Q: merged})
	if len(c.frames) > maxFrames {
		c.frames = c.frames[len(c.frames)-maxFrames:]
	}
	c.mu.Unlock()
	log.Printf("行情缓存已刷新：%d 只，用时 %s", len(merged), time.Since(start).Round(time.Millisecond))
}

// Ready reports whether at least one refresh completed.
func (c *Cache) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.by) > 0
}

// Updated returns the time of the last successful refresh (zero if none).
func (c *Cache) Updated() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.updated
}

// Snapshot returns a copy of the current quote map.
func (c *Cache) Snapshot() map[string]tdx.Quote {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]tdx.Quote, len(c.by))
	for k, v := range c.by {
		out[k] = v
	}
	return out
}

// Frames 返回当日滚动快照序列的浅拷贝（帧内 map 不再变更，可安全读）。
func (c *Cache) Frames() []Frame {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Frame, len(c.frames))
	copy(out, c.frames)
	return out
}
