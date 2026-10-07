package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"ssl-checker/internal/cnamecheck"
)

// ACMEState는 활성 ACME 계정의 위임 상태다. 키와 키 해시는 담지 않는다.
type ACMEState struct {
	AccountID           int64
	DomainID            int64
	Domain              string
	Username            string
	Subdomain           string
	AllowFrom           []netip.Prefix
	CNAMEStatus         cnamecheck.Status
	CNAMEValue          string
	CNAMECheckedAt      *time.Time
	LastUpdateAt        *time.Time
	LastFailureAt       *time.Time
	ConsecutiveFailures int
}

const acmeStateQuery = `SELECT a.id, a.domain_id, d.fqdn, a.username::text, a.subdomain::text, a.allowfrom,
	a.cname_status, a.cname_value, a.cname_checked_at, a.last_update_at, a.last_failure_at, a.consecutive_failures
	FROM acme_accounts a JOIN domains d ON d.id = a.domain_id
	WHERE a.revoked_at IS NULL`

func scanACMEState(row pgx.CollectableRow) (ACMEState, error) {
	var s ACMEState
	var allow []string
	var cname string
	if err := row.Scan(&s.AccountID, &s.DomainID, &s.Domain, &s.Username, &s.Subdomain, &allow, &cname, &s.CNAMEValue,
		&s.CNAMECheckedAt, &s.LastUpdateAt, &s.LastFailureAt, &s.ConsecutiveFailures); err != nil {
		return ACMEState{}, err
	}
	s.CNAMEStatus = cnamecheck.Status(cname)
	s.CNAMECheckedAt, s.LastUpdateAt, s.LastFailureAt = utcPtr(s.CNAMECheckedAt), utcPtr(s.LastUpdateAt), utcPtr(s.LastFailureAt)
	var err error
	if s.AllowFrom, err = parsePrefixes(allow); err != nil {
		return ACMEState{}, fmt.Errorf("account %d allowfrom: %w", s.AccountID, err)
	}
	return s, nil
}

func (s *Store) ActiveACMEStates(ctx context.Context) ([]ACMEState, error) {
	rows, err := s.pool.Query(ctx, acmeStateQuery+` ORDER BY d.fqdn`)
	if err != nil {
		return nil, fmt.Errorf("store: acme states: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanACMEState)
	if err != nil {
		return nil, fmt.Errorf("store: acme states: %w", err)
	}
	return out, nil
}

// ACMEStateByDomain은 도메인의 활성 계정 상태다. ACME가 꺼져 있으면 ErrNotFound다.
func (s *Store) ACMEStateByDomain(ctx context.Context, domainID int64) (ACMEState, error) {
	rows, err := s.pool.Query(ctx, acmeStateQuery+` AND a.domain_id = $1`, domainID)
	if err != nil {
		return ACMEState{}, fmt.Errorf("store: acme state: %w", err)
	}
	st, err := pgx.CollectExactlyOneRow(rows, scanACMEState)
	if errors.Is(err, pgx.ErrNoRows) {
		return ACMEState{}, ErrNotFound
	}
	if err != nil {
		return ACMEState{}, fmt.Errorf("store: acme state: %w", err)
	}
	return st, nil
}

// SetCNAMEStatus는 CNAME 점검 결과를 남긴다. 조회 오류일 때는 마지막으로 알아낸 값을 지우지 않는다.
func (s *Store) SetCNAMEStatus(ctx context.Context, accountID int64, status cnamecheck.Status, value string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE acme_accounts SET cname_status = $2,
		cname_value = CASE WHEN $2 = 'error' THEN cname_value ELSE $3 END, cname_checked_at = $4 WHERE id = $1`,
		accountID, string(status), value, at)
	if err != nil {
		return fmt.Errorf("store: set cname status: %w", err)
	}
	return nil
}

// RecordUpdateResult는 /update 결과를 남긴다. 성공하면 연속 실패 수를 0으로 되돌린다.
func (s *Store) RecordUpdateResult(ctx context.Context, accountID int64, ok bool) error {
	q := `UPDATE acme_accounts SET last_failure_at = now(), consecutive_failures = consecutive_failures + 1 WHERE id = $1`
	if ok {
		q = `UPDATE acme_accounts SET last_update_at = now(), consecutive_failures = 0 WHERE id = $1`
	}
	if _, err := s.pool.Exec(ctx, q, accountID); err != nil {
		return fmt.Errorf("store: record update result: %w", err)
	}
	return nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
