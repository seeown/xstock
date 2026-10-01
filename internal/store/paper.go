package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// 模拟仓（paper trading）账本。持仓不落表——数量/可卖(T+1)/成本全部从
// paper_trades 推导（买入日期 < 今日的净买入即"可卖"），避免持仓表与
// 流水双写不一致；单账户持仓几十只、流水几千条，内存推导零压力。

type PaperAccount struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	InitialCash float64 `json:"initialCash"`
	Cash        float64 `json:"cash"`
}

type PaperTrade struct {
	ID        int64           `json:"id"`
	Symbol    string          `json:"symbol"`
	Name      string          `json:"name"`
	Side      string          `json:"side"` // buy | sell
	Price     float64         `json:"price"`
	Qty       int             `json:"qty"`
	Amount    float64         `json:"amount"` // price*qty
	Fee       float64         `json:"fee"`    // 佣金 + 过户费
	Tax       float64         `json:"tax"`    // 印花税（卖出）
	Note      string          `json:"note,omitempty"`
	Signal    json.RawMessage `json:"signal,omitempty"` // 下单时的雷达信号上下文
	TradedAt  string          `json:"tradedAt"`
}

type PaperEquityPoint struct {
	Date        string  `json:"date"`
	Cash        float64 `json:"cash"`
	MarketValue float64 `json:"marketValue"`
	Total       float64 `json:"total"`
}

const paperSchema = `
CREATE TABLE IF NOT EXISTS paper_accounts (
	id           BIGSERIAL PRIMARY KEY,
	name         TEXT NOT NULL UNIQUE,
	initial_cash DOUBLE PRECISION NOT NULL,
	cash         DOUBLE PRECISION NOT NULL,
	created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS paper_trades (
	id        BIGSERIAL PRIMARY KEY,
	account_id BIGINT NOT NULL REFERENCES paper_accounts(id),
	symbol    TEXT NOT NULL,
	name      TEXT NOT NULL DEFAULT '',
	side      TEXT NOT NULL,
	price     DOUBLE PRECISION NOT NULL,
	qty       INTEGER NOT NULL,
	amount    DOUBLE PRECISION NOT NULL,
	fee       DOUBLE PRECISION NOT NULL,
	tax       DOUBLE PRECISION NOT NULL,
	note      TEXT NOT NULL DEFAULT '',
	signal    JSONB,
	traded_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS paper_trades_acct_idx ON paper_trades (account_id, traded_at DESC);
CREATE TABLE IF NOT EXISTS paper_equity_curve (
	account_id  BIGINT NOT NULL REFERENCES paper_accounts(id),
	date        TEXT NOT NULL,
	cash        DOUBLE PRECISION NOT NULL,
	market_value DOUBLE PRECISION NOT NULL,
	total       DOUBLE PRECISION NOT NULL,
	PRIMARY KEY (account_id, date)
);
`

func (s *Store) ensurePaperSchema(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, paperSchema); err != nil {
		return fmt.Errorf("create paper tables: %w", err)
	}
	return nil
}

// EnsurePaperAccount 取默认模拟账户，不存在则以初始资金开立。
func (s *Store) EnsurePaperAccount(ctx context.Context, name string, initialCash float64) (PaperAccount, error) {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return PaperAccount{}, err
	}
	var a PaperAccount
	err := s.db.QueryRowContext(ctx,
		`INSERT INTO paper_accounts (name, initial_cash, cash) VALUES ($1, $2, $2)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id, name, initial_cash, cash`, name, initialCash).Scan(&a.ID, &a.Name, &a.InitialCash, &a.Cash)
	if err != nil {
		return PaperAccount{}, fmt.Errorf("ensure paper account: %w", err)
	}
	return a, nil
}

// ExecPaperTrade 在一个事务里完成「记账 + 落流水」：买入扣现金，卖出入账。
func (s *Store) ExecPaperTrade(ctx context.Context, acctID int64, t PaperTrade) (PaperTrade, error) {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return PaperTrade{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PaperTrade{}, err
	}
	defer tx.Rollback()
	var delta float64
	if t.Side == "buy" {
		delta = -(t.Amount + t.Fee)
	} else {
		delta = t.Amount - t.Fee - t.Tax
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE paper_accounts SET cash = cash + $2 WHERE id = $1 AND cash + $2 >= -0.005`, acctID, delta)
	if err != nil {
		return PaperTrade{}, fmt.Errorf("update paper cash: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return PaperTrade{}, fmt.Errorf("现金不足")
	}
	var sig any
	if len(t.Signal) > 0 {
		sig = string(t.Signal)
	}
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO paper_trades (account_id, symbol, name, side, price, qty, amount, fee, tax, note, signal)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		 RETURNING id, traded_at`,
		acctID, t.Symbol, t.Name, t.Side, t.Price, t.Qty, t.Amount, t.Fee, t.Tax, t.Note, sig,
	).Scan(&t.ID, &t.TradedAt); err != nil {
		return PaperTrade{}, fmt.Errorf("insert paper trade: %w", err)
	}
	return t, tx.Commit()
}

// PaperTrades 流水倒序（新 → 旧）。
func (s *Store) PaperTrades(ctx context.Context, acctID int64, limit int) ([]PaperTrade, error) {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, symbol, name, side, price, qty, amount, fee, tax, note,
		       COALESCE(signal::text, ''), to_char(traded_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD HH24:MI:SS')
		FROM paper_trades WHERE account_id = $1
		ORDER BY id DESC LIMIT $2`, acctID, limit)
	if err != nil {
		return nil, fmt.Errorf("query paper trades: %w", err)
	}
	defer rows.Close()
	out := make([]PaperTrade, 0, 32)
	for rows.Next() {
		var t PaperTrade
		var sig string
		if err := rows.Scan(&t.ID, &t.Symbol, &t.Name, &t.Side, &t.Price, &t.Qty, &t.Amount,
			&t.Fee, &t.Tax, &t.Note, &sig, &t.TradedAt); err != nil {
			return nil, err
		}
		if sig != "" && sig != "null" {
			t.Signal = json.RawMessage(sig)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PaperPosition 推导出的持仓行（现价与盈亏由服务端填充）。
type PaperPosition struct {
	Symbol    string  `json:"symbol"`
	Name      string  `json:"name"`
	Qty       int     `json:"qty"`      // 总持仓
	AvailQty  int     `json:"availQty"` // T+1 可卖
	CostPrice float64 `json:"costPrice"`
	TodayQty  int     `json:"-"`          // 今日买入量（当日盈亏从成交价起算）
	TodayCost float64 `json:"-"`          // 今日买入成本（含费用）
}

// PaperRawPositions 从流水推导持仓与可卖（不含现价）。today 为北京时间
// YYYY-MM-DD：可卖 = 净买入中"买入日早于今日"的部分。
func (s *Store) PaperRawPositions(ctx context.Context, acctID int64, today string) ([]PaperPosition, error) {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT symbol,
		       MAX(name) FILTER (WHERE name <> ''),
		       SUM(CASE WHEN side = 'buy' THEN qty ELSE -qty END),
		       SUM(CASE WHEN side = 'buy' AND to_char(traded_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') < $2
		                THEN qty ELSE 0 END)
		        - SUM(CASE WHEN side = 'sell' THEN qty ELSE 0 END),
		       CASE WHEN SUM(CASE WHEN side = 'buy' THEN qty ELSE -qty END) > 0
		            THEN SUM(CASE WHEN side = 'buy' THEN amount + fee ELSE 0 END)
		                 / SUM(CASE WHEN side = 'buy' THEN qty ELSE 0 END)
		            ELSE 0 END,
		       SUM(CASE WHEN side = 'buy' AND to_char(traded_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') = $2
		                THEN qty ELSE 0 END),
		       SUM(CASE WHEN side = 'buy' AND to_char(traded_at AT TIME ZONE 'Asia/Shanghai', 'YYYY-MM-DD') = $2
		                THEN amount + fee ELSE 0 END)
		FROM paper_trades WHERE account_id = $1
		GROUP BY symbol`, acctID, today)
	if err != nil {
		return nil, fmt.Errorf("derive paper positions: %w", err)
	}
	defer rows.Close()
	out := make([]PaperPosition, 0, 8)
	for rows.Next() {
		var p PaperPosition
		if err := rows.Scan(&p.Symbol, &p.Name, &p.Qty, &p.AvailQty, &p.CostPrice, &p.TodayQty, &p.TodayCost); err != nil {
			return nil, err
		}
		if p.Qty > 0 {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// UpsertPaperEquity 记录某日净值快照（同日重复以最后一次为准）。
func (s *Store) UpsertPaperEquity(ctx context.Context, acctID int64, p PaperEquityPoint) error {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO paper_equity_curve (account_id, date, cash, market_value, total)
		 VALUES ($1,$2,$3,$4,$5)
		 ON CONFLICT (account_id, date) DO UPDATE SET cash = EXCLUDED.cash,
		   market_value = EXCLUDED.market_value, total = EXCLUDED.total`,
		acctID, p.Date, p.Cash, p.MarketValue, p.Total)
	if err != nil {
		return fmt.Errorf("upsert paper equity: %w", err)
	}
	return nil
}

// PaperEquityCurve 净值曲线（日期升序）。
func (s *Store) PaperEquityCurve(ctx context.Context, acctID int64) ([]PaperEquityPoint, error) {
	if err := s.ensurePaperSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT date, cash, market_value, total FROM paper_equity_curve
		 WHERE account_id = $1 ORDER BY date`, acctID)
	if err != nil {
		return nil, fmt.Errorf("query paper equity curve: %w", err)
	}
	defer rows.Close()
	out := make([]PaperEquityPoint, 0, 32)
	for rows.Next() {
		var p PaperEquityPoint
		if err := rows.Scan(&p.Date, &p.Cash, &p.MarketValue, &p.Total); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
