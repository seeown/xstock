package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ScreenSnapshot 每日筛查形态的定格快照：date 为归档日（=扫描数据基准
// asOf），key_date 为形态关键日；setup 存 NPSetup 全量 JSONB，参数组
// 快照存 meta 表——将来调参后历史归档仍按当时口径可查。
type ScreenSnapshot struct {
	Date     string
	Stage    string
	Symbol   string
	KeyDate  string
	Name     string
	Industry string
	Setup    json.RawMessage
}

const screenArchiveSchema = `
CREATE TABLE IF NOT EXISTS screen_snapshots (
	id       BIGSERIAL PRIMARY KEY,
	date     TEXT NOT NULL,
	stage    TEXT NOT NULL,
	symbol   TEXT NOT NULL,
	key_date TEXT NOT NULL,
	name     TEXT NOT NULL DEFAULT '',
	industry TEXT NOT NULL DEFAULT '',
	setup    JSONB NOT NULL,
	UNIQUE (date, symbol, stage, key_date)
);
CREATE INDEX IF NOT EXISTS screen_snapshots_date_idx ON screen_snapshots (date, key_date DESC);
CREATE TABLE IF NOT EXISTS screen_archive_meta (
	date       TEXT PRIMARY KEY,
	params     JSONB NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

func (s *Store) ensureScreenArchiveSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, screenArchiveSchema); err != nil {
		return fmt.Errorf("create screen archive tables: %w", err)
	}
	return nil
}

// ReplaceScreenSnapshots 以归档日为单位全量替换（重复归档以最后一次为准），
// 同一事务内写 meta（参数组快照）。
func (s *Store) ReplaceScreenSnapshots(ctx context.Context, date string, params json.RawMessage, items []ScreenSnapshot) error {
	if err := s.ensureScreenArchiveSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM screen_snapshots WHERE date = $1`, date); err != nil {
		return fmt.Errorf("clear screen snapshots %s: %w", date, err)
	}
	const chunk = 200
	for start := 0; start < len(items); start += chunk {
		end := min(start+chunk, len(items))
		part := items[start:end]
		var sb strings.Builder
		// ON CONFLICT DO NOTHING：形态扫描理论上可能对同一 (symbol, stage,
		// key_date) 产出多条（不同 A 段入口的路径在突破日汇合），并发归档
		// 也会撞键——全量替换语义下重复键留一条即可，不值得 500。
		sb.WriteString("INSERT INTO screen_snapshots (date, stage, symbol, key_date, name, industry, setup) VALUES ")
		args := make([]any, 0, len(part)*7)
		for i, it := range part {
			if i > 0 {
				sb.WriteByte(',')
			}
			base := i * 7
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d,$%d,$%d)", base+1, base+2, base+3, base+4, base+5, base+6, base+7)
			args = append(args, it.Date, it.Stage, it.Symbol, it.KeyDate, it.Name, it.Industry, it.Setup)
		}
		sb.WriteString(" ON CONFLICT (date, symbol, stage, key_date) DO NOTHING")
		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("insert screen snapshots %s: %w", date, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO screen_archive_meta (date, params) VALUES ($1, $2)
		 ON CONFLICT (date) DO UPDATE SET params = EXCLUDED.params, created_at = now()`,
		date, params); err != nil {
		return fmt.Errorf("upsert screen archive meta %s: %w", date, err)
	}
	return tx.Commit()
}

// ListScreenArchiveDates 归档日期列表，近 → 远。
func (s *Store) ListScreenArchiveDates(ctx context.Context) ([]string, error) {
	if err := s.ensureScreenArchiveSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT date FROM screen_archive_meta ORDER BY date DESC`)
	if err != nil {
		return nil, fmt.Errorf("list screen archive dates: %w", err)
	}
	defer rows.Close()
	out := make([]string, 0, 8)
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ScreenSnapshotsOfDay 取某归档日的全部形态（关键日新 → 旧）。
func (s *Store) ScreenSnapshotsOfDay(ctx context.Context, date string) ([]ScreenSnapshot, error) {
	if err := s.ensureScreenArchiveSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT stage, symbol, key_date, name, industry, setup
		FROM screen_snapshots WHERE date = $1
		ORDER BY key_date DESC, symbol`, date)
	if err != nil {
		return nil, fmt.Errorf("query screen snapshots %s: %w", date, err)
	}
	defer rows.Close()
	out := make([]ScreenSnapshot, 0, 16)
	for rows.Next() {
		var it ScreenSnapshot
		if err := rows.Scan(&it.Stage, &it.Symbol, &it.KeyDate, &it.Name, &it.Industry, &it.Setup); err != nil {
			return nil, err
		}
		it.Date = date
		out = append(out, it)
	}
	return out, rows.Err()
}
