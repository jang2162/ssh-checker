package dnsprovider_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"ssl-checker/internal/dnsprovider"
)

func TestKindOf(t *testing.T) {
	cfgErr := &dnsprovider.Error{Kind: dnsprovider.KindConfig, Op: "set", Err: errors.New("forbidden")}
	if got := dnsprovider.KindOf(fmt.Errorf("wrapped: %w", cfgErr)); got != dnsprovider.KindConfig {
		t.Fatalf("got %v", got)
	}
	if got := dnsprovider.KindOf(errors.New("plain")); got != dnsprovider.KindTransient {
		t.Fatalf("plain error kind = %v", got)
	}
	if dnsprovider.KindConfig.String() != "config" || dnsprovider.KindTransient.String() != "transient" {
		t.Fatal("kind strings")
	}
}

func TestDedupe(t *testing.T) {
	got := dnsprovider.Dedupe([]string{"b", "a", "b", "", "a"})
	if !slices.Equal(got, []string{"b", "a"}) {
		t.Fatalf("got %v", got)
	}
}
