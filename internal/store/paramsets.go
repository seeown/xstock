package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ParamSet is a named, user-tunable strategy parameter group. Params is the
// raw JSON body; the server never interprets it, so new strategy fields keep
// working without a migration. Exactly one set per strategy may be the
// default that /api/params/default falls back to (built-in Go defaults are
// used when no default row exists).
type ParamSet struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Strategy  string          `json:"strategy"`
	Params    json.RawMessage `json:"params"`
	IsDefault bool            `json:"isDefault"`
	UpdatedAt string          `json:"updatedAt,omitempty"`
}

const paramSetSchema = `
CREATE TABLE IF NOT EXISTS param_sets (
	id         BIGSERIAL PRIMARY KEY,
	name       TEXT NOT NULL,
	strategy   TEXT NOT NULL,
	params     JSONB NOT NULL,
	is_default BOOLEAN NOT NULL DEFAULT FALSE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
	updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS param_sets_one_default ON param_sets (strategy) WHERE is_default;
`

func (s *Store) ensureParamSetSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, paramSetSchema); err != nil {
		return fmt.Errorf("create param_sets table: %w", err)
	}
	return nil
}

func scanParamSet(row interface{ Scan(...any) error }) (ParamSet, error) {
	var ps ParamSet
	err := row.Scan(&ps.ID, &ps.Name, &ps.Strategy, &ps.Params, &ps.IsDefault, &ps.UpdatedAt)
	return ps, err
}

const paramSetCols = `id, name, strategy, params, is_default, updated_at`

// ListParamSets returns all sets ordered by strategy then name.
func (s *Store) ListParamSets(ctx context.Context) ([]ParamSet, error) {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+paramSetCols+` FROM param_sets ORDER BY strategy, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ParamSet{}
	for rows.Next() {
		ps, err := scanParamSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ps)
	}
	return out, rows.Err()
}

// DefaultParamSet returns the set marked default for the strategy, or nil
// when the built-in Go defaults apply.
func (s *Store) DefaultParamSet(ctx context.Context, strategy string) (*ParamSet, error) {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return nil, err
	}
	ps, err := scanParamSet(s.db.QueryRowContext(ctx,
		`SELECT `+paramSetCols+` FROM param_sets WHERE strategy = $1 AND is_default LIMIT 1`, strategy))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ps, nil
}

// CreateParamSet inserts a new set and returns it with the assigned id.
func (s *Store) CreateParamSet(ctx context.Context, name, strategy string, params json.RawMessage) (ParamSet, error) {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return ParamSet{}, err
	}
	ps, err := scanParamSet(s.db.QueryRowContext(ctx,
		`INSERT INTO param_sets (name, strategy, params)
		 VALUES ($1, $2, $3) RETURNING `+paramSetCols, name, strategy, string(params)))
	if err != nil {
		return ParamSet{}, err
	}
	return ps, nil
}

// UpdateParamSet renames and/or replaces the params JSON of a set.
func (s *Store) UpdateParamSet(ctx context.Context, id int64, name string, params json.RawMessage) error {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE param_sets SET name = $2, params = $3, updated_at = now() WHERE id = $1`, id, name, string(params))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("param set %d not found", id)
	}
	return nil
}

// DeleteParamSet removes a set; deleting the default row silently falls the
// strategy back to built-in defaults.
func (s *Store) DeleteParamSet(ctx context.Context, id int64) error {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM param_sets WHERE id = $1`, id)
	return err
}

// SetDefaultParamSet marks the set as the default for its strategy and
// clears the flag on the strategy's previous default (one per strategy).
func (s *Store) SetDefaultParamSet(ctx context.Context, id int64) error {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var strategy string
	if err := tx.QueryRowContext(ctx, `SELECT strategy FROM param_sets WHERE id = $1`, id).Scan(&strategy); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE param_sets SET is_default = FALSE WHERE strategy = $1 AND is_default`, strategy); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE param_sets SET is_default = TRUE, updated_at = now() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("param set %d not found", id)
	}
	return tx.Commit()
}

// ClearDefaultParamSet drops the default flag for a strategy so the
// built-in Go defaults apply again.
func (s *Store) ClearDefaultParamSet(ctx context.Context, strategy string) error {
	if err := s.ensureParamSetSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE param_sets SET is_default = FALSE WHERE strategy = $1 AND is_default`, strategy)
	return err
}
