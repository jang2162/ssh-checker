// Package cleanup은 DNS 공급자의 챌린지 TXT를 DB와 대조해 고아 레코드를 정리한다.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/store"
)

type Cleaner struct {
	Store    *store.Store
	Provider dnsprovider.Provider
	Zone     string
	TTL      int
	Log      *slog.Logger
}

type Result struct {
	Deleted  int // 활성 계정이 없는 이름의 TXT를 지운 수
	Repaired int // 활성 계정의 TXT를 DB 값으로 다시 맞춘 수
}

// Run은 이름이 <UUID>.<zone>인 레코드만 다룬다. 존의 다른 레코드는 건드리지 않는다.
// 한 이름의 실패가 나머지 정리를 막지 않도록, 오류는 모아서 마지막에 돌려준다.
func (c *Cleaner) Run(ctx context.Context) (Result, error) {
	var res Result
	recs, err := c.Provider.ListTXT(ctx, c.Zone)
	if err != nil {
		return res, fmt.Errorf("cleanup: list provider records: %w", err)
	}
	active, err := c.Store.ActiveTXTBySubdomain(ctx)
	if err != nil {
		return res, fmt.Errorf("cleanup: load active accounts: %w", err)
	}
	have := map[string][]string{}
	for _, r := range recs {
		label, ok := strings.CutSuffix(r.FQDN, "."+c.Zone)
		if !ok || !acmedns.ValidSubdomain(label) {
			continue
		}
		have[label] = append(have[label], r.Value)
	}

	var errs []error
	for _, label := range slices.Sorted(maps.Keys(have)) {
		fqdn := label + "." + c.Zone
		acct, ok := active[label]
		if ok && sameSet(have[label], acct.Values) {
			continue
		}
		if ok {
			// 계정 행을 잠근 채 커밋된 값으로 맞춘다. 진행 중인 /update가 있으면 끝날 때까지 기다린다.
			err := c.Store.UpdateTXT(ctx, acct.AccountID, func(current []string) ([]string, error) {
				return current, c.Provider.SetTXT(ctx, fqdn, current, c.TTL)
			})
			if err == nil {
				res.Repaired++
				c.audit(ctx, fqdn, "repaired")
				continue
			}
			if !errors.Is(err, store.ErrNotFound) {
				errs = append(errs, fmt.Errorf("cleanup: repair %s: %w", fqdn, err))
				continue
			}
			// 그사이 폐기됐다면 아래에서 지운다.
		}
		if err := c.Provider.DeleteTXT(ctx, fqdn); err != nil {
			errs = append(errs, fmt.Errorf("cleanup: delete %s: %w", fqdn, err))
			continue
		}
		res.Deleted++
		c.audit(ctx, fqdn, "deleted")
	}
	return res, errors.Join(errs...)
}

func (c *Cleaner) audit(ctx context.Context, fqdn, detail string) {
	if err := c.Store.Audit(ctx, store.AuditEntry{Actor: "worker", Action: "txt_cleanup", Target: fqdn, Detail: detail}); err != nil {
		c.Log.Error("audit", "err", err)
	}
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}
