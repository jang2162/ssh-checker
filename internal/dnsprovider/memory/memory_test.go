package memory_test

import (
	"context"
	"errors"
	"testing"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/dnsprovider/providertest"
)

func TestConformance(t *testing.T) {
	providertest.Run(t, memory.New(), "acme-chk.test")
}

func TestSetFailure(t *testing.T) {
	p := memory.New()
	p.SetFailure(errors.New("down"))
	err := p.SetTXT(context.Background(), "x.acme-chk.test", []string{"v"}, 60)
	if dnsprovider.KindOf(err) != dnsprovider.KindTransient || err == nil {
		t.Fatalf("err = %v", err)
	}
	if err := p.Check(context.Background()); err == nil {
		t.Fatal("Check succeeded while failing")
	}
	p.SetFailure(nil)
	if err := p.SetTXT(context.Background(), "X.acme-chk.test.", []string{"v"}, 60); err != nil {
		t.Fatal(err)
	}
	if got := p.Values("x.acme-chk.test"); len(got) != 1 || got[0] != "v" {
		t.Fatalf("values = %v", got)
	}
}
