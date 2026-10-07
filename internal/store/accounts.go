package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"
)

// RotateKey는 활성 계정의 키 해시만 바꾼다. username과 서브도메인이 그대로라 CNAME을 다시 걸 필요가 없다.
// 진행 중인 /update가 계정 행을 잠그고 있으면 끝날 때까지 기다린다.
func (s *Store) RotateKey(ctx context.Context, accountID int64, keyHash string) (Account, error) {
	var a Account
	var allow []string
	err := s.pool.QueryRow(ctx, `UPDATE acme_accounts a SET key_hash = $2
		FROM domains d
		WHERE a.id = $1 AND a.revoked_at IS NULL AND d.id = a.domain_id
		RETURNING a.id, a.domain_id, d.fqdn, a.username::text, a.key_hash, a.subdomain::text, a.allowfrom, a.created_at`,
		accountID, keyHash).Scan(&a.ID, &a.DomainID, &a.Domain, &a.Username, &a.KeyHash, &a.Subdomain, &allow, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: rotate key: %w", err)
	}
	if a.AllowFrom, err = parsePrefixes(allow); err != nil {
		return Account{}, fmt.Errorf("store: account %d allowfrom: %w", a.ID, err)
	}
	return a, nil
}

// RevokeAccount는 계정 행을 잠근 채 beforeCommit(공급자 TXT 삭제)을 부르고, 성공하면 TXT 기록을 지우고
// 계정을 폐기한다. beforeCommit이 실패하면 아무것도 바꾸지 않는다. 잠금 덕분에 진행 중인 /update와 겹치지 않는다.
func (s *Store) RevokeAccount(ctx context.Context, accountID int64, beforeCommit func(ctx context.Context, subdomain string) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var sub string
		err := tx.QueryRow(ctx, `SELECT subdomain::text FROM acme_accounts WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, accountID).Scan(&sub)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: lock account: %w", err)
		}
		if err := beforeCommit(ctx, sub); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM txt_records WHERE account_id = $1`, accountID); err != nil {
			return fmt.Errorf("store: delete txt: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, accountID); err != nil {
			return fmt.Errorf("store: revoke account: %w", err)
		}
		return nil
	})
}

func (s *Store) SetAllowFrom(ctx context.Context, accountID int64, allow []netip.Prefix) error {
	tag, err := s.pool.Exec(ctx, `UPDATE acme_accounts SET allowfrom = $2 WHERE id = $1 AND revoked_at IS NULL`,
		accountID, prefixStrings(allow))
	if err != nil {
		return fmt.Errorf("store: set allowfrom: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
