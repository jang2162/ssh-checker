package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"ssl-checker/internal/monitor"
)

var (
	ErrDomainExists = errors.New("store: domain already exists")
	ErrTargetExists = errors.New("store: target already exists")
)

const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

type Domain struct {
	ID        int64
	FQDN      string
	Memo      string
	CreatedAt time.Time
}

func (s *Store) CreateDomain(ctx context.Context, fqdn, memo string) (Domain, error) {
	d := Domain{FQDN: fqdn, Memo: memo}
	err := s.pool.QueryRow(ctx, `INSERT INTO domains (fqdn, memo) VALUES ($1, $2) RETURNING id, created_at`, fqdn, memo).
		Scan(&d.ID, &d.CreatedAt)
	switch {
	case pgCode(err) == codeUniqueViolation:
		return Domain{}, ErrDomainExists
	case err != nil:
		return Domain{}, fmt.Errorf("store: create domain: %w", err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

func scanDomain(row pgx.CollectableRow) (Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.FQDN, &d.Memo, &d.CreatedAt)
	d.CreatedAt = d.CreatedAt.UTC()
	return d, err
}

func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, fqdn, memo, created_at FROM domains ORDER BY fqdn`)
	if err != nil {
		return nil, fmt.Errorf("store: list domains: %w", err)
	}
	ds, err := pgx.CollectRows(rows, scanDomain)
	if err != nil {
		return nil, fmt.Errorf("store: list domains: %w", err)
	}
	return ds, nil
}

func (s *Store) GetDomain(ctx context.Context, id int64) (Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, fqdn, memo, created_at FROM domains WHERE id = $1`, id)
	if err != nil {
		return Domain{}, fmt.Errorf("store: get domain: %w", err)
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDomain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, fmt.Errorf("store: get domain: %w", err)
	}
	return d, nil
}

// Target은 저장된 모니터링 대상이다. 점검에 필요한 값은 monitor.Target에 있다.
type Target struct {
	monitor.Target
	DomainID  int64
	Memo      string
	CreatedAt time.Time
}

type NewTarget struct {
	DomainID int64
	Hostname string
	Port     int
	SNI      string
	IPs      []netip.Addr
	Memo     string
}

// CreateTarget은 대상을 추가한다. 도메인이 없으면 ErrNotFound, 같은 (호스트, 포트, SNI)가 있으면 ErrTargetExists다.
func (s *Store) CreateTarget(ctx context.Context, in NewTarget) (Target, error) {
	t := Target{
		Target:   monitor.Target{Hostname: in.Hostname, Port: in.Port, SNI: in.SNI, IPs: in.IPs},
		DomainID: in.DomainID,
		Memo:     in.Memo,
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO monitor_targets (domain_id, hostname, port, sni, ips, memo)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		in.DomainID, in.Hostname, in.Port, in.SNI, addrStrings(in.IPs), in.Memo).Scan(&t.ID, &t.CreatedAt)
	switch pgCode(err) {
	case codeUniqueViolation:
		return Target{}, ErrTargetExists
	case codeForeignKeyViolation:
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("store: create target: %w", err)
	}
	t.CreatedAt = t.CreatedAt.UTC()
	return t, nil
}

const targetCols = `id, domain_id, hostname, port, sni, ips, memo, created_at`

func scanTarget(row pgx.CollectableRow) (Target, error) {
	var t Target
	var ips []string
	if err := row.Scan(&t.ID, &t.DomainID, &t.Hostname, &t.Port, &t.SNI, &ips, &t.Memo, &t.CreatedAt); err != nil {
		return Target{}, err
	}
	t.CreatedAt = t.CreatedAt.UTC()
	for _, s := range ips {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return Target{}, fmt.Errorf("target %d ip %q: %w", t.ID, s, err)
		}
		t.IPs = append(t.IPs, a)
	}
	return t, nil
}

func (s *Store) queryTargets(ctx context.Context, query string, args ...any) ([]Target, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: targets: %w", err)
	}
	ts, err := pgx.CollectRows(rows, scanTarget)
	if err != nil {
		return nil, fmt.Errorf("store: targets: %w", err)
	}
	return ts, nil
}

func (s *Store) ListTargets(ctx context.Context) ([]Target, error) {
	return s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets ORDER BY domain_id, hostname, port, id`)
}

func (s *Store) TargetsByDomain(ctx context.Context, domainID int64) ([]Target, error) {
	return s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets WHERE domain_id = $1 ORDER BY hostname, port, id`, domainID)
}

func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	ts, err := s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets WHERE id = $1`, id)
	if err != nil {
		return Target{}, err
	}
	if len(ts) == 0 {
		return Target{}, ErrNotFound
	}
	return ts[0], nil
}

const resultCols = `address, checked_at, status, reasons, tls_version, cipher, subject_cn, sans, issuer, serial,
	not_before, not_after, key_alg, key_bits, chain_ok, hostname_ok, error`

// SaveResults는 점검 한 번의 결과를 한 트랜잭션으로 저장한다. monitor.Sink를 구현한다.
func (s *Store) SaveResults(ctx context.Context, targetID int64, rs []monitor.Result) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range rs {
			_, err := tx.Exec(ctx, `INSERT INTO check_results (target_id, `+resultCols+`)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
				targetID, r.Address, r.CheckedAt, string(r.Status), nonNil(r.Reasons), r.TLSVersion, r.Cipher, r.SubjectCN,
				nonNil(r.SANs), r.Issuer, r.Serial, nullTime(r.NotBefore), nullTime(r.NotAfter), r.KeyAlg, r.KeyBits,
				r.ChainOK, r.HostnameOK, r.Error)
			if err != nil {
				return fmt.Errorf("store: save result %s: %w", r.Address, err)
			}
		}
		return nil
	})
}

// scanResult는 resultCols 순서의 행을 읽는다. prefix는 그 앞에 오는 열(예: target_id)의 대상이다.
func scanResult(row pgx.Row, prefix ...any) (monitor.Result, error) {
	var r monitor.Result
	var status string
	var nb, na *time.Time
	dst := append(prefix, &r.Address, &r.CheckedAt, &status, &r.Reasons, &r.TLSVersion, &r.Cipher, &r.SubjectCN, &r.SANs,
		&r.Issuer, &r.Serial, &nb, &na, &r.KeyAlg, &r.KeyBits, &r.ChainOK, &r.HostnameOK, &r.Error)
	if err := row.Scan(dst...); err != nil {
		return monitor.Result{}, err
	}
	r.Status = monitor.Status(status)
	r.CheckedAt = r.CheckedAt.UTC()
	if nb != nil {
		r.NotBefore = nb.UTC()
	}
	if na != nil {
		r.NotAfter = na.UTC()
	}
	r.Reasons, r.SANs = nonNil(r.Reasons), nonNil(r.SANs)
	return r, nil
}

// LatestResults는 대상마다 지금 설정에 맞는 접속 주소별 최신 결과다. 모든 대상의 키가 들어 있다.
// IP 목록이 있으면 DialAddresses 순서대로 주소별 최신 한 건, 없으면 호스트명으로 접속한 가장 최근 한 건만 쓴다
// (풀린 주소가 바뀌었을 수 있다). 조회마다 인덱스로 한 건만 읽어 전체 이력을 훑지 않는다.
func (s *Store) LatestResults(ctx context.Context, targets []Target) (map[int64][]monitor.Result, error) {
	var ipIDs, hostIDs []int64
	var ipAddrs []string
	for _, t := range targets {
		if len(t.IPs) == 0 {
			hostIDs = append(hostIDs, t.ID)
			continue
		}
		for _, a := range t.DialAddresses() {
			ipIDs = append(ipIDs, t.ID)
			ipAddrs = append(ipAddrs, a)
		}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT k.target_id, r.* FROM unnest($1::bigint[], $2::text[]) AS k(target_id, address)
		CROSS JOIN LATERAL (SELECT `+resultCols+` FROM check_results c
			WHERE c.target_id = k.target_id AND c.address = k.address
			ORDER BY c.checked_at DESC, c.id DESC LIMIT 1) r
		UNION ALL
		SELECT k.target_id, r.* FROM unnest($3::bigint[]) AS k(target_id)
		CROSS JOIN LATERAL (SELECT `+resultCols+` FROM check_results c
			WHERE c.target_id = k.target_id
			ORDER BY c.checked_at DESC, c.id DESC LIMIT 1) r`, ipIDs, ipAddrs, hostIDs)
	if err != nil {
		return nil, fmt.Errorf("store: latest results: %w", err)
	}
	defer rows.Close()
	type key struct {
		id   int64
		addr string
	}
	found := map[key]monitor.Result{}
	for rows.Next() {
		var id int64
		r, err := scanResult(rows, &id)
		if err != nil {
			return nil, fmt.Errorf("store: latest results: %w", err)
		}
		found[key{id, r.Address}] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: latest results: %w", err)
	}
	out := make(map[int64][]monitor.Result, len(targets))
	for _, t := range targets {
		rs := []monitor.Result{}
		if len(t.IPs) == 0 {
			// 호스트명 모드는 주소를 미리 알 수 없어 대상당 한 건뿐인 결과를 그대로 쓴다.
			for k, r := range found {
				if k.id == t.ID {
					rs = append(rs, r)
				}
			}
		} else {
			for _, a := range t.DialAddresses() {
				if r, ok := found[key{t.ID, a}]; ok {
					rs = append(rs, r)
				}
			}
		}
		out[t.ID] = rs
	}
	return out, nil
}

func (s *Store) ResultHistory(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+resultCols+` FROM check_results WHERE target_id = $1
		ORDER BY checked_at DESC, id DESC LIMIT $2`, targetID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: result history: %w", err)
	}
	defer rows.Close()
	out := []monitor.Result{}
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, fmt.Errorf("store: result history: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PruneResults(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM check_results WHERE checked_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("store: prune results: %w", err)
	}
	return tag.RowsAffected(), nil
}

func addrStrings(as []netip.Addr) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.String())
	}
	return out
}

func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
