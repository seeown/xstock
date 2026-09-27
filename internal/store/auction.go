package store

import (
	"context"
	"fmt"
	"strings"
)

// AuctionBar 是一股某交易日的竞价定格（9:25 撮合后的开盘价/昨收/竞价成交额）。
// 量级同日线（每日全市场约 5500 行），窄表直存 PG。
type AuctionBar struct {
	Symbol     string
	Open       float64
	PreClose   float64
	AuctionAmt float64
}

const auctionSchema = `
CREATE TABLE IF NOT EXISTS auction_bars (
	symbol      TEXT NOT NULL,
	date        TEXT NOT NULL,
	open        DOUBLE PRECISION NOT NULL DEFAULT 0,
	pre_close   DOUBLE PRECISION NOT NULL DEFAULT 0,
	auction_amt DOUBLE PRECISION NOT NULL DEFAULT 0,
	PRIMARY KEY (symbol, date)
);
CREATE TABLE IF NOT EXISTS auction_reports (
	date       TEXT PRIMARY KEY,
	payload    JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS auction_bars_symbol_idx ON auction_bars (symbol, date DESC);
`

func (s *Store) ensureAuctionSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, auctionSchema); err != nil {
		return fmt.Errorf("create auction tables: %w", err)
	}
	return nil
}

// ArchiveAuctionBars upserts one day's bars in parameterised chunks (重跑安全)。
func (s *Store) ArchiveAuctionBars(ctx context.Context, date string, bars []AuctionBar) error {
	if err := s.ensureAuctionSchema(ctx); err != nil {
		return err
	}
	const chunk = 400
	for start := 0; start < len(bars); start += chunk {
		end := start + chunk
		if end > len(bars) {
			end = len(bars)
		}
		part := bars[start:end]
		var sb strings.Builder
		sb.WriteString("INSERT INTO auction_bars (symbol, date, open, pre_close, auction_amt) VALUES ")
		args := make([]any, 0, len(part)*5)
		for i, b := range part {
			if i > 0 {
				sb.WriteByte(',')
			}
			base := i * 5
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5)
			args = append(args, b.Symbol, date, b.Open, b.PreClose, b.AuctionAmt)
		}
		sb.WriteString(" ON CONFLICT (symbol, date) DO UPDATE SET open = EXCLUDED.open, pre_close = EXCLUDED.pre_close, auction_amt = EXCLUDED.auction_amt")
		if _, err := s.db.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("archive auction bars %s: %w", date, err)
		}
	}
	return nil
}

// SaveAuctionReport upserts the day's morning-report payload (JSONB).
func (s *Store) SaveAuctionReport(ctx context.Context, date string, payload []byte) error {
	if err := s.ensureAuctionSchema(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO auction_reports (date, payload) VALUES ($1, $2)
		 ON CONFLICT (date) DO UPDATE SET payload = EXCLUDED.payload, created_at = now()`,
		date, payload); err != nil {
		return fmt.Errorf("save auction report %s: %w", date, err)
	}
	return nil
}

// AuctionReport returns the archived payload for a date, if any.
func (s *Store) AuctionReport(ctx context.Context, date string) ([]byte, bool, error) {
	if err := s.ensureAuctionSchema(ctx); err != nil {
		return nil, false, err
	}
	var payload []byte
	err := s.db.QueryRowContext(ctx, `SELECT payload FROM auction_reports WHERE date = $1`, date).Scan(&payload)
	if err != nil {
		if err.Error() == "sql: no rows" {
			return nil, false, nil
		}
		return nil, false, err
	}
	return payload, true, nil
}
