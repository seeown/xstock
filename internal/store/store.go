// Package store keeps daily bars in PostgreSQL and mirrors them into an
// in-memory cache for lock-free reads by the strategy engine.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nstock/internal/market"
)

type Store struct {
	db *sql.DB

	mu       sync.RWMutex
	bars     map[string][]market.Candle
	profiles map[string]Profile

	// lean disables the in-memory bar mirror; bulk tools write thousands of
	// series and would otherwise hold the whole market (~GBs) in RAM.
	lean bool
}

// SymbolInfo describes one cached symbol series for the data-management page.
type SymbolInfo struct {
	Symbol    string `json:"symbol"`
	Count     int    `json:"count"`
	FirstDate string `json:"firstDate"`
	LastDate  string `json:"lastDate"`
}

// Profile is the slow-changing metadata of one stock: who they are and what
// they do, as opposed to bars which change every trading day.
type Profile struct {
	Symbol    string   `json:"symbol"`
	Name      string   `json:"name"`
	Industry  string   `json:"industry"`
	Market    string   `json:"market"`
	Board     string   `json:"board"`
	ListDate  string   `json:"listDate"`
	Business  string   `json:"business"`
	Concepts  []string `json:"concepts"`
	UpdatedAt string   `json:"updatedAt"`
}

// Open opens the PostgreSQL database at dsn (URL or key=value form) and loads
// all persisted bars and profiles into memory. Example dsn:
// "postgres://wyf:password@127.0.0.1:5432/nstock".
func Open(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, false)
}

// OpenLean opens the store for bulk writing: profiles are mirrored as usual,
// but bar series are neither loaded at start nor mirrored on Replace.
func OpenLean(ctx context.Context, dsn string) (*Store, error) {
	return open(ctx, dsn, true)
}

func open(ctx context.Context, dsn string, lean bool) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS bars (
		symbol TEXT NOT NULL,
		date   TEXT NOT NULL,
		open   DOUBLE PRECISION NOT NULL,
		high   DOUBLE PRECISION NOT NULL,
		low    DOUBLE PRECISION NOT NULL,
		close  DOUBLE PRECISION NOT NULL,
		volume DOUBLE PRECISION NOT NULL,
		PRIMARY KEY (symbol, date)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create bars table: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS stock_profile (
		symbol     TEXT PRIMARY KEY,
		name       TEXT NOT NULL DEFAULT '',
		industry   TEXT NOT NULL DEFAULT '',
		market     TEXT NOT NULL DEFAULT '',
		board      TEXT NOT NULL DEFAULT '',
		list_date  TEXT NOT NULL DEFAULT '',
		business   TEXT NOT NULL DEFAULT '',
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create stock_profile table: %w", err)
	}
	// Earlier deployments created the table without the board column.
	if _, err := db.ExecContext(ctx, `ALTER TABLE stock_profile ADD COLUMN IF NOT EXISTS board TEXT NOT NULL DEFAULT ''`); err != nil {
		db.Close()
		return nil, fmt.Errorf("add board column: %w", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS stock_concept (
		symbol  TEXT NOT NULL,
		concept TEXT NOT NULL,
		PRIMARY KEY (symbol, concept)
	)`); err != nil {
		db.Close()
		return nil, fmt.Errorf("create stock_concept table: %w", err)
	}
	s := &Store{db: db, lean: lean, bars: map[string][]market.Candle{}, profiles: map[string]Profile{}}
	if lean {
		if err := s.loadProfiles(ctx); err != nil {
			db.Close()
			return nil, err
		}
		return s, nil
	}
	if err := s.loadAll(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) loadAll(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, date, open, high, low, close, volume FROM bars ORDER BY symbol, date`)
	if err != nil {
		return fmt.Errorf("load bars: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var symbol string
		var c market.Candle
		if err := rows.Scan(&symbol, &c.Date, &c.Open, &c.High, &c.Low, &c.Close, &c.Volume); err != nil {
			return fmt.Errorf("scan bar: %w", err)
		}
		s.bars[symbol] = append(s.bars[symbol], c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.loadProfiles(ctx)
}

func (s *Store) loadProfiles(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT symbol, name, industry, market, board, list_date, business, to_char(updated_at, 'YYYY-MM-DD HH24:MI') FROM stock_profile ORDER BY symbol`)
	if err != nil {
		return fmt.Errorf("load profiles: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.Symbol, &p.Name, &p.Industry, &p.Market, &p.Board, &p.ListDate, &p.Business, &p.UpdatedAt); err != nil {
			return fmt.Errorf("scan profile: %w", err)
		}
		p.Concepts = []string{}
		s.profiles[p.Symbol] = p
	}
	if err := rows.Err(); err != nil {
		return err
	}
	crews, err := s.db.QueryContext(ctx, `SELECT symbol, concept FROM stock_concept ORDER BY symbol, concept`)
	if err != nil {
		return fmt.Errorf("load concepts: %w", err)
	}
	defer crews.Close()
	for crews.Next() {
		var symbol, concept string
		if err := crews.Scan(&symbol, &concept); err != nil {
			return fmt.Errorf("scan concept: %w", err)
		}
		if p, ok := s.profiles[symbol]; ok {
			p.Concepts = append(p.Concepts, concept)
			s.profiles[symbol] = p
		}
	}
	return crews.Err()
}

// Profile returns the cached metadata for symbol.
func (s *Store) Profile(symbol string) (Profile, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.profiles[symbol]
	return p, ok
}

// AllProfiles returns a snapshot of every cached profile (sorted by symbol).
func (s *Store) AllProfiles() []Profile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Profile, 0, len(s.profiles))
	for _, p := range s.profiles {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

// ProfileFilter narrows the profile cache for the stock browser page.
type ProfileFilter struct {
	Q          string // substring of symbol or name
	Board      string
	Industry   string
	Concept    string
	SyncedOnly bool // only stocks that also have local bars
}

// ProfileListItem couples a profile with its local bar-series status. The
// heavy business text is left out; the drawer fetches it via /profile.
type ProfileListItem struct {
	Symbol   string   `json:"symbol"`
	Name     string   `json:"name"`
	Industry string   `json:"industry"`
	Market   string   `json:"market"`
	Board    string   `json:"board"`
	ListDate string   `json:"listDate"`
	Concepts []string `json:"concepts"`
	BarCount int      `json:"barCount"`
}

// QueryProfiles filters the in-memory profile cache (a few thousand rows, so
// a linear scan under RLock is plenty) and returns every match in symbol
// order; sorting and paging are left to the caller.
func (s *Store) QueryProfiles(f ProfileFilter) []ProfileListItem {
	q := strings.ToLower(f.Q)

	s.mu.RLock()
	matched := make([]ProfileListItem, 0, len(s.profiles))
	for _, p := range s.profiles {
		if q != "" && !strings.Contains(strings.ToLower(p.Symbol), q) && !strings.Contains(p.Name, f.Q) {
			continue
		}
		if f.Board != "" && p.Board != f.Board {
			continue
		}
		if f.Industry != "" && p.Industry != f.Industry {
			continue
		}
		if f.Concept != "" && !slices.Contains(p.Concepts, f.Concept) {
			continue
		}
		barCount := len(s.bars[p.Symbol])
		if f.SyncedOnly && barCount == 0 {
			continue
		}
		matched = append(matched, ProfileListItem{
			Symbol: p.Symbol, Name: p.Name, Industry: p.Industry, Market: p.Market,
			Board: p.Board, ListDate: p.ListDate, Concepts: p.Concepts, BarCount: barCount,
		})
	}
	s.mu.RUnlock()

	sort.Slice(matched, func(i, j int) bool { return matched[i].Symbol < matched[j].Symbol })
	return matched
}

// ConceptCount aggregates how many stocks each concept holds.
type ConceptCount struct {
	Concept string `json:"concept"`
	Count   int    `json:"count"`
}

// ConceptCounts aggregates concept membership over the cache, most popular first.
func (s *Store) ConceptCounts() []ConceptCount {
	s.mu.RLock()
	counts := map[string]int{}
	for _, p := range s.profiles {
		for _, c := range p.Concepts {
			counts[c]++
		}
	}
	s.mu.RUnlock()
	out := make([]ConceptCount, 0, len(counts))
	for c, n := range counts {
		out = append(out, ConceptCount{Concept: c, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Concept < out[j].Concept
	})
	return out
}

// Industries returns the distinct non-empty industry names.
func (s *Store) Industries() []string {
	s.mu.RLock()
	set := map[string]bool{}
	for _, p := range s.profiles {
		if p.Industry != "" {
			set[p.Industry] = true
		}
	}
	s.mu.RUnlock()
	out := make([]string, 0, len(set))
	for i := range set {
		out = append(out, i)
	}
	sort.Strings(out)
	return out
}

// Bars returns the cached series for symbol (nil if unknown).
func (s *Store) Bars(symbol string) []market.Candle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.bars[symbol]
}

// Symbols returns a sorted summary of every cached series (including seeds).
func (s *Store) Symbols() []SymbolInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]SymbolInfo, 0, len(s.bars))
	for symbol, bars := range s.bars {
		if len(bars) == 0 {
			continue
		}
		out = append(out, SymbolInfo{
			Symbol:    symbol,
			Count:     len(bars),
			FirstDate: bars[0].Date,
			LastDate:  bars[len(bars)-1].Date,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out
}

// Seed registers a memory-only series (used for the built-in DEMO symbol).
func (s *Store) Seed(symbol string, bars []market.Candle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bars[symbol]; !ok {
		s.bars[symbol] = bars
	}
}

// Replace atomically swaps the whole stored series for symbol. A full
// replacement (rather than an append) matches how QFQ data is rebased.
func (s *Store) Replace(ctx context.Context, symbol string, bars []market.Candle) error {
	if len(bars) == 0 {
		return fmt.Errorf("cannot store empty bar series")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM bars WHERE symbol = $1`, symbol); err != nil {
		return fmt.Errorf("delete old bars: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO bars (symbol, date, open, high, low, close, volume) VALUES ($1, $2, $3, $4, $5, $6, $7)`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	for _, c := range bars {
		if _, err := stmt.ExecContext(ctx, symbol, c.Date, c.Open, c.High, c.Low, c.Close, c.Volume); err != nil {
			return fmt.Errorf("insert bar %s: %w", c.Date, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	if !s.lean {
		s.mu.Lock()
		s.bars[symbol] = bars
		s.mu.Unlock()
	}
	return nil
}

// BarSymbols returns every symbol that already has persisted bars, for
// resumable bulk syncs.
func (s *Store) BarSymbols(ctx context.Context) (map[string]bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT symbol FROM bars`)
	if err != nil {
		return nil, fmt.Errorf("query bar symbols: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var sym string
		if err := rows.Scan(&sym); err != nil {
			return nil, err
		}
		out[sym] = true
	}
	return out, rows.Err()
}

// SaveProfile upserts one stock's metadata and atomically swaps its concept
// list in the same transaction, then mirrors the result into the cache.
func (s *Store) SaveProfile(ctx context.Context, p Profile) error {
	if p.Symbol == "" {
		return fmt.Errorf("profile symbol is empty")
	}
	if p.Concepts == nil {
		p.Concepts = []string{}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO stock_profile (symbol, name, industry, market, board, list_date, business, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())
		ON CONFLICT (symbol) DO UPDATE SET
			name = EXCLUDED.name, industry = EXCLUDED.industry, market = EXCLUDED.market,
			board = EXCLUDED.board, list_date = EXCLUDED.list_date, business = EXCLUDED.business, updated_at = now()`,
		p.Symbol, p.Name, p.Industry, p.Market, p.Board, p.ListDate, p.Business); err != nil {
		return fmt.Errorf("upsert profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM stock_concept WHERE symbol = $1`, p.Symbol); err != nil {
		return fmt.Errorf("delete old concepts: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO stock_concept (symbol, concept) VALUES ($1, $2)`)
	if err != nil {
		return fmt.Errorf("prepare concept insert: %w", err)
	}
	defer stmt.Close()
	for _, c := range p.Concepts {
		if _, err := stmt.ExecContext(ctx, p.Symbol, c); err != nil {
			return fmt.Errorf("insert concept %s: %w", c, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	s.mu.Lock()
	s.profiles[p.Symbol] = p
	s.mu.Unlock()
	return nil
}
