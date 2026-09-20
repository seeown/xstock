package market

import "testing"

func TestFindNSignalsDemo(t *testing.T) {
	signals := FindNSignals("DEMO", DemoBars(), DefaultParams())
	if len(signals) == 0 {
		t.Fatal("expected at least one signal on the demo series")
	}
	for _, s := range signals {
		if s.Symbol != "DEMO" || s.Date == "" || s.BreakoutPrice <= 0 {
			t.Fatalf("malformed signal: %+v", s)
		}
	}
}

func TestBacktestEntersNextOpen(t *testing.T) {
	bars := DemoBars()
	signals := FindNSignals("DEMO", bars, DefaultParams())
	if len(signals) == 0 {
		t.Fatal("expected a signal to anchor the trade")
	}
	result := Backtest("DEMO", bars, DefaultParams(), 100000)
	if len(result.Trades) == 0 {
		t.Fatal("expected at least one trade")
	}
	// Every buy must be the trading day after its signal, not the signal day.
	byDate := map[string]int{}
	for i, b := range bars {
		byDate[b.Date] = i
	}
	for _, tr := range result.Trades {
		buyIdx := byDate[tr.BuyDate]
		sigIdx := byDate[tr.BuyDate] - 1
		if sigIdx < 0 || bars[sigIdx].Date == "" {
			t.Fatalf("trade %v has no prior bar", tr.BuyDate)
		}
		if tr.BuyPrice != bars[buyIdx].Open {
			t.Fatalf("buy price %.4f != next open %.4f", tr.BuyPrice, bars[buyIdx].Open)
		}
	}
	if result.Metrics.TradeCount != len(result.Trades) {
		t.Fatalf("metrics trade count %d != %d", result.Metrics.TradeCount, len(result.Trades))
	}
}
