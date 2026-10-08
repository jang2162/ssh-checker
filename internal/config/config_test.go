package config_test

import (
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/config"
	"ssl-checker/internal/lang"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func apiEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL":    "postgres://x",
		"DNS_PROVIDER":    "cloudflare",
		"CF_API_TOKEN":    "tok",
		"CHALLENGE_ZONE":  "ACME-chk.net.",
		"ADMIN_KEY":       strings.Repeat("k", 32),
		"TRUSTED_PROXIES": "10.0.0.0/8, 192.168.1.1",
	}
}

func TestLoadAPI(t *testing.T) {
	cfg, err := config.Load(config.RoleAPI, env(apiEnv()))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChallengeZone != "acme-chk.net" || cfg.TXTTTL != 60 || cfg.CFAPIToken != "tok" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.TrustedProxies) != 2 || cfg.TrustedProxies[1].String() != "192.168.1.1/32" {
		t.Fatalf("trusted proxies: %v", cfg.TrustedProxies)
	}
}

func TestLoadMigrateNeedsOnlyDatabaseURL(t *testing.T) {
	if _, err := config.Load(config.RoleMigrate, env(map[string]string{"DATABASE_URL": "postgres://x"})); err != nil {
		t.Fatal(err)
	}
}

func TestLoadWorkerDoesNotNeedAdminKey(t *testing.T) {
	m := apiEnv()
	delete(m, "ADMIN_KEY")
	m["DNS_PROVIDER"] = "memory"
	if _, err := config.Load(config.RoleWorker, env(m)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name string
		role config.Role
		edit func(map[string]string)
		want string
	}{
		{"no database", config.RoleMigrate, func(m map[string]string) { delete(m, "DATABASE_URL") }, "DATABASE_URL"},
		{"no admin key", config.RoleAPI, func(m map[string]string) { delete(m, "ADMIN_KEY") }, "ADMIN_KEY"},
		{"short admin key", config.RoleAPI, func(m map[string]string) { m["ADMIN_KEY"] = "short" }, "ADMIN_KEY"},
		{"cloudflare without token", config.RoleAPI, func(m map[string]string) { delete(m, "CF_API_TOKEN") }, "CF_API_TOKEN"},
		{"no provider", config.RoleWorker, func(m map[string]string) { delete(m, "DNS_PROVIDER") }, "DNS_PROVIDER"},
		{"unknown provider", config.RoleWorker, func(m map[string]string) { m["DNS_PROVIDER"] = "route53" }, "DNS_PROVIDER"},
		{"zone without dot", config.RoleWorker, func(m map[string]string) { m["CHALLENGE_ZONE"] = "localhost" }, "CHALLENGE_ZONE"},
		{"ttl not number", config.RoleWorker, func(m map[string]string) { m["TXT_TTL"] = "abc" }, "TXT_TTL"},
		{"ttl zero", config.RoleWorker, func(m map[string]string) { m["TXT_TTL"] = "0" }, "TXT_TTL"},
		{"bad proxies", config.RoleAPI, func(m map[string]string) { m["TRUSTED_PROXIES"] = "10.0.0.0/8,bad" }, "TRUSTED_PROXIES"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := apiEnv()
			c.edit(m)
			_, err := config.Load(c.role, env(m))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want mention of %s", err, c.want)
			}
		})
	}
}

func workerEnv() map[string]string {
	m := apiEnv()
	delete(m, "ADMIN_KEY")
	return m
}

func TestMonitoringDefaults(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker} {
		cfg, err := config.Load(role, env(apiEnv()))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.CheckInterval != 6*time.Hour || cfg.DNSResolver != "1.1.1.1:53" {
			t.Fatalf("role %d defaults = %s %s", role, cfg.CheckInterval, cfg.DNSResolver)
		}
	}
}

func TestCheckInterval(t *testing.T) {
	m := workerEnv()
	m["CHECK_INTERVAL"] = "30m"
	cfg, err := config.Load(config.RoleWorker, env(m))
	if err != nil || cfg.CheckInterval != 30*time.Minute {
		t.Fatalf("30m = %s, %v", cfg.CheckInterval, err)
	}
	for _, bad := range []string{"4m", "abc", "-1h"} {
		m["CHECK_INTERVAL"] = bad
		if _, err := config.Load(config.RoleWorker, env(m)); err == nil || !strings.Contains(err.Error(), "CHECK_INTERVAL") {
			t.Errorf("CHECK_INTERVAL=%q err = %v", bad, err)
		}
	}
}

func TestDNSResolver(t *testing.T) {
	cases := map[string]string{
		"8.8.8.8:53":        "8.8.8.8:53",
		"9.9.9.9":           "9.9.9.9:53",
		"[2606:4700::1111]": "[2606:4700::1111]:53",
		"2606:4700::1111":   "[2606:4700::1111]:53",
		"dns.internal:5353": "dns.internal:5353",
		"  1.0.0.1:53  ":    "1.0.0.1:53",
	}
	for in, want := range cases {
		m := workerEnv()
		m["DNS_RESOLVER"] = in
		cfg, err := config.Load(config.RoleWorker, env(m))
		if err != nil || cfg.DNSResolver != want {
			t.Errorf("DNS_RESOLVER=%q = %q, %v; want %q", in, cfg.DNSResolver, err, want)
		}
	}
	for _, bad := range []string{"dns.internal", "1.1.1.1:0", "1.1.1.1:abc", ":53"} {
		m := workerEnv()
		m["DNS_RESOLVER"] = bad
		if _, err := config.Load(config.RoleWorker, env(m)); err == nil || !strings.Contains(err.Error(), "DNS_RESOLVER") {
			t.Errorf("DNS_RESOLVER=%q err = %v", bad, err)
		}
	}
}

func TestPublicURL(t *testing.T) {
	cases := map[string]string{
		"":                                 "",
		"https://ssl-checker.example.com":  "https://ssl-checker.example.com",
		"https://ssl-checker.example.com/": "https://ssl-checker.example.com",
		"http://10.0.0.5:8080":             "http://10.0.0.5:8080",
	}
	for in, want := range cases {
		m := apiEnv()
		m["PUBLIC_URL"] = in
		cfg, err := config.Load(config.RoleAPI, env(m))
		if err != nil || cfg.PublicURL != want {
			t.Errorf("PUBLIC_URL=%q = %q, %v; want %q", in, cfg.PublicURL, err, want)
		}
	}
	for _, bad := range []string{"ssl-checker.example.com", "ftp://x.com", "https://x.com/app", "https://x.com?a=1", "https://"} {
		m := apiEnv()
		m["PUBLIC_URL"] = bad
		if _, err := config.Load(config.RoleAPI, env(m)); err == nil || !strings.Contains(err.Error(), "PUBLIC_URL") {
			t.Errorf("PUBLIC_URL=%q err = %v", bad, err)
		}
	}
}

// UI_LANGUAGE는 api가 읽는다. 비우면 en이고, 모르는 값은 시작하지 않는다.
func TestLoadUILanguage(t *testing.T) {
	for in, want := range map[string]lang.Lang{"": lang.EN, "en": lang.EN, "ko": lang.KO, " KO ": lang.KO} {
		m := apiEnv()
		m["UI_LANGUAGE"] = in
		cfg, err := config.Load(config.RoleAPI, env(m))
		if err != nil || cfg.Language != want {
			t.Errorf("UI_LANGUAGE=%q = %q, %v; want %q", in, cfg.Language, err, want)
		}
	}
	m := apiEnv()
	m["UI_LANGUAGE"] = "fr"
	if _, err := config.Load(config.RoleAPI, env(m)); err == nil || !strings.Contains(err.Error(), "UI_LANGUAGE") {
		t.Errorf("fr should be rejected: %v", err)
	}
	// worker와 migrate는 언어를 쓰지 않아서 잘못된 값이어도 시작한다.
	m = map[string]string{"DATABASE_URL": "postgres://x", "UI_LANGUAGE": "fr"}
	if _, err := config.Load(config.RoleMigrate, env(m)); err != nil {
		t.Errorf("migrate must ignore UI_LANGUAGE: %v", err)
	}
}
