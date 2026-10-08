// Package config는 환경변수에서 역할별 실행 설정을 읽고 검증한다.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ssl-checker/internal/lang"
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

	defaultCheckInterval = 6 * time.Hour
	minCheckInterval     = 5 * time.Minute
	defaultDNSResolver   = "1.1.1.1:53"
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
	CheckInterval  time.Duration
	PublicURL      string
	DNSResolver    string
	// Language는 화면, 스크립트, llms.txt의 언어다(UI_LANGUAGE, 기본 en).
	Language lang.Lang
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
	c.CheckInterval = defaultCheckInterval
	if v := strings.TrimSpace(getenv("CHECK_INTERVAL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < minCheckInterval {
			errs = append(errs, fmt.Errorf("CHECK_INTERVAL must be a duration of at least %s, got %q", minCheckInterval, v))
		} else {
			c.CheckInterval = d
		}
	}
	resolver, err := parseResolver(getenv("DNS_RESOLVER"))
	if err != nil {
		errs = append(errs, fmt.Errorf("DNS_RESOLVER: %w", err))
	}
	c.DNSResolver = resolver
	if role == RoleAPI {
		if len(c.AdminKey) < minAdminKeyLen {
			errs = append(errs, fmt.Errorf("ADMIN_KEY must be at least %d bytes", minAdminKeyLen))
		}
		proxies, err := netutil.ParsePrefixes(getenv("TRUSTED_PROXIES"))
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES: %w", err))
		}
		c.TrustedProxies = proxies
		publicURL, err := parsePublicURL(getenv("PUBLIC_URL"))
		if err != nil {
			errs = append(errs, fmt.Errorf("PUBLIC_URL: %w", err))
		}
		c.PublicURL = publicURL
		language, err := lang.Parse(getenv("UI_LANGUAGE"))
		if err != nil {
			errs = append(errs, err)
		}
		c.Language = language
	}
	return c, errors.Join(errs...)
}

// parseResolver는 host:port를 받는다. 포트 없는 IP는 53번 포트로 본다. 포트 없는 호스트명은
// IPv6 주소와 헷갈리지 않게 거부한다.
func parseResolver(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return defaultDNSResolver, nil
	}
	if ip, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return net.JoinHostPort(ip.String(), "53"), nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", fmt.Errorf("want host:port, got %q", s)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("port must be 1..65535, got %q", s)
	}
	return s, nil
}

// parsePublicURL은 외부에서 api에 닿는 주소를 scheme://host[:port]로 정규화한다. 경로는 받지 않는다.
func parsePublicURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("want http(s)://host[:port], got %q", s)
	}
	return u.Scheme + "://" + u.Host, nil
}
