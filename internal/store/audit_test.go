package store_test

import (
	"context"
	"testing"

	"ssl-checker/internal/store"
)

func TestListAudit(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	for _, action := range []string{"a1", "a2", "a3"} {
		if err := st.Audit(ctx, store.AuditEntry{Actor: "admin", Action: action, Target: "a.com", SourceIP: "10.0.0.1", Detail: "d"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.ListAudit(ctx, 0, 10)
	if err != nil || len(all) != 3 || all[0].Action != "a3" || all[2].Action != "a1" || all[0].At.IsZero() ||
		all[0].Actor != "admin" || all[0].Target != "a.com" || all[0].SourceIP != "10.0.0.1" || all[0].Detail != "d" {
		t.Fatalf("ListAudit = %+v, %v", all, err)
	}
	page, err := st.ListAudit(ctx, all[0].ID, 1)
	if err != nil || len(page) != 1 || page[0].Action != "a2" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if empty, _ := st.ListAudit(ctx, all[2].ID, 10); len(empty) != 0 {
		t.Fatalf("before first = %+v", empty)
	}
}
