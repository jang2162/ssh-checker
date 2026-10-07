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
	if n != 2 {
		t.Fatalf("schema_migrations rows = %d, want 2", n)
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

func TestMonitoringSchema(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()

	var hasStatus bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'domains' AND column_name = 'status')`).Scan(&hasStatus); err != nil {
		t.Fatal(err)
	}
	if hasStatus {
		t.Error("domains.status should be dropped")
	}

	// 도메인을 지우면 계정, TXT, 대상, 점검 기록이 함께 지워진다.
	var domainID, accountID, targetID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ('a.com') RETURNING id`).Scan(&domainID))
	must(pool.QueryRow(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain)
		VALUES ($1, gen_random_uuid(), 'h', gen_random_uuid()) RETURNING id`, domainID).Scan(&accountID))
	_, err := pool.Exec(ctx, `INSERT INTO txt_records (account_id, value) VALUES ($1, 'v')`, accountID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO monitor_targets (domain_id, hostname) VALUES ($1, 'www.a.com') RETURNING id`, domainID).Scan(&targetID))
	_, err = pool.Exec(ctx, `INSERT INTO check_results (target_id, address, status) VALUES ($1, '10.0.0.1:443', 'ok')`, targetID)
	must(err)

	var cname string
	var failures int
	must(pool.QueryRow(ctx, `SELECT cname_status, consecutive_failures FROM acme_accounts WHERE id = $1`, accountID).Scan(&cname, &failures))
	if cname != "unknown" || failures != 0 {
		t.Fatalf("acme defaults = %q, %d", cname, failures)
	}

	_, err = pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, domainID)
	must(err)
	for _, table := range []string{"acme_accounts", "txt_records", "monitor_targets", "check_results"} {
		var n int
		must(pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Errorf("%s has %d rows after domain delete", table, n)
		}
	}
}
