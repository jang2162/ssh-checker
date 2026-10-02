package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"ssl-checker/internal/db"
	"ssl-checker/internal/db/dbtest"
)

func TestMigrateCreatesTablesAndIsIdempotent(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	for _, table := range []string{"domains", "acme_accounts", "txt_records", "audit_logs"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s missing", table)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
}

func TestOneActiveAccountPerDomain(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	var domainID int64
	if err := pool.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ('a.com') RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	insert := func(user, sub string) error {
		_, err := pool.Exec(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain) VALUES ($1, $2, 'h', $3)`,
			domainID, user, sub)
		return err
	}
	if err := insert("11111111-1111-1111-1111-111111111111", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	err := insert("22222222-2222-2222-2222-222222222222", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "acme_accounts_one_active" {
		t.Fatalf("second active account: err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	if err := insert("22222222-2222-2222-2222-222222222222", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
}
