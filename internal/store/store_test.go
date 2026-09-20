package store

import (
	"context"
	"path/filepath"
	"testing"

	"nstock/internal/market"
)

func TestReplacePersistsAcrossReopen(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	ctx := context.Background()

	s1, err := Open(ctx, "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	bars := market.DemoBars()
	if err := s1.Replace(ctx, "600519.SH", bars); err != nil {
		t.Fatal(err)
	}
	s1.Seed("DEMO", market.DemoBars())
	if got := s1.Bars("600519.SH"); len(got) != len(bars) {
		t.Fatalf("cached bars = %d, want %d", len(got), len(bars))
	}
	if err := s1.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(ctx, "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got := s2.Bars("600519.SH")
	if len(got) != len(bars) {
		t.Fatalf("persisted bars = %d, want %d", len(got), len(bars))
	}
	if got[0] != bars[0] || got[len(got)-1] != bars[len(bars)-1] {
		t.Fatal("persisted series content mismatch")
	}
	// The seeded DEMO series is memory-only and must not leak into the DB.
	if s2.Bars("DEMO") != nil {
		t.Fatal("DEMO leaked into sqlite; seed must be memory-only")
	}
}

func TestReplaceSwapsSeries(t *testing.T) {
	s, err := Open(context.Background(), "file:"+filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	full := market.DemoBars()
	if err := s.Replace(ctx, "000001.SZ", full); err != nil {
		t.Fatal(err)
	}
	short := full[:5]
	if err := s.Replace(ctx, "000001.SZ", short); err != nil {
		t.Fatal(err)
	}
	if got := s.Bars("000001.SZ"); len(got) != len(short) {
		t.Fatalf("after replace bars = %d, want %d", len(got), len(short))
	}
}
