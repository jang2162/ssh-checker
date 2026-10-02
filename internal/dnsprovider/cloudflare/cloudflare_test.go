package cloudflare

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/providertest"
)

func v(c byte) string { return strings.Repeat(string(c), 43) }

func TestConformance(t *testing.T) {
	f := newFakeCF(t)
	providertest.Run(t, f.provider(), f.zoneName)
}

func TestConformanceWithQuotedContent(t *testing.T) {
	f := newFakeCF(t)
	f.quote = true
	providertest.Run(t, f.provider(), f.zoneName)
}

func TestQuotedContentIsIdempotent(t *testing.T) {
	f := newFakeCF(t)
	f.quote = true
	p := f.provider()
	ctx := context.Background()
	fqdn := "sub." + f.zoneName
	if err := p.SetTXT(ctx, fqdn, []string{v('a'), v('b')}, 60); err != nil {
		t.Fatal(err)
	}
	before := f.mutations
	if err := p.SetTXT(ctx, fqdn, []string{v('a'), v('b')}, 60); err != nil {
		t.Fatal(err)
	}
	if f.mutations != before {
		t.Fatalf("second identical SetTXT made %d changes", f.mutations-before)
	}
	recs, err := p.ListTXT(ctx, f.zoneName)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if strings.Contains(r.Value, `"`) {
			t.Fatalf("ListTXT returned quoted value %q", r.Value)
		}
	}
}

func TestReplaceUsesUpdateNotDeleteCreate(t *testing.T) {
	f := newFakeCF(t)
	p := f.provider()
	ctx := context.Background()
	fqdn := "sub." + f.zoneName
	if err := p.SetTXT(ctx, fqdn, []string{v('a'), v('b')}, 60); err != nil {
		t.Fatal(err)
	}
	before := f.mutations
	if err := p.SetTXT(ctx, fqdn, []string{v('b'), v('c')}, 60); err != nil {
		t.Fatal(err)
	}
	if got := f.mutations - before; got != 1 {
		t.Fatalf("replacing one value took %d changes, want 1 (PUT)", got)
	}
	if got := f.values(fqdn); !slices.Equal(got, []string{v('b'), v('c')}) {
		t.Fatalf("values = %v", got)
	}
}

func TestPagination(t *testing.T) {
	f := newFakeCF(t)
	f.maxPerPage = 2
	p := f.provider()
	ctx := context.Background()
	for _, label := range []string{"one", "two", "three"} {
		if err := p.SetTXT(ctx, label+"."+f.zoneName, []string{v('a'), v('b')}, 60); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := p.ListTXT(ctx, f.zoneName)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 6 {
		t.Fatalf("ListTXT returned %d records across pages, want 6", len(recs))
	}
}

func TestTTLClampedToMinimum(t *testing.T) {
	f := newFakeCF(t)
	if err := f.provider().SetTXT(context.Background(), "sub."+f.zoneName, []string{v('a')}, 30); err != nil {
		t.Fatalf("ttl 30 should be raised to 60: %v", err)
	}
}

func TestZoneIDSkipsLookup(t *testing.T) {
	f := newFakeCF(t)
	p := New(Config{Token: f.token, Zone: "not-listed.test", ZoneID: f.zoneID, BaseURL: f.srv.URL})
	if err := p.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestErrorClassification(t *testing.T) {
	ctx := context.Background()
	t.Run("zone not found", func(t *testing.T) {
		f := newFakeCF(t)
		p := New(Config{Token: f.token, Zone: "missing.test", BaseURL: f.srv.URL})
		if err := p.Check(ctx); dnsprovider.KindOf(err) != dnsprovider.KindConfig || err == nil {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad token", func(t *testing.T) {
		f := newFakeCF(t)
		p := New(Config{Token: "wrong", Zone: f.zoneName, BaseURL: f.srv.URL})
		err := p.Check(ctx)
		if dnsprovider.KindOf(err) != dnsprovider.KindConfig || err == nil {
			t.Fatalf("err = %v", err)
		}
		if strings.Contains(err.Error(), "wrong") {
			t.Fatalf("error leaks token: %v", err)
		}
	})
	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			f := newFakeCF(t)
			p := New(Config{Token: f.token, Zone: f.zoneName, ZoneID: f.zoneID, BaseURL: f.srv.URL})
			f.failStatus = status
			err := p.SetTXT(ctx, "sub."+f.zoneName, []string{v('a')}, 60)
			if dnsprovider.KindOf(err) != dnsprovider.KindTransient || err == nil {
				t.Fatalf("err = %v", err)
			}
		})
	}
	t.Run("network error", func(t *testing.T) {
		f := newFakeCF(t)
		p := New(Config{Token: f.token, Zone: f.zoneName, ZoneID: f.zoneID, BaseURL: f.srv.URL})
		f.srv.Close()
		err := p.SetTXT(ctx, "sub."+f.zoneName, []string{v('a')}, 60)
		if dnsprovider.KindOf(err) != dnsprovider.KindTransient || err == nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestListGuardsAgainstIgnoredNameFilter(t *testing.T) {
	f := newFakeCF(t)
	p := f.provider()
	ctx := context.Background()
	other, target := "other."+f.zoneName, "target."+f.zoneName
	if err := p.SetTXT(ctx, other, []string{v('x')}, 60); err != nil {
		t.Fatal(err)
	}
	f.ignoreNameFilter = true
	if err := p.SetTXT(ctx, target, []string{v('a'), v('b')}, 60); err != nil {
		t.Fatal(err)
	}
	if got := f.values(other); !slices.Equal(got, []string{v('x')}) {
		t.Fatalf("SetTXT touched other name: %v", got)
	}
	if err := p.DeleteTXT(ctx, target); err != nil {
		t.Fatal(err)
	}
	if got := f.values(other); !slices.Equal(got, []string{v('x')}) {
		t.Fatalf("DeleteTXT touched other name: %v", got)
	}
	if got := f.values(target); len(got) != 0 {
		t.Fatalf("target not deleted: %v", got)
	}
}

// TestRealCloudflare는 실제 Cloudflare 존에 적합성 테스트를 돌린다.
// CF_TEST_TOKEN, CF_TEST_ZONE을 지정했을 때만 실행한다. 운영 존이 아닌 시험용 존을 쓴다.
func TestRealCloudflare(t *testing.T) {
	token, zone := os.Getenv("CF_TEST_TOKEN"), os.Getenv("CF_TEST_ZONE")
	if token == "" || zone == "" {
		t.Skip("CF_TEST_TOKEN/CF_TEST_ZONE not set")
	}
	providertest.Run(t, New(Config{Token: token, Zone: zone, ZoneID: os.Getenv("CF_TEST_ZONE_ID")}), zone)
}
