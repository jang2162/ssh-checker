package app

import (
	"fmt"
	"log/slog"

	"ssl-checker/internal/config"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/cloudflare"
	"ssl-checker/internal/dnsprovider/memory"
)

// newProvider는 DNS_PROVIDER 값으로 구현체를 고른다.
func newProvider(cfg config.Config) (dnsprovider.Provider, error) {
	switch cfg.DNSProvider {
	case "cloudflare":
		return cloudflare.New(cloudflare.Config{Token: cfg.CFAPIToken, Zone: cfg.ChallengeZone, ZoneID: cfg.CFZoneID}), nil
	case "memory":
		slog.Warn("DNS_PROVIDER=memory: TXT records are kept in memory only and never published to DNS")
		return memory.New(), nil
	}
	return nil, fmt.Errorf("unsupported DNS_PROVIDER %q", cfg.DNSProvider)
}
