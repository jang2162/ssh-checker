// Package providertest는 모든 DNSProvider 구현체가 통과해야 하는 적합성 테스트다.
package providertest

import (
	"context"
	"slices"
	"strings"
	"testing"

	"ssl-checker/internal/dnsprovider"
)

// Run은 p로 zone 아래 테스트용 이름에 TXT를 기록하며 계약을 검사한다.
// 실제 공급자에 돌릴 때를 위해 끝나면 만든 이름을 모두 지운다.
func Run(t *testing.T, p dnsprovider.Provider, zone string) {
	t.Helper()
	ctx := context.Background()
	val := func(c byte) string { return strings.Repeat(string(c), 43) }
	name := func(label string) string {
		fqdn := label + "." + zone
		t.Cleanup(func() { _ = p.DeleteTXT(context.Background(), fqdn) })
		return fqdn
	}
	values := func(t *testing.T, fqdn string) []string {
		t.Helper()
		recs, err := p.ListTXT(ctx, zone)
		if err != nil {
			t.Fatalf("ListTXT: %v", err)
		}
		out := []string{}
		for _, r := range recs {
			if r.FQDN == fqdn {
				out = append(out, r.Value)
			}
		}
		slices.Sort(out)
		return out
	}
	set := func(t *testing.T, fqdn string, vals ...string) {
		t.Helper()
		if err := p.SetTXT(ctx, fqdn, vals, 60); err != nil {
			t.Fatalf("SetTXT(%v): %v", vals, err)
		}
	}
	expect := func(t *testing.T, fqdn string, want ...string) {
		t.Helper()
		if want == nil {
			want = []string{}
		}
		if got := values(t, fqdn); !slices.Equal(got, want) {
			t.Fatalf("%s values = %v, want %v", fqdn, got, want)
		}
	}

	t.Run("Check", func(t *testing.T) {
		if err := p.Check(ctx); err != nil {
			t.Fatalf("Check: %v", err)
		}
	})
	t.Run("SetTwoValues", func(t *testing.T) {
		fqdn := name("conf-set-two")
		set(t, fqdn, val('a'), val('b'))
		expect(t, fqdn, val('a'), val('b'))
	})
	t.Run("ReplaceOldest", func(t *testing.T) {
		fqdn := name("conf-replace")
		set(t, fqdn, val('a'), val('b'))
		set(t, fqdn, val('b'), val('c'))
		expect(t, fqdn, val('b'), val('c'))
	})
	t.Run("Idempotent", func(t *testing.T) {
		fqdn := name("conf-idem")
		set(t, fqdn, val('a'), val('b'))
		set(t, fqdn, val('a'), val('b'))
		expect(t, fqdn, val('a'), val('b'))
	})
	t.Run("EmptySetDeletes", func(t *testing.T) {
		fqdn := name("conf-empty")
		set(t, fqdn, val('a'))
		set(t, fqdn)
		expect(t, fqdn)
	})
	t.Run("Delete", func(t *testing.T) {
		fqdn := name("conf-delete")
		set(t, fqdn, val('a'))
		if err := p.DeleteTXT(ctx, fqdn); err != nil {
			t.Fatalf("DeleteTXT: %v", err)
		}
		expect(t, fqdn)
		if err := p.DeleteTXT(ctx, fqdn); err != nil {
			t.Fatalf("DeleteTXT on missing name: %v", err)
		}
	})
	t.Run("ListScopedToZone", func(t *testing.T) {
		fqdn := name("conf-list")
		set(t, fqdn, val('a'))
		recs, err := p.ListTXT(ctx, zone)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if r.FQDN != zone && !strings.HasSuffix(r.FQDN, "."+zone) {
				t.Errorf("record outside zone: %+v", r)
			}
		}
	})
}
