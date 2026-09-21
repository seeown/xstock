// Package store keeps daily bars in PostgreSQL and mirrors them into an
// in-memory cache for lock-free reads by the strategy engine.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"

	"nstock/internal/market"
)

type Store struct {
	db *sql.DB

	mu   sync.RWMutex
	bars map[string][]market.Candle
}

// SymbolInfo describes one cached symbol series for the data-management page.
type SymbolInfo struct {
	Symbol    string `json:"symbol"`
	Count     int    `json:"count"`
	FirstDate string `json:"firstDate"`
	LastDate  string `json:"lastDate"`
}

// Open opens the PostgreSQL database at dsn (URL or key=value form) and loads
// all persisted bars into memory. Example dsn:
// "postgres://wyf:password@127.0.0.1:5432/nstock".
func Open(ctx context.Context, dsn string) (*Store, error) {
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
	s := &Store{db: db, bars: map[string][]market.Candle{}}
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
	return rows.Err()
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

	s.mu.Lock()
	s.bars[symbol] = bars
	s.mu.Unlock()
	return nil
}
