package store

import (
	"context"
	"fmt"
)

// WatchItem is one row of the user's watchlist (自选). Single-user tool: no
// owner column. Notes are free-form and optional.
type WatchItem struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name,omitempty"`
	Note     string `json:"note,omitempty"`
	AddedAt  string `json:"addedAt,omitempty"`
}

const watchSchema = `
CREATE TABLE IF NOT EXISTS watchlist (
	symbol    TEXT PRIMARY KEY,
	note      TEXT NOT NULL DEFAULT '',
	added_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

func (s *Store) ensureWatchSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, watchSchema); err != nil {
		return fmt.Errorf("create watchlist table: %w", err)
	}
	return nil
}

// WatchList returns the whole watchlist joined with profile names, oldest
// entry first (stable ordering for the UI).
func (s *Store) WatchList(ctx context.Context) ([]WatchItem, error) {
	if err := s.ensureWatchSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.symbol, COALESCE(p.name, ''), w.note, w.added_at::text
		FROM watchlist w
		LEFT JOIN stock_profile p ON p.symbol = w.symbol
		ORDER BY w.added_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("query watchlist: %w", err)
	}
	defer rows.Close()
	out := make([]WatchItem, 0, 16)
	for rows.Next() {
		var it WatchItem
		if err := rows.Scan(&it.Symbol, &it.Name, &it.Note, &it.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// WatchAdd adds a symbol (idempotent; re-adding keeps the original time).
func (s *Store) WatchAdd(ctx context.Context, symbol string) error {
	if err := s.ensureWatchSchema(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO watchlist (symbol) VALUES ($1) ON CONFLICT (symbol) DO NOTHING`, symbol); err != nil {
		return fmt.Errorf("add watch %s: %w", symbol, err)
	}
	return nil
}

// WatchRemove drops a symbol (missing rows are fine).
func (s *Store) WatchRemove(ctx context.Context, symbol string) error {
	if err := s.ensureWatchSchema(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM watchlist WHERE symbol = $1`, symbol); err != nil {
		return fmt.Errorf("remove watch %s: %w", symbol, err)
	}
	return nil
}

// WatchSet returns the in-memory set of watched symbols; the profile cache is
// the join source for names so callers can render rows without another round
// trip.
func (s *Store) WatchSet(ctx context.Context) (map[string]bool, error) {
	items, err := s.WatchList(ctx)
	if err != nil {
		return nil, err
	}
	set := make(map[string]bool, len(items))
	for _, it := range items {
		set[it.Symbol] = true
	}
	return set, nil
}
