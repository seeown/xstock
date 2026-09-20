// Package store keeps daily bars in SQLite and mirrors them into an in-memory
// cache for lock-free reads by the strategy engine.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"

	"nstock/internal/market"
)

type Store struct {
	db *sql.DB

	mu   sync.RWMutex
	bars map[string][]market.Candle
}

// Open opens (creating if needed) the SQLite database at dsn and loads all
// persisted bars into memory. Example dsn: "file:nstock.db".
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single connection avoids SQLITE_BUSY between concurrent handlers.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS bars (
		symbol TEXT NOT NULL,
		date   TEXT NOT NULL,
		open   REAL NOT NULL,
		high   REAL NOT NULL,
		low    REAL NOT NULL,
		close  REAL NOT NULL,
		volume REAL NOT NULL,
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM bars WHERE symbol = ?`, symbol); err != nil {
		return fmt.Errorf("delete old bars: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO bars (symbol, date, open, high, low, close, volume) VALUES (?, ?, ?, ?, ?, ?, ?)`)
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
