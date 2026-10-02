// Package store는 PostgreSQL에 도메인, 검증 계정, TXT 값, 감사 로그를 저장한다.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/netutil"
)

var (
	ErrNotFound            = errors.New("store: not found")
	ErrActiveAccountExists = errors.New("store: domain already has an active account")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Account struct {
	ID        int64
	DomainID  int64
	Domain    string
	Username  string
	KeyHash   string
	Subdomain string
	AllowFrom []netip.Prefix
	CreatedAt time.Time
}

type NewAccount struct {
	Domain    string
	Username  uuid.UUID
	KeyHash   string
	Subdomain uuid.UUID
	AllowFrom []netip.Prefix
}

// CreateAccount는 도메인 행을 (없으면) 만들고 그 도메인의 활성 계정을 추가한다.
func (s *Store) CreateAccount(ctx context.Context, in NewAccount) (Account, error) {
	a := Account{
		Domain:    in.Domain,
		Username:  in.Username.String(),
		KeyHash:   in.KeyHash,
		Subdomain: in.Subdomain.String(),
		AllowFrom: in.AllowFrom,
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ($1)
			ON CONFLICT (fqdn) DO UPDATE SET fqdn = EXCLUDED.fqdn RETURNING id`, in.Domain).Scan(&a.DomainID); err != nil {
			return fmt.Errorf("store: upsert domain: %w", err)
		}
		err := tx.QueryRow(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain, allowfrom)
			VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
			a.DomainID, a.Username, a.KeyHash, a.Subdomain, prefixStrings(in.AllowFrom)).Scan(&a.ID, &a.CreatedAt)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "acme_accounts_one_active" {
			return ErrActiveAccountExists
		}
		if err != nil {
			return fmt.Errorf("store: insert account: %w", err)
		}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// ActiveAccountByUsername은 폐기되지 않은 계정을 찾는다. username이 UUID 형식이 아니어도 ErrNotFound다.
func (s *Store) ActiveAccountByUsername(ctx context.Context, username string) (Account, error) {
	id, err := uuid.Parse(username)
	if err != nil {
		return Account{}, ErrNotFound
	}
	var a Account
	var allow []string
	err = s.pool.QueryRow(ctx, `SELECT a.id, a.domain_id, d.fqdn, a.username::text, a.key_hash, a.subdomain::text, a.allowfrom, a.created_at
		FROM acme_accounts a JOIN domains d ON d.id = a.domain_id
		WHERE a.username = $1 AND a.revoked_at IS NULL`, id.String()).
		Scan(&a.ID, &a.DomainID, &a.Domain, &a.Username, &a.KeyHash, &a.Subdomain, &allow, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: load account: %w", err)
	}
	if a.AllowFrom, err = parsePrefixes(allow); err != nil {
		return Account{}, fmt.Errorf("store: account %d allowfrom: %w", a.ID, err)
	}
	return a, nil
}

// UpdateTXT는 계정 행을 잠근 채 현재 값(오래된 순)을 fn에 넘기고, fn이 돌려준 값 집합을 저장한다.
// 같은 계정의 갱신은 이 잠금으로 직렬화된다. fn이 오류를 돌려주면 아무것도 바꾸지 않는다.
func (s *Store) UpdateTXT(ctx context.Context, accountID int64, fn func(current []string) ([]string, error)) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM acme_accounts WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, accountID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: lock account: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT value FROM txt_records WHERE account_id = $1 ORDER BY created_at, id`, accountID)
		if err != nil {
			return fmt.Errorf("store: load txt: %w", err)
		}
		current, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("store: load txt: %w", err)
		}
		next, err := fn(current)
		if err != nil {
			return err
		}
		if next == nil {
			next = []string{}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM txt_records WHERE account_id = $1 AND NOT (value = ANY($2))`, accountID, next); err != nil {
			return fmt.Errorf("store: prune txt: %w", err)
		}
		for _, v := range next {
			if slices.Contains(current, v) {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO txt_records (account_id, value) VALUES ($1, $2)`, accountID, v); err != nil {
				return fmt.Errorf("store: insert txt: %w", err)
			}
		}
		return nil
	})
}

type ActiveTXT struct {
	AccountID int64
	Values    []string
}

// ActiveTXTBySubdomain은 활성 계정의 서브도메인별 저장 값이다. 값이 없는 계정은 빈 슬라이스로 들어간다.
func (s *Store) ActiveTXTBySubdomain(ctx context.Context) (map[string]ActiveTXT, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id, a.subdomain::text, t.value
		FROM acme_accounts a LEFT JOIN txt_records t ON t.account_id = a.id
		WHERE a.revoked_at IS NULL
		ORDER BY a.id, t.created_at, t.id`)
	if err != nil {
		return nil, fmt.Errorf("store: active txt: %w", err)
	}
	defer rows.Close()
	out := map[string]ActiveTXT{}
	for rows.Next() {
		var id int64
		var sub string
		var val *string
		if err := rows.Scan(&id, &sub, &val); err != nil {
			return nil, fmt.Errorf("store: active txt: %w", err)
		}
		e := out[sub]
		e.AccountID = id
		if e.Values == nil {
			e.Values = []string{}
		}
		if val != nil {
			e.Values = append(e.Values, *val)
		}
		out[sub] = e
	}
	return out, rows.Err()
}

type AuditEntry struct {
	Actor    string
	Action   string
	Target   string
	SourceIP string
	Detail   string
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_logs (actor, action, target, source_ip, detail) VALUES ($1, $2, $3, $4, $5)`,
		e.Actor, e.Action, e.Target, e.SourceIP, e.Detail)
	if err != nil {
		return fmt.Errorf("store: audit: %w", err)
	}
	return nil
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}

func parsePrefixes(ss []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netutil.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
