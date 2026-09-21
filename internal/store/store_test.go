package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/joho/godotenv"

	"nstock/internal/market"
)

// testServer describes the PostgreSQL instance tests run against, taken from
// the same .env.produce file (or real environment) the server uses.
type testServer struct {
	user, passwd, host, port string
}

// envTestServer loads connection settings from the environment, falling back
// to the project-root .env.produce so `go test` works from any package dir.
func envTestServer() (testServer, error) {
	_ = godotenv.Load(".env.produce")
	_ = godotenv.Load("../../.env.produce")
	s := testServer{
		user:   os.Getenv("PG_USER"),
		passwd: os.Getenv("PG_PASSWD"),
		host:   os.Getenv("PG_HOST"),
		port:   os.Getenv("PG_PORT"),
	}
	for k, v := range map[string]string{"PG_USER": s.user, "PG_PASSWD": s.passwd, "PG_HOST": s.host, "PG_PORT": s.port} {
		if v == "" {
			return s, fmt.Errorf("%s is not set (no .env.produce found)", k)
		}
	}
	return s, nil
}

func (s testServer) dsn(dbName string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		url.PathEscape(s.user), url.PathEscape(s.passwd), s.host, s.port, dbName)
}

// openTestStore creates a uniquely named throwaway database on the local
// PostgreSQL server and opens a store on it; the database is dropped when the
// test finishes.
func openTestStore(t *testing.T) *Store {
	t.Helper()
	srv, err := envTestServer()
	if err != nil {
		t.Skipf("postgres config: %v", err)
	}
	admin, err := sql.Open("pgx", srv.dsn("postgres"))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("postgres unreachable at %s:%s: %v", srv.host, srv.port, err)
	}

	dbName := fmt.Sprintf("nstock_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		drop, err := sql.Open("pgx", srv.dsn("postgres"))
		if err != nil {
			return
		}
		defer drop.Close()
		dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer dcancel()
		_, _ = drop.ExecContext(dctx, `DROP DATABASE `+dbName)
	})

	s, err := Open(ctx, srv.dsn(dbName))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestReplacePersistsAcrossReopen(t *testing.T) {
	srv, err := envTestServer()
	if err != nil {
		t.Skipf("postgres config: %v", err)
	}
	dbName := fmt.Sprintf("nstock_test_%d", time.Now().UnixNano())
	ctx := context.Background()

	admin, err := sql.Open("pgx", srv.dsn("postgres"))
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := admin.PingContext(ctx); err != nil {
		t.Skipf("postgres unreachable at %s:%s: %v", srv.host, srv.port, err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	defer func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE `+dbName)
	}()

	s1, err := Open(ctx, srv.dsn(dbName))
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

	s2, err := Open(ctx, srv.dsn(dbName))
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
		t.Fatal("DEMO leaked into postgres; seed must be memory-only")
	}
}

func TestReplaceSwapsSeries(t *testing.T) {
	s := openTestStore(t)
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
