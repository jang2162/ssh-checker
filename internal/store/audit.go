package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuditRecord struct {
	ID int64
	At time.Time
	AuditEntry
}

// ListAudit는 감사 로그를 최신순으로 돌려준다. beforeID가 0보다 크면 그보다 오래된 것만 준다(다음 페이지).
func (s *Store) ListAudit(ctx context.Context, beforeID int64, limit int) ([]AuditRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, at, actor, action, target, source_ip, detail FROM audit_logs
		WHERE $1::bigint = 0 OR id < $1::bigint ORDER BY id DESC LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditRecord, error) {
		var r AuditRecord
		err := row.Scan(&r.ID, &r.At, &r.Actor, &r.Action, &r.Target, &r.SourceIP, &r.Detail)
		r.At = r.At.UTC()
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	return out, nil
}
