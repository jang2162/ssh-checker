// Package config는 환경변수에서 역할별 실행 설정을 읽고 검증한다.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"ssl-checker/internal/netutil"
)

// Role은 실행할 하위 명령이다. 역할마다 필요한 변수가 다르다.
type Role int

const (
	RoleMigrate Role = iota + 1
	RoleAPI
	RoleWorker
)

const (
	defaultTXTTTL  = 60
	minAdminKeyLen = 32
)

type Config struct {
	DatabaseURL    string
	DNSProvider    string
	ChallengeZone  string
	TXTTTL         int
	CFAPIToken     string
	CFZoneID       string
	AdminKey       string
	TrustedProxies []netip.Prefix
}

// Load는 role에 필요한 변수를 읽는다. 문제가 여러 개면 한 번에 모두 돌려준다.
func Load(role Role, getenv func(string) string) (Config, error) {
	c := Config{
		DatabaseURL:   strings.TrimSpace(getenv("DATABASE_URL")),
		DNSProvider:   strings.TrimSpace(getenv("DNS_PROVIDER")),
		ChallengeZone: netutil.NormalizeName(getenv("CHALLENGE_ZONE")),
		TXTTTL:        defaultTXTTTL,
		CFAPIToken:    strings.TrimSpace(getenv("CF_API_TOKEN")),
		CFZoneID:      strings.TrimSpace(getenv("CF_ZONE_ID")),
		AdminKey:      getenv("ADMIN_KEY"),
	}
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if role == RoleMigrate {
		return c, errors.Join(errs...)
	}

	switch c.DNSProvider {
	case "cloudflare":
		if c.CFAPIToken == "" {
			errs = append(errs, errors.New("CF_API_TOKEN is required when DNS_PROVIDER=cloudflare"))
		}
	case "memory":
	case "":
		errs = append(errs, errors.New("DNS_PROVIDER is required"))
	default:
		errs = append(errs, fmt.Errorf("DNS_PROVIDER %q is not supported (cloudflare, memory)", c.DNSProvider))
	}
	if !strings.Contains(c.ChallengeZone, ".") {
		errs = append(errs, fmt.Errorf("CHALLENGE_ZONE must be a domain name, got %q", c.ChallengeZone))
	}
	if v := strings.TrimSpace(getenv("TXT_TTL")); v != "" {
		ttl, err := strconv.Atoi(v)
		if err != nil || ttl < 1 || ttl > 86400 {
			errs = append(errs, fmt.Errorf("TXT_TTL must be 1..86400 seconds, got %q", v))
		} else {
			c.TXTTTL = ttl
		}
	}
	if role == RoleAPI {
		if len(c.AdminKey) < minAdminKeyLen {
			errs = append(errs, fmt.Errorf("ADMIN_KEY must be at least %d bytes", minAdminKeyLen))
		}
		proxies, err := netutil.ParsePrefixes(getenv("TRUSTED_PROXIES"))
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %w", err))
		}
		c.TrustedProxies = proxies
	}
	return c, errors.Join(errs...)
}
