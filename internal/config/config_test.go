package config_test

import (
	"strings"
	"testing"

	"ssl-checker/internal/config"
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
