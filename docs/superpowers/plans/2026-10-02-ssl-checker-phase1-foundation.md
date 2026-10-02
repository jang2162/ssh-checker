# ssl-checker 1단계(기반) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** acme.sh의 `dns_acmedns` 플러그인이 그대로 붙는 acme-dns 호환 API(`/register`, `/update`, `/health`)와 DNS 공급자 인터페이스(memory, Cloudflare), DB·감사 로그, 단일 이미지 배포 구성을 만들어 첫 게이트(Let's Encrypt 스테이징 호환 테스트)를 통과할 수 있는 상태로 만든다.

**Architecture:** Go 단일 바이너리 `ssl-checker`가 하위 명령 `migrate`, `serve`, `worker`, `healthcheck`를 제공한다. `/update`는 계정 행을 `SELECT … FOR UPDATE`로 잠근 채 "최근 값 2개 유지" 규칙으로 다음 값 집합을 계산하고, `DNSProvider.SetTXT`(원하는 상태 전달)가 성공했을 때만 DB에 커밋한다. worker는 하루 한 번 공급자 레코드와 DB를 대조해 고아 TXT를 정리한다.

**Tech Stack:** Go 1.27, PostgreSQL 17, `github.com/jackc/pgx/v5`, `golang.org/x/crypto/bcrypt`, `github.com/google/uuid`, `golang.org/x/time/rate`, 표준 `net/http`(1.22+ 패턴 라우팅), Docker(distroless), Docker Compose(Komodo Stack)

**Spec:** 「ssl-checker 구현 계획서 (acme-dns 호환 API 방식)」 https://claude.ai/code/artifact/4b4d7215-cacb-4e14-967d-ddb156a69e9d (Claude Docs 문서. docs 도구의 `read`로 읽는다.) 이 계획은 그 문서의 「일정과 단계별 산출물」 중 **1. 기반** 단계와 첫 게이트 **호환 테스트 통과(스테이징 발급 성공)** 를 구현한다. 2단계(관리 화면·CNAME 확인·부트스트랩), 3단계(TLS 모니터링)는 별도 계획으로 작성한다.

## Global Constraints

- 모듈 경로: `ssl-checker`. 바이너리 이름: `ssl-checker`. 하위 명령: `migrate`, `serve --listen :8080`, `worker`, `healthcheck`.
- `/update` 요청: `POST /update`, 헤더 `X-Api-User`, `X-Api-Key`, 본문 `{"subdomain": "<uuid>", "txt": "<43자>"}`. 성공 시 200과 `{"txt": "<저장한 값>"}`. 오류 응답은 `{"error": "..."}`.
- `txt` 값: 정확히 43자, `[A-Za-z0-9_-]`만 허용.
- 값 유지: 서브도메인당 최근 값 2개. 3번째 값이 오면 가장 오래된 값을 교체.
- DNS 공급자 반영에 실패하면 `/update`는 5xx를 돌려준다.
- 레코드 이름: `<subdomain>.<CHALLENGE_ZONE>`, subdomain은 무작위 UUID. 도메인 이름에서 계산한 값은 쓰지 않는다.
- TTL 기본 60초 (`TXT_TTL`). Cloudflare 최소 TTL 60초.
- 키는 bcrypt 해시로만 저장한다. 평문은 등록 응답에서 한 번만 노출한다.
- `ADMIN_KEY`: 32바이트 이상. 비교는 상수 시간, 인증 실패는 IP별 횟수 제한. 코드·로그에 남기지 않는다.
- `allowfrom` CIDR이 있으면 클라이언트 IP가 포함돼야 한다. 프록시 헤더는 `TRUSTED_PROXIES`에 속한 프록시가 보낸 것만 쓴다.
- `/update`에 계정별 요청 빈도 제한을 둔다.
- 감사 로그: register, update(성공·실패·거부), 정리 작업을 `audit_logs`에 기록한다. 관리자는 개인 구분이 없으므로 `source_ip`로 추적한다.
- 환경변수: `DATABASE_URL`, `DNS_PROVIDER`(`cloudflare`|`memory`), `CHALLENGE_ZONE`, `TXT_TTL`, `CF_API_TOKEN`, `CF_ZONE_ID`(선택), `ADMIN_KEY`(api만), `TRUSTED_PROXIES`(api만), `CHECK_INTERVAL`(3단계에서 사용).
- 이미지 하나로 `migrate`, `api`, `worker` 세 서비스를 실행하고, 헬스체크는 `ssl-checker healthcheck` 하위 명령으로 처리한다(이미지에 curl·wget 없음).
- 비밀값은 compose 파일이나 `env_file`에 직접 쓰지 않는다. Komodo `[[이름]]` 보간으로만 넣는다.
- 개발·테스트 발급은 Let's Encrypt **스테이징**에서만 한다.
- 모든 커밋 메시지는 마지막 줄에 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`를 넣는다(아래 커밋 명령은 이를 위해 `-m`을 두 번 쓴다).
- 코드 주석은 한국어, 식별자는 영어.

### 문서와 다르게 정한 것 (리뷰 시 확인)

- `/register`의 "내부 호출만 허용"은 `Authorization: Bearer <ADMIN_KEY>`로 구현한다. 2단계의 `POST /api/domains`가 같은 서비스 함수를 호출한다. 본문에 `domain`을 받아 `domains` 행과 연결한다(원본 acme-dns에는 없는 필드).
- 1단계 마이그레이션은 `domains`, `acme_accounts`, `txt_records`, `audit_logs` 4개만 만든다. `monitor_targets`, `check_results`는 3단계 마이그레이션에서 추가한다.
- `audit_logs`에 `detail` 컬럼을 추가한다(실패 사유, 공급자 오류 종류 기록용).
- `txt_records.provider_ref`는 컬럼만 만들고 1단계에서는 채우지 않는다. 문서의 인터페이스(`SetTXT`가 `error`만 반환)를 그대로 따르고, Cloudflare 구현체는 매번 이름으로 레코드를 조회한다. 문서의 "코어 로직은 이 값에 의존하지 않는다" 원칙과 맞는다.
- `CF_ZONE_ID`(선택)를 추가한다. 존 단위 DNS 편집 토큰은 `GET /zones` 조회 권한이 없을 수 있어서, 비어 있으면 이름으로 조회하고 실패하면 설정 오류로 표시한다.
- compose의 api 포트에 `${API_PORT:-8080}`을 추가한다(로컬 smoke 테스트 충돌 방지). 기본값은 문서와 같다.

## Review Focus

1. **정리 작업의 삭제 범위:** 챌린지 존에 UUID가 아닌 이름(존 자체 레코드, 다른 용도 TXT)이 있어도 worker 정리는 절대 지우지 않아야 한다. → Task 9 `TestCleanupLeavesNonUUIDNames`.
2. **X-Forwarded-For 위조:** 신뢰하지 않는 주소에서 온 요청이 `X-Forwarded-For`로 허용 대역 IP를 주장해도 `allowfrom`을 통과하면 안 된다. 신뢰 프록시 뒤에서는 오른쪽부터 첫 비신뢰 주소를 써야 한다. → Task 7 `TestClientIP`, Task 8 `TestUpdateBehindTrustedProxy`.
3. **Cloudflare의 따옴표 TXT:** Cloudflare가 TXT content를 `"값"` 형태로 돌려줘도 같은 값으로 인식해, 같은 입력을 다시 넣을 때 레코드를 새로 만들거나 바꾸지 않아야 한다. → Task 4 `TestQuotedContentIsIdempotent`.
4. **같은 계정의 동시 `/update`:** a.com과 \*.a.com, 또는 여러 서버가 동시에 갱신해도 값이 2개를 넘거나 공급자와 DB가 어긋나면 안 된다. → Task 6 `TestUpdateTXTConcurrent`.
5. **이상한 인증 입력:** UUID가 아닌 `X-Api-User`, 72바이트를 넘는 키, 4KB를 넘는 본문은 500이 아니라 `401 {"error":"forbidden"}`이어야 한다. → Task 8 `TestUpdateForbidden`.

## File Structure

```
cmd/ssl-checker/main.go              하위 명령 분기
internal/netutil/netutil.go          DNS 이름 정규화, CIDR/IP 파싱
internal/config/config.go            역할별 환경변수 로드·검증
internal/db/db.go                    pgxpool 연결
internal/db/migrate.go               내장 SQL 마이그레이션 실행
internal/db/migrations/0001_init.sql
internal/db/dbtest/dbtest.go         테스트용 격리 스키마
internal/dnsprovider/provider.go     DNSProvider 인터페이스, 오류 종류
internal/dnsprovider/memory/         테스트·로컬용 구현체
internal/dnsprovider/providertest/   공급자 적합성 테스트
internal/dnsprovider/cloudflare/     Cloudflare REST 구현체
internal/store/store.go              계정·TXT·감사 로그 저장
internal/acmedns/validate.go         txt·subdomain·domain 검증
internal/acmedns/service.go          등록·인증·TXT 갱신 정책
internal/acmedns/health.go           DB + 공급자 상태 확인
internal/acmedns/http.go             /register, /update, /health 핸들러
internal/httpx/                      클라이언트 IP, 키별 제한, 관리자 인증, JSON 응답
internal/cleanup/cleanup.go          고아 TXT 정리
internal/app/                        하위 명령 실행(migrate, serve, worker, healthcheck)
Dockerfile, .dockerignore
deploy/compose.yaml, deploy/local.env.example
scripts/smoke.sh, scripts/compat.sh
test/compat/                         원본 acme-dns 응답 대조 테스트
docs/runbooks/gate1-staging.md       게이트 1 수동 절차
```

## 테스트 DB

DB가 필요한 테스트는 `TEST_DATABASE_URL`이 없으면 건너뛴다. 실행 전에 한 번 띄운다.

```bash
make db-up      # postgres:17 컨테이너를 55432 포트로 띄운다
make test       # TEST_DATABASE_URL을 넣고 go test ./...
make db-down
```

---

### Task 1: 프로젝트 골격, netutil, config

**Files:**
- Create: `go.mod`, `.gitignore`, `Makefile`
- Create: `internal/netutil/netutil.go`, `internal/netutil/netutil_test.go`
- Create: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Produces:
  - `netutil.NormalizeName(s string) string`
  - `netutil.ParsePrefix(s string) (netip.Prefix, error)`, `netutil.ParsePrefixes(s string) ([]netip.Prefix, error)`
  - `config.Role` (`RoleMigrate`, `RoleAPI`, `RoleWorker`), `config.Config{DatabaseURL, DNSProvider, ChallengeZone string; TXTTTL int; CFAPIToken, CFZoneID, AdminKey string; TrustedProxies []netip.Prefix}`, `config.Load(role Role, getenv func(string) string) (Config, error)`

- [ ] **Step 1: 모듈과 기본 파일 만들기**

```bash
cd /Users/jang2162/IdeaProjects/ssh-checker
go mod init ssl-checker
```

`.gitignore`:

```gitignore
/bin/
.env
```

`Makefile`:

```make
TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable

.PHONY: db-up db-down test

db-up:
	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
	until docker exec sslchk-testdb pg_isready -U postgres -h 127.0.0.1 >/dev/null 2>&1; do sleep 1; done

db-down:
	docker rm -f sslchk-testdb

test:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...
```

- [ ] **Step 2: netutil 실패 테스트 작성**

`internal/netutil/netutil_test.go`:

```go
package netutil_test

import (
	"testing"

	"ssl-checker/internal/netutil"
)

func TestNormalizeName(t *testing.T) {
	if got := netutil.NormalizeName(" ACME-chk.NET. "); got != "acme-chk.net" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePrefix(t *testing.T) {
	ok := map[string]string{
		"10.0.0.0/8":      "10.0.0.0/8",
		"10.1.2.3/16":     "10.1.0.0/16",
		"192.168.1.1":     "192.168.1.1/32",
		"2001:db8::1":     "2001:db8::1/128",
		"::ffff:10.0.0.1": "10.0.0.1/32",
	}
	for in, want := range ok {
		p, err := netutil.ParsePrefix(in)
		if err != nil || p.String() != want {
			t.Errorf("ParsePrefix(%q) = %v, %v; want %s", in, p, err, want)
		}
	}
	for _, bad := range []string{"10.0.0.0/33", "nope", ""} {
		if _, err := netutil.ParsePrefix(bad); err == nil {
			t.Errorf("ParsePrefix(%q) succeeded", bad)
		}
	}
}

func TestParsePrefixes(t *testing.T) {
	ps, err := netutil.ParsePrefixes("10.0.0.0/8, ,192.168.1.1")
	if err != nil || len(ps) != 2 || ps[1].String() != "192.168.1.1/32" {
		t.Fatalf("got %v, %v", ps, err)
	}
	if _, err := netutil.ParsePrefixes("10.0.0.0/8,bad"); err == nil {
		t.Fatal("bad entry accepted")
	}
	if ps, err := netutil.ParsePrefixes(""); err != nil || len(ps) != 0 {
		t.Fatalf("empty: %v, %v", ps, err)
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/netutil/`
Expected: FAIL (`undefined: netutil.NormalizeName` 등 컴파일 오류)

- [ ] **Step 4: netutil 구현**

`internal/netutil/netutil.go`:

```go
// Package netutil은 DNS 이름과 IP 대역을 다루는 작은 도우미를 모은다.
package netutil

import (
	"fmt"
	"net/netip"
	"strings"
)

// NormalizeName은 DNS 이름을 소문자로 바꾸고 앞뒤 공백과 끝의 점을 없앤다.
func NormalizeName(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// ParsePrefix는 CIDR 또는 단일 IP를 대역으로 읽는다. 단일 IP는 /32 또는 /128이 된다.
func ParsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("invalid CIDR %q", s)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid IP %q", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// ParsePrefixes는 쉼표로 구분한 CIDR·IP 목록을 읽는다. 빈 항목은 건너뛴다.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, err := ParsePrefix(part)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/netutil/`
Expected: PASS

- [ ] **Step 6: config 실패 테스트 작성**

`internal/config/config_test.go`:

```go
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
```

- [ ] **Step 7: 실패 확인**

Run: `go test ./internal/config/`
Expected: FAIL (`undefined: config.Load`)

- [ ] **Step 8: config 구현**

`internal/config/config.go`:

```go
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
```

- [ ] **Step 9: 통과 확인**

Run: `go test ./internal/...`
Expected: PASS

- [ ] **Step 10: 커밋**

```bash
git add go.mod .gitignore Makefile internal/netutil internal/config
git commit -m "feat: add project skeleton, netutil and env config" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: DB 연결, 마이그레이션, `migrate` 명령

**Files:**
- Create: `internal/db/db.go`, `internal/db/migrate.go`, `internal/db/migrations/0001_init.sql`
- Create: `internal/db/dbtest/dbtest.go`, `internal/db/migrate_test.go`
- Create: `internal/app/migrate.go`, `cmd/ssl-checker/main.go`

**Interfaces:**
- Consumes: `config.Load(config.RoleMigrate, getenv)`
- Produces:
  - `db.Open(ctx context.Context, url string) (*pgxpool.Pool, error)`
  - `db.Migrate(ctx context.Context, pool *pgxpool.Pool) error`
  - `dbtest.New(t *testing.T) *pgxpool.Pool` — 테스트마다 새 스키마를 만들고 마이그레이션을 적용한 풀. `TEST_DATABASE_URL`이 없으면 `t.Skip`.
  - `app.Migrate(ctx context.Context, getenv func(string) string) error`
  - 테이블: `domains(id, fqdn UNIQUE, memo, status, created_at)`, `acme_accounts(id, domain_id, username uuid UNIQUE, key_hash, subdomain uuid UNIQUE, allowfrom text[], created_at, revoked_at)` + 부분 유니크 인덱스 `acme_accounts_one_active(domain_id) WHERE revoked_at IS NULL`, `txt_records(id, account_id, value, provider_ref, created_at, UNIQUE(account_id, value))`, `audit_logs(id, actor, action, target, source_ip, detail, at)`

- [ ] **Step 1: 의존성 추가**

```bash
go get github.com/jackc/pgx/v5@latest
```

- [ ] **Step 2: 테스트 DB 띄우기**

Run: `make db-up`
Expected: 컨테이너 ID 출력 후 프롬프트 복귀

- [ ] **Step 3: 마이그레이션 SQL 작성**

`internal/db/migrations/0001_init.sql`:

```sql
CREATE TABLE domains (
    id         bigserial PRIMARY KEY,
    fqdn       text        NOT NULL UNIQUE,
    memo       text        NOT NULL DEFAULT '',
    status     text        NOT NULL DEFAULT 'pending_cname',
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE acme_accounts (
    id         bigserial PRIMARY KEY,
    domain_id  bigint      NOT NULL REFERENCES domains (id),
    username   uuid        NOT NULL UNIQUE,
    key_hash   text        NOT NULL,
    subdomain  uuid        NOT NULL UNIQUE,
    allowfrom  text[]      NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz
);

-- 도메인당 활성 계정은 1개. 재발급하면 기존 행에 revoked_at을 기록한다.
CREATE UNIQUE INDEX acme_accounts_one_active ON acme_accounts (domain_id) WHERE revoked_at IS NULL;

CREATE TABLE txt_records (
    id           bigserial PRIMARY KEY,
    account_id   bigint      NOT NULL REFERENCES acme_accounts (id),
    value        text        NOT NULL,
    provider_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, value)
);

CREATE TABLE audit_logs (
    id        bigserial PRIMARY KEY,
    actor     text        NOT NULL,
    action    text        NOT NULL,
    target    text        NOT NULL DEFAULT '',
    source_ip text        NOT NULL DEFAULT '',
    detail    text        NOT NULL DEFAULT '',
    at        timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_logs_at ON audit_logs (at DESC);
```

- [ ] **Step 4: 테스트 도우미와 실패 테스트 작성**

`internal/db/dbtest/dbtest.go`:

```go
// Package dbtest는 테스트마다 격리된 스키마에 마이그레이션을 적용한 연결 풀을 만든다.
package dbtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/db"
)

// New는 TEST_DATABASE_URL에 임시 스키마를 만들고, search_path를 그 스키마로 둔 풀을 돌려준다.
// 테스트가 끝나면 스키마를 지운다. 변수가 없으면 테스트를 건너뛴다.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; run `make db-up` and `make test`")
	}
	ctx := context.Background()
	schema := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))

	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, err := pgx.Connect(context.Background(), url)
		if err != nil {
			return
		}
		defer c.Close(context.Background())
		_, _ = c.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	})
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}
```

`internal/db/migrate_test.go`:

```go
package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"ssl-checker/internal/db"
	"ssl-checker/internal/db/dbtest"
)

func TestMigrateCreatesTablesAndIsIdempotent(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	for _, table := range []string{"domains", "acme_accounts", "txt_records", "audit_logs"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s missing", table)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
}

func TestOneActiveAccountPerDomain(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	var domainID int64
	if err := pool.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ('a.com') RETURNING id`).Scan(&domainID); err != nil {
		t.Fatal(err)
	}
	insert := func(user, sub string) error {
		_, err := pool.Exec(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain) VALUES ($1, $2, 'h', $3)`,
			domainID, user, sub)
		return err
	}
	if err := insert("11111111-1111-1111-1111-111111111111", "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	err := insert("22222222-2222-2222-2222-222222222222", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.ConstraintName != "acme_accounts_one_active" {
		t.Fatalf("second active account: err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	if err := insert("22222222-2222-2222-2222-222222222222", "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"); err != nil {
		t.Fatalf("after revoke: %v", err)
	}
}
```

- [ ] **Step 5: 실패 확인**

Run: `make test`
Expected: FAIL (`undefined: db.Migrate`)

- [ ] **Step 6: db 패키지 구현**

`internal/db/db.go`:

```go
// Package db는 PostgreSQL 연결과 스키마 마이그레이션을 맡는다.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open은 연결 풀을 만들고 한 번 Ping해 접속을 확인한다.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("db: open: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}
```

`internal/db/migrate.go`:

```go
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// migrationLockID는 여러 인스턴스가 동시에 마이그레이션하지 않도록 잡는 advisory lock 키다.
const migrationLockID = 727274

// Migrate는 아직 적용하지 않은 migrations/*.sql을 파일 이름 순으로 적용한다.
// 각 파일은 하나의 트랜잭션으로 적용되고 schema_migrations에 기록된다.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("migrate: acquire: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("migrate: lock: %w", err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    text PRIMARY KEY,
		applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return fmt.Errorf("migrate: create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return fmt.Errorf("migrate: read dir: %w", err)
	}
	for _, e := range entries {
		version := strings.TrimSuffix(e.Name(), ".sql")
		var applied bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version).Scan(&applied); err != nil {
			return fmt.Errorf("migrate: check %s: %w", version, err)
		}
		if applied {
			continue
		}
		sql, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("migrate: read %s: %w", e.Name(), err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("migrate: begin %s: %w", version, err)
		}
		// 인자가 없는 Exec는 simple protocol을 써서 파일 안의 여러 문장을 한 번에 실행한다.
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migrate: apply %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migrate: record %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("migrate: commit %s: %w", version, err)
		}
	}
	return nil
}
```

- [ ] **Step 7: 통과 확인**

Run: `make test`
Expected: PASS (`internal/db` 테스트 2개 통과, 건너뜀 없음 — `go test -v ./internal/db/`로 SKIP이 없는지 확인)

- [ ] **Step 8: `migrate` 명령 연결**

`internal/app/migrate.go`:

```go
// Package app은 하위 명령(migrate, serve, worker, healthcheck)의 실행 흐름을 담는다.
package app

import (
	"context"

	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
)

// Migrate는 DB 마이그레이션을 적용하고 끝난다.
func Migrate(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(config.RoleMigrate, getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return db.Migrate(ctx, pool)
}
```

`cmd/ssl-checker/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ssl-checker/internal/app"
)

const usage = `usage: ssl-checker <command> [flags]

commands:
  migrate      DB 마이그레이션을 적용하고 종료한다
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "migrate":
		err = app.Migrate(ctx, os.Getenv)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 9: 명령 동작 확인**

Run: `DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go run ./cmd/ssl-checker migrate && echo OK`
Expected: `OK` (두 번 실행해도 `OK`). 확인 후 테스트 DB의 public 스키마를 비운다: `docker exec sslchk-testdb psql -U postgres -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'`

- [ ] **Step 10: 커밋**

```bash
go mod tidy
git add go.mod go.sum internal/db internal/app cmd
git commit -m "feat: add postgres connection, embedded migrations and migrate command" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: DNSProvider 인터페이스, memory 구현체, 적합성 테스트

**Files:**
- Create: `internal/dnsprovider/provider.go`, `internal/dnsprovider/provider_test.go`
- Create: `internal/dnsprovider/memory/memory.go`, `internal/dnsprovider/memory/memory_test.go`
- Create: `internal/dnsprovider/providertest/providertest.go`

**Interfaces:**
- Consumes: `netutil.NormalizeName`
- Produces:
  - `dnsprovider.Provider` 인터페이스: `SetTXT(ctx, fqdn string, values []string, ttl int) error`, `DeleteTXT(ctx, fqdn string) error`, `ListTXT(ctx, zone string) ([]TXTRecord, error)`, `Check(ctx) error`, `Capabilities() Capabilities`
  - `dnsprovider.TXTRecord{FQDN, Value, ProviderRef string}` (FQDN은 소문자, 끝 점 없음), `dnsprovider.Capabilities{MinTTL, MaxRecordsPerZone int}`
  - `dnsprovider.ErrorKind` (`KindTransient`, `KindConfig`, `String()`은 `"transient"`/`"config"`), `dnsprovider.Error{Kind ErrorKind; Op string; Err error}`, `dnsprovider.KindOf(err error) ErrorKind`
  - `dnsprovider.Dedupe(values []string) []string` — 순서를 지키며 중복·빈 문자열 제거
  - `memory.New() *memory.Provider`, `(*memory.Provider).SetFailure(err error)`, `(*memory.Provider).Values(fqdn string) []string`(정렬된 복사본)
  - `providertest.Run(t *testing.T, p dnsprovider.Provider, zone string)`

- [ ] **Step 1: 인터페이스 테스트 작성**

`internal/dnsprovider/provider_test.go`:

```go
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
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/dnsprovider/`
Expected: FAIL (`undefined: dnsprovider.Error`)

- [ ] **Step 3: 인터페이스 구현**

`internal/dnsprovider/provider.go`:

```go
// Package dnsprovider는 챌린지 TXT를 기록하는 DNS 공급자 계약을 정의한다.
// 구현체는 기록만 하고, 값 2개 유지·검증·감사 로그 같은 정책은 앱 코어가 맡는다.
package dnsprovider

import (
	"context"
	"errors"
	"fmt"
)

type Provider interface {
	// SetTXT는 fqdn의 TXT 값 집합을 values와 같게 맞춘다 (최대 2개).
	// 같은 입력으로 여러 번 호출해도 결과가 같아야 한다. values가 비면 모두 지운다.
	SetTXT(ctx context.Context, fqdn string, values []string, ttl int) error

	// DeleteTXT는 fqdn의 TXT 레코드를 모두 지운다. 없으면 성공으로 본다.
	DeleteTXT(ctx context.Context, fqdn string) error

	// ListTXT는 존 안의 TXT 레코드 목록이다. DB와 대조해 고아 레코드를 정리할 때 쓴다.
	ListTXT(ctx context.Context, zone string) ([]TXTRecord, error)

	// Check는 자격증명과 존 접근이 정상인지 확인한다. /health에 반영한다.
	Check(ctx context.Context) error

	// Capabilities는 최소 TTL, 존당 레코드 수 제한 같은 공급자 특성이다.
	Capabilities() Capabilities
}

// TXTRecord의 FQDN은 소문자이고 끝에 점이 없다. ProviderRef는 공급자별 레코드 ID로, 코어는 의존하지 않는다.
type TXTRecord struct {
	FQDN        string
	Value       string
	ProviderRef string
}

type Capabilities struct {
	MinTTL            int
	MaxRecordsPerZone int // 0이면 알 수 없음
}

// ErrorKind는 재시도하면 될 일시 오류와 사람이 고쳐야 할 설정 오류를 나눈다.
type ErrorKind int

const (
	KindTransient ErrorKind = iota + 1 // 요청 제한, 시간 초과, 5xx
	KindConfig                         // 권한, 존 없음, 잘못된 요청
)

func (k ErrorKind) String() string {
	switch k {
	case KindTransient:
		return "transient"
	case KindConfig:
		return "config"
	}
	return "unknown"
}

type Error struct {
	Kind ErrorKind
	Op   string
	Err  error
}

func (e *Error) Error() string { return fmt.Sprintf("dns %s (%s): %v", e.Op, e.Kind, e.Err) }
func (e *Error) Unwrap() error { return e.Err }

// KindOf는 err 안의 *Error 종류를 돌려준다. 분류되지 않은 오류는 일시 오류로 본다.
func KindOf(err error) ErrorKind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return KindTransient
}

// Dedupe는 순서를 지키며 중복과 빈 문자열을 없앤다.
func Dedupe(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/dnsprovider/`
Expected: PASS

- [ ] **Step 5: 적합성 테스트와 memory 테스트 작성**

`internal/dnsprovider/providertest/providertest.go`:

```go
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
```

`internal/dnsprovider/memory/memory_test.go`:

```go
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
```

- [ ] **Step 6: 실패 확인**

Run: `go test ./internal/dnsprovider/...`
Expected: FAIL (`undefined: memory.New`)

- [ ] **Step 7: memory 구현**

`internal/dnsprovider/memory/memory.go`:

```go
// Package memory는 TXT를 메모리에만 두는 공급자다. 테스트와 로컬 실행용이며 실제 DNS에는 아무것도 기록하지 않는다.
package memory

import (
	"context"
	"slices"
	"sort"
	"strings"
	"sync"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
)

type Provider struct {
	mu      sync.Mutex
	records map[string][]string
	fail    error
}

func New() *Provider { return &Provider{records: map[string][]string{}} }

// SetFailure는 이후 모든 호출이 err를 감싼 일시 오류를 돌려주게 한다. nil이면 해제한다.
func (p *Provider) SetFailure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = err
}

// Values는 fqdn의 현재 값을 정렬해 돌려준다. 테스트용.
func (p *Provider) Values(fqdn string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := slices.Clone(p.records[netutil.NormalizeName(fqdn)])
	slices.Sort(out)
	return out
}

func (p *Provider) failure(op string) error {
	if p.fail == nil {
		return nil
	}
	return &dnsprovider.Error{Kind: dnsprovider.KindTransient, Op: op, Err: p.fail}
}

func (p *Provider) SetTXT(_ context.Context, fqdn string, values []string, _ int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("set"); err != nil {
		return err
	}
	fqdn = netutil.NormalizeName(fqdn)
	vals := dnsprovider.Dedupe(values)
	if len(vals) == 0 {
		delete(p.records, fqdn)
		return nil
	}
	p.records[fqdn] = vals
	return nil
}

func (p *Provider) DeleteTXT(_ context.Context, fqdn string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("delete"); err != nil {
		return err
	}
	delete(p.records, netutil.NormalizeName(fqdn))
	return nil
}

func (p *Provider) ListTXT(_ context.Context, zone string) ([]dnsprovider.TXTRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.failure("list"); err != nil {
		return nil, err
	}
	zone = netutil.NormalizeName(zone)
	var out []dnsprovider.TXTRecord
	for fqdn, vals := range p.records {
		if fqdn != zone && !strings.HasSuffix(fqdn, "."+zone) {
			continue
		}
		for _, v := range vals {
			out = append(out, dnsprovider.TXTRecord{FQDN: fqdn, Value: v})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FQDN != out[j].FQDN {
			return out[i].FQDN < out[j].FQDN
		}
		return out[i].Value < out[j].Value
	})
	return out, nil
}

func (p *Provider) Check(context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failure("check")
}

func (p *Provider) Capabilities() dnsprovider.Capabilities {
	return dnsprovider.Capabilities{MinTTL: 1}
}
```

- [ ] **Step 8: 통과 확인**

Run: `go test ./internal/dnsprovider/...`
Expected: PASS

- [ ] **Step 9: 커밋**

```bash
git add internal/dnsprovider
git commit -m "feat: add DNSProvider contract, memory provider and conformance suite" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: Cloudflare 구현체

**Files:**
- Create: `internal/dnsprovider/cloudflare/cloudflare.go`
- Create: `internal/dnsprovider/cloudflare/fake_test.go`, `internal/dnsprovider/cloudflare/cloudflare_test.go`

**Interfaces:**
- Consumes: `dnsprovider.Provider`, `dnsprovider.Error`, `dnsprovider.Dedupe`, `netutil.NormalizeName`, `providertest.Run`
- Produces:
  - `cloudflare.Config{Token, Zone, ZoneID, BaseURL string; HTTPClient *http.Client}` (BaseURL 기본값 `https://api.cloudflare.com/client/v4`)
  - `cloudflare.New(cfg Config) *cloudflare.Provider` (dnsprovider.Provider 구현)

Cloudflare API 사용 범위: `GET /zones?name=`, `GET /zones/{zone}/dns_records?type=TXT&name=&page=&per_page=`, `POST /zones/{zone}/dns_records`, `PUT /zones/{zone}/dns_records/{id}`, `DELETE /zones/{zone}/dns_records/{id}`. 응답은 `{"success", "errors":[{"code","message"}], "result", "result_info":{"page","total_pages"}}`. 429와 5xx, 네트워크 오류는 일시 오류, 나머지 4xx는 설정 오류다.

- [ ] **Step 1: 가짜 Cloudflare 서버 작성**

`internal/dnsprovider/cloudflare/fake_test.go`:

```go
package cloudflare

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strconv"
	"sync"
	"testing"
)

// fakeCF는 테스트에 필요한 Cloudflare DNS API 일부를 흉내 낸다.
type fakeCF struct {
	token      string
	zoneID     string
	zoneName   string
	quote      bool // content를 따옴표로 감싸 저장·반환한다
	maxPerPage int

	mu         sync.Mutex
	nextID     int
	records    map[string]record
	failStatus int // 0이 아니면 모든 요청에 이 상태 코드로 실패한다
	mutations  int
	srv        *httptest.Server
}

func newFakeCF(t *testing.T) *fakeCF {
	f := &fakeCF{token: "tok", zoneID: "zone123", zoneName: "acme-chk.test", maxPerPage: 100, records: map[string]record{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /zones", f.listZones)
	mux.HandleFunc("GET /zones/{zid}/dns_records", f.listRecords)
	mux.HandleFunc("POST /zones/{zid}/dns_records", f.createRecord)
	mux.HandleFunc("PUT /zones/{zid}/dns_records/{rid}", f.updateRecord)
	mux.HandleFunc("DELETE /zones/{zid}/dns_records/{rid}", f.deleteRecord)
	f.srv = httptest.NewServer(f.guard(mux))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCF) provider() *Provider {
	return New(Config{Token: f.token, Zone: f.zoneName, BaseURL: f.srv.URL})
}

func (f *fakeCF) values(fqdn string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, r := range f.records {
		if r.Name == fqdn {
			out = append(out, unquote(r.Content))
		}
	}
	slices.Sort(out)
	return out
}

func writeCF(w http.ResponseWriter, status int, success bool, result any, info any, errs ...apiError) {
	if errs == nil {
		errs = []apiError{}
	}
	body := map[string]any{"success": success, "errors": errs, "messages": []any{}, "result": result}
	if info != nil {
		body["result_info"] = info
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeCF) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		status := f.failStatus
		f.mu.Unlock()
		if status != 0 {
			writeCF(w, status, false, nil, nil, apiError{Code: 9999, Message: "injected failure"})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			writeCF(w, http.StatusForbidden, false, nil, nil, apiError{Code: 10000, Message: "Authentication error"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (f *fakeCF) zoneOK(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("zid") != f.zoneID {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 7003, Message: "Could not route to zone"})
		return false
	}
	return true
}

func (f *fakeCF) listZones(w http.ResponseWriter, r *http.Request) {
	res := []map[string]string{}
	if r.URL.Query().Get("name") == f.zoneName {
		res = append(res, map[string]string{"id": f.zoneID, "name": f.zoneName})
	}
	writeCF(w, http.StatusOK, true, res, nil)
}

func (f *fakeCF) listRecords(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	q := r.URL.Query()
	f.mu.Lock()
	defer f.mu.Unlock()
	matched := []record{}
	for _, rec := range f.records {
		if t := q.Get("type"); t != "" && rec.Type != t {
			continue
		}
		if n := q.Get("name"); n != "" && rec.Name != n {
			continue
		}
		matched = append(matched, rec)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	per, _ := strconv.Atoi(q.Get("per_page"))
	if per <= 0 || per > f.maxPerPage {
		per = f.maxPerPage
	}
	page, _ := strconv.Atoi(q.Get("page"))
	if page <= 0 {
		page = 1
	}
	total := (len(matched) + per - 1) / per
	start := min((page-1)*per, len(matched))
	end := min(start+per, len(matched))
	writeCF(w, http.StatusOK, true, matched[start:end], map[string]int{"page": page, "per_page": per, "total_pages": total})
}

func (f *fakeCF) decode(w http.ResponseWriter, r *http.Request) (record, bool) {
	var in record
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Type != "TXT" || in.Name == "" {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 1004, Message: "DNS Validation Error"})
		return record{}, false
	}
	if in.TTL != 1 && in.TTL < 60 {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 1004, Message: "TTL must be at least 60"})
		return record{}, false
	}
	if f.quote {
		in.Content = `"` + in.Content + `"`
	}
	return in, true
}

// duplicate는 같은 이름·내용의 레코드가 이미 있는지 본다. Cloudflare는 이를 81058로 거부한다.
func (f *fakeCF) duplicate(in record, exceptID string) bool {
	for id, rec := range f.records {
		if id != exceptID && rec.Name == in.Name && rec.Content == in.Content {
			return true
		}
	}
	return false
}

func (f *fakeCF) createRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	in, ok := f.decode(w, r)
	if !ok {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.duplicate(in, "") {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 81058, Message: "An identical record already exists."})
		return
	}
	f.nextID++
	in.ID = fmt.Sprintf("rec%04d", f.nextID)
	f.records[in.ID] = in
	f.mutations++
	writeCF(w, http.StatusOK, true, in, nil)
}

func (f *fakeCF) updateRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	in, ok := f.decode(w, r)
	if !ok {
		return
	}
	rid := r.PathValue("rid")
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.records[rid]; !exists {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 81044, Message: "Record does not exist."})
		return
	}
	if f.duplicate(in, rid) {
		writeCF(w, http.StatusBadRequest, false, nil, nil, apiError{Code: 81058, Message: "An identical record already exists."})
		return
	}
	in.ID = rid
	f.records[rid] = in
	f.mutations++
	writeCF(w, http.StatusOK, true, in, nil)
}

func (f *fakeCF) deleteRecord(w http.ResponseWriter, r *http.Request) {
	if !f.zoneOK(w, r) {
		return
	}
	rid := r.PathValue("rid")
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.records[rid]; !exists {
		writeCF(w, http.StatusNotFound, false, nil, nil, apiError{Code: 81044, Message: "Record does not exist."})
		return
	}
	delete(f.records, rid)
	f.mutations++
	writeCF(w, http.StatusOK, true, map[string]string{"id": rid}, nil)
}
```

- [ ] **Step 2: 실패 테스트 작성**

`internal/dnsprovider/cloudflare/cloudflare_test.go`:

```go
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

// TestRealCloudflare는 실제 Cloudflare 존에 적합성 테스트를 돌린다.
// CF_TEST_TOKEN, CF_TEST_ZONE을 지정했을 때만 실행한다. 운영 존이 아닌 시험용 존을 쓴다.
func TestRealCloudflare(t *testing.T) {
	token, zone := os.Getenv("CF_TEST_TOKEN"), os.Getenv("CF_TEST_ZONE")
	if token == "" || zone == "" {
		t.Skip("CF_TEST_TOKEN/CF_TEST_ZONE not set")
	}
	providertest.Run(t, New(Config{Token: token, Zone: zone, ZoneID: os.Getenv("CF_TEST_ZONE_ID")}), zone)
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/dnsprovider/cloudflare/`
Expected: FAIL (`undefined: record`, `undefined: New` 등)

- [ ] **Step 4: Cloudflare 구현**

`internal/dnsprovider/cloudflare/cloudflare.go`:

```go
// Package cloudflare는 Cloudflare DNS API로 챌린지 TXT를 기록하는 공급자 구현체다.
// Cloudflare는 레코드 단위 API라서, SetTXT는 현재 레코드와 원하는 값 집합을 비교해 생성·교체·삭제한다.
package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
)

const (
	DefaultBaseURL = "https://api.cloudflare.com/client/v4"
	minTTL         = 60
	perPage        = 100
	maxBodyBytes   = 4 << 20
)

type Config struct {
	Token      string
	Zone       string // 챌린지 전용 존 이름 (예: acme-chk.net)
	ZoneID     string // 비어 있으면 Zone 이름으로 조회한다
	BaseURL    string
	HTTPClient *http.Client
}

type Provider struct {
	cfg Config
	hc  *http.Client

	mu     sync.Mutex
	zoneID string
}

func New(cfg Config) *Provider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	cfg.Zone = netutil.NormalizeName(cfg.Zone)
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Provider{cfg: cfg, hc: hc, zoneID: cfg.ZoneID}
}

type record struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Success    bool            `json:"success"`
	Errors     []apiError      `json:"errors"`
	Result     json.RawMessage `json:"result"`
	ResultInfo *struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

// httpError는 Cloudflare가 실패로 응답한 경우다. 토큰은 담지 않는다.
type httpError struct {
	Status int
	Errors []apiError
}

func (e *httpError) Error() string {
	msgs := make([]string, 0, len(e.Errors))
	for _, a := range e.Errors {
		msgs = append(msgs, fmt.Sprintf("%d %s", a.Code, a.Message))
	}
	return fmt.Sprintf("cloudflare: HTTP %d: %s", e.Status, strings.Join(msgs, "; "))
}

func (p *Provider) Capabilities() dnsprovider.Capabilities {
	return dnsprovider.Capabilities{MinTTL: minTTL}
}

func (p *Provider) Check(ctx context.Context) error {
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	q := url.Values{"type": {"TXT"}, "per_page": {"5"}}
	_, err = p.do(ctx, "check", http.MethodGet, "/zones/"+zid+"/dns_records", q, nil, nil)
	return err
}

func (p *Provider) SetTXT(ctx context.Context, fqdn string, values []string, ttl int) error {
	fqdn = netutil.NormalizeName(fqdn)
	ttl = max(ttl, minTTL)
	desired := dnsprovider.Dedupe(values)
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	current, err := p.list(ctx, zid, fqdn)
	if err != nil {
		return err
	}
	kept := map[string]bool{}
	var spare []record
	for _, r := range current {
		v := unquote(r.Content)
		if slices.Contains(desired, v) && !kept[v] {
			kept[v] = true
			continue
		}
		spare = append(spare, r)
	}
	for _, v := range desired {
		if kept[v] {
			continue
		}
		body := record{Type: "TXT", Name: fqdn, Content: v, TTL: ttl}
		if len(spare) > 0 {
			r := spare[0]
			spare = spare[1:]
			if _, err := p.do(ctx, "set", http.MethodPut, "/zones/"+zid+"/dns_records/"+r.ID, nil, body, nil); err != nil {
				return err
			}
			continue
		}
		if _, err := p.do(ctx, "set", http.MethodPost, "/zones/"+zid+"/dns_records", nil, body, nil); err != nil {
			return err
		}
	}
	for _, r := range spare {
		if err := p.deleteRecord(ctx, zid, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) DeleteTXT(ctx context.Context, fqdn string) error {
	zid, err := p.zone(ctx)
	if err != nil {
		return err
	}
	current, err := p.list(ctx, zid, netutil.NormalizeName(fqdn))
	if err != nil {
		return err
	}
	for _, r := range current {
		if err := p.deleteRecord(ctx, zid, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) ListTXT(ctx context.Context, zone string) ([]dnsprovider.TXTRecord, error) {
	zone = netutil.NormalizeName(zone)
	zid, err := p.zone(ctx)
	if err != nil {
		return nil, err
	}
	recs, err := p.list(ctx, zid, "")
	if err != nil {
		return nil, err
	}
	out := make([]dnsprovider.TXTRecord, 0, len(recs))
	for _, r := range recs {
		name := netutil.NormalizeName(r.Name)
		if name != zone && !strings.HasSuffix(name, "."+zone) {
			continue
		}
		out = append(out, dnsprovider.TXTRecord{FQDN: name, Value: unquote(r.Content), ProviderRef: r.ID})
	}
	return out, nil
}

func (p *Provider) deleteRecord(ctx context.Context, zid, id string) error {
	_, err := p.do(ctx, "delete", http.MethodDelete, "/zones/"+zid+"/dns_records/"+id, nil, nil, nil)
	var he *httpError
	if errors.As(err, &he) && he.Status == http.StatusNotFound {
		return nil // 이미 지워졌다
	}
	return err
}

// zone은 존 ID를 돌려준다. 설정에 없으면 이름으로 한 번 조회해 기억한다.
func (p *Provider) zone(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.zoneID != "" {
		return p.zoneID, nil
	}
	var zones []struct {
		ID string `json:"id"`
	}
	if _, err := p.do(ctx, "zone", http.MethodGet, "/zones", url.Values{"name": {p.cfg.Zone}}, nil, &zones); err != nil {
		return "", err
	}
	if len(zones) == 0 {
		return "", &dnsprovider.Error{Kind: dnsprovider.KindConfig, Op: "zone",
			Err: fmt.Errorf("cloudflare: zone %q not found or token cannot read it (set CF_ZONE_ID)", p.cfg.Zone)}
	}
	p.zoneID = zones[0].ID
	return p.zoneID, nil
}

// list는 TXT 레코드를 모든 페이지에 걸쳐 읽는다. name이 비면 존 전체다.
func (p *Provider) list(ctx context.Context, zid, name string) ([]record, error) {
	var all []record
	for page := 1; ; page++ {
		q := url.Values{"type": {"TXT"}, "per_page": {strconv.Itoa(perPage)}, "page": {strconv.Itoa(page)}}
		if name != "" {
			q.Set("name", name)
		}
		var recs []record
		env, err := p.do(ctx, "list", http.MethodGet, "/zones/"+zid+"/dns_records", q, nil, &recs)
		if err != nil {
			return nil, err
		}
		all = append(all, recs...)
		if env.ResultInfo == nil || page >= env.ResultInfo.TotalPages {
			return all, nil
		}
	}
}

func (p *Provider) do(ctx context.Context, op, method, path string, q url.Values, body, out any) (*envelope, error) {
	fail := func(kind dnsprovider.ErrorKind, err error) (*envelope, error) {
		return nil, &dnsprovider.Error{Kind: kind, Op: op, Err: err}
	}
	u := p.cfg.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fail(dnsprovider.KindConfig, err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return fail(dnsprovider.KindConfig, err)
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.hc.Do(req)
	if err != nil {
		return fail(dnsprovider.KindTransient, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fail(dnsprovider.KindTransient, err)
	}
	var env envelope
	decErr := json.Unmarshal(raw, &env)
	if resp.StatusCode >= 300 || decErr != nil || !env.Success {
		kind := dnsprovider.KindConfig
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 || resp.StatusCode < 300 {
			kind = dnsprovider.KindTransient
		}
		return fail(kind, &httpError{Status: resp.StatusCode, Errors: env.Errors})
	}
	if out != nil {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fail(dnsprovider.KindTransient, fmt.Errorf("decode result: %w", err))
		}
	}
	return &env, nil
}

// unquote는 Cloudflare가 TXT content를 따옴표로 감싸 돌려줄 때 원래 값으로 되돌린다.
func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/dnsprovider/...`
Expected: PASS (`TestRealCloudflare`는 SKIP)

- [ ] **Step 6: 커밋**

```bash
git add internal/dnsprovider/cloudflare
git commit -m "feat: add Cloudflare DNS provider with desired-state TXT sync" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: 저장소 계층 (store)

**Files:**
- Create: `internal/store/store.go`, `internal/store/store_test.go`

**Interfaces:**
- Consumes: `dbtest.New`, `netutil.ParsePrefix`
- Produces:
  - `store.New(pool *pgxpool.Pool) *store.Store`
  - `store.ErrNotFound`, `store.ErrActiveAccountExists`
  - `store.Account{ID, DomainID int64; Domain, Username, KeyHash, Subdomain string; AllowFrom []netip.Prefix; CreatedAt time.Time}` (Username, Subdomain은 소문자 UUID 문자열)
  - `store.NewAccount{Domain string; Username uuid.UUID; KeyHash string; Subdomain uuid.UUID; AllowFrom []netip.Prefix}`
  - `(*Store).CreateAccount(ctx, NewAccount) (Account, error)` — 도메인 행이 없으면 만든다. 활성 계정이 있으면 `ErrActiveAccountExists`.
  - `(*Store).ActiveAccountByUsername(ctx, username string) (Account, error)` — UUID가 아니거나 없거나 폐기됐으면 `ErrNotFound`.
  - `(*Store).UpdateTXT(ctx, accountID int64, fn func(current []string) ([]string, error)) error` — 계정 행을 잠근 채 현재 값(오래된 순)을 fn에 넘기고, fn이 돌려준 집합을 저장. fn 오류면 아무것도 바꾸지 않는다. 계정이 없거나 폐기됐으면 `ErrNotFound`.
  - `store.ActiveTXT{AccountID int64; Values []string}`, `(*Store).ActiveTXTBySubdomain(ctx) (map[string]ActiveTXT, error)` — 값이 없는 활성 계정도 빈 슬라이스로 포함
  - `store.AuditEntry{Actor, Action, Target, SourceIP, Detail string}`, `(*Store).Audit(ctx, AuditEntry) error`

- [ ] **Step 1: 의존성 추가**

```bash
go get github.com/google/uuid@latest
```

- [ ] **Step 2: 실패 테스트 작성**

`internal/store/store_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/store"
)

func newStore(t *testing.T) (*store.Store, *pgxpool.Pool) {
	pool := dbtest.New(t)
	return store.New(pool), pool
}

func create(t *testing.T, st *store.Store, domain string, allow ...netip.Prefix) store.Account {
	t.Helper()
	a, err := st.CreateAccount(context.Background(), store.NewAccount{
		Domain: domain, Username: uuid.New(), KeyHash: "hash", Subdomain: uuid.New(), AllowFrom: allow,
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	return a
}

func TestCreateAndLoadAccount(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	allow := []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16"), netip.MustParsePrefix("2001:db8::/32")}
	a := create(t, st, "a.com", allow...)

	got, err := st.ActiveAccountByUsername(ctx, a.Username)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != a.ID || got.Domain != "a.com" || got.Subdomain != a.Subdomain || got.KeyHash != "hash" {
		t.Fatalf("got %+v, want %+v", got, a)
	}
	if !slices.Equal(got.AllowFrom, allow) {
		t.Fatalf("allowfrom = %v", got.AllowFrom)
	}
}

func TestCreateAccountWithoutAllowFrom(t *testing.T) {
	st, _ := newStore(t)
	a := create(t, st, "a.com")
	got, err := st.ActiveAccountByUsername(context.Background(), a.Username)
	if err != nil || len(got.AllowFrom) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestOneActiveAccountPerDomain(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	create(t, st, "a.com")
	_, err := st.CreateAccount(ctx, store.NewAccount{Domain: "a.com", Username: uuid.New(), KeyHash: "h", Subdomain: uuid.New()})
	if !errors.Is(err, store.ErrActiveAccountExists) {
		t.Fatalf("err = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	create(t, st, "a.com")
}

func TestActiveAccountByUsernameNotFound(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	revoked := create(t, st, "a.com")
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"not-a-uuid", "", uuid.NewString(), revoked.Username} {
		if _, err := st.ActiveAccountByUsername(ctx, name); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("ActiveAccountByUsername(%q) err = %v", name, err)
		}
	}
}

func TestUpdateTXT(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")

	set := func(want []string) {
		t.Helper()
		if err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return want, nil }); err != nil {
			t.Fatal(err)
		}
	}
	current := func() []string {
		t.Helper()
		var got []string
		_ = st.UpdateTXT(ctx, a.ID, func(cur []string) ([]string, error) { got = cur; return cur, nil })
		return got
	}

	set([]string{"v1"})
	set([]string{"v1", "v2"})
	if got := current(); !slices.Equal(got, []string{"v1", "v2"}) {
		t.Fatalf("after two: %v", got)
	}
	set([]string{"v2", "v3"})
	if got := current(); !slices.Equal(got, []string{"v2", "v3"}) {
		t.Fatalf("oldest-first order after replace: %v", got)
	}

	boom := errors.New("provider down")
	err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return nil, boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if got := current(); !slices.Equal(got, []string{"v2", "v3"}) {
		t.Fatalf("fn error changed values: %v", got)
	}

	set(nil)
	if got := current(); len(got) != 0 {
		t.Fatalf("nil set should clear: %v", got)
	}
}

func TestUpdateTXTRevoked(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	called := false
	err := st.UpdateTXT(ctx, a.ID, func(cur []string) ([]string, error) { called = true; return cur, nil })
	if !errors.Is(err, store.ErrNotFound) || called {
		t.Fatalf("err = %v, called = %v", err, called)
	}
}

func TestActiveTXTBySubdomain(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	withValues := create(t, st, "a.com")
	empty := create(t, st, "b.com")
	revoked := create(t, st, "c.com")
	if err := st.UpdateTXT(ctx, withValues.ID, func([]string) ([]string, error) { return []string{"x", "y"}, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.ActiveTXTBySubdomain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries: %v", len(got), got)
	}
	if e := got[withValues.Subdomain]; e.AccountID != withValues.ID || !slices.Equal(e.Values, []string{"x", "y"}) {
		t.Fatalf("withValues = %+v", e)
	}
	if e, ok := got[empty.Subdomain]; !ok || e.Values == nil || len(e.Values) != 0 {
		t.Fatalf("empty = %+v, %v", e, ok)
	}
}

func TestAudit(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	if err := st.Audit(ctx, store.AuditEntry{Actor: "admin", Action: "register", Target: "a.com", SourceIP: "10.0.0.1", Detail: "d"}); err != nil {
		t.Fatal(err)
	}
	var actor, action, target, ip, detail string
	err := pool.QueryRow(ctx, `SELECT actor, action, target, source_ip, detail FROM audit_logs`).Scan(&actor, &action, &target, &ip, &detail)
	if err != nil || actor != "admin" || action != "register" || target != "a.com" || ip != "10.0.0.1" || detail != "d" {
		t.Fatalf("row = %s %s %s %s %s, %v", actor, action, target, ip, detail, err)
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `make test`
Expected: FAIL (`undefined: store.New`)

- [ ] **Step 4: store 구현**

`internal/store/store.go`:

```go
// Package store는 PostgreSQL에 도메인, 검증 계정, TXT 값, 감사 로그를 저장한다.
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/netutil"
)

var (
	ErrNotFound            = errors.New("store: not found")
	ErrActiveAccountExists = errors.New("store: domain already has an active account")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type Account struct {
	ID        int64
	DomainID  int64
	Domain    string
	Username  string
	KeyHash   string
	Subdomain string
	AllowFrom []netip.Prefix
	CreatedAt time.Time
}

type NewAccount struct {
	Domain    string
	Username  uuid.UUID
	KeyHash   string
	Subdomain uuid.UUID
	AllowFrom []netip.Prefix
}

// CreateAccount는 도메인 행을 (없으면) 만들고 그 도메인의 활성 계정을 추가한다.
func (s *Store) CreateAccount(ctx context.Context, in NewAccount) (Account, error) {
	a := Account{
		Domain:    in.Domain,
		Username:  in.Username.String(),
		KeyHash:   in.KeyHash,
		Subdomain: in.Subdomain.String(),
		AllowFrom: in.AllowFrom,
	}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ($1)
			ON CONFLICT (fqdn) DO UPDATE SET fqdn = EXCLUDED.fqdn RETURNING id`, in.Domain).Scan(&a.DomainID); err != nil {
			return fmt.Errorf("store: upsert domain: %w", err)
		}
		err := tx.QueryRow(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain, allowfrom)
			VALUES ($1, $2, $3, $4, $5) RETURNING id, created_at`,
			a.DomainID, a.Username, a.KeyHash, a.Subdomain, prefixStrings(in.AllowFrom)).Scan(&a.ID, &a.CreatedAt)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "acme_accounts_one_active" {
			return ErrActiveAccountExists
		}
		if err != nil {
			return fmt.Errorf("store: insert account: %w", err)
		}
		return nil
	})
	if err != nil {
		return Account{}, err
	}
	return a, nil
}

// ActiveAccountByUsername은 폐기되지 않은 계정을 찾는다. username이 UUID 형식이 아니어도 ErrNotFound다.
func (s *Store) ActiveAccountByUsername(ctx context.Context, username string) (Account, error) {
	id, err := uuid.Parse(username)
	if err != nil {
		return Account{}, ErrNotFound
	}
	var a Account
	var allow []string
	err = s.pool.QueryRow(ctx, `SELECT a.id, a.domain_id, d.fqdn, a.username::text, a.key_hash, a.subdomain::text, a.allowfrom, a.created_at
		FROM acme_accounts a JOIN domains d ON d.id = a.domain_id
		WHERE a.username = $1 AND a.revoked_at IS NULL`, id.String()).
		Scan(&a.ID, &a.DomainID, &a.Domain, &a.Username, &a.KeyHash, &a.Subdomain, &allow, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: load account: %w", err)
	}
	if a.AllowFrom, err = parsePrefixes(allow); err != nil {
		return Account{}, fmt.Errorf("store: account %d allowfrom: %w", a.ID, err)
	}
	return a, nil
}

// UpdateTXT는 계정 행을 잠근 채 현재 값(오래된 순)을 fn에 넘기고, fn이 돌려준 값 집합을 저장한다.
// 같은 계정의 갱신은 이 잠금으로 직렬화된다. fn이 오류를 돌려주면 아무것도 바꾸지 않는다.
func (s *Store) UpdateTXT(ctx context.Context, accountID int64, fn func(current []string) ([]string, error)) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM acme_accounts WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, accountID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: lock account: %w", err)
		}
		rows, err := tx.Query(ctx, `SELECT value FROM txt_records WHERE account_id = $1 ORDER BY created_at, id`, accountID)
		if err != nil {
			return fmt.Errorf("store: load txt: %w", err)
		}
		current, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return fmt.Errorf("store: load txt: %w", err)
		}
		next, err := fn(current)
		if err != nil {
			return err
		}
		if next == nil {
			next = []string{}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM txt_records WHERE account_id = $1 AND NOT (value = ANY($2))`, accountID, next); err != nil {
			return fmt.Errorf("store: prune txt: %w", err)
		}
		for _, v := range next {
			if slices.Contains(current, v) {
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO txt_records (account_id, value) VALUES ($1, $2)`, accountID, v); err != nil {
				return fmt.Errorf("store: insert txt: %w", err)
			}
		}
		return nil
	})
}

type ActiveTXT struct {
	AccountID int64
	Values    []string
}

// ActiveTXTBySubdomain은 활성 계정의 서브도메인별 저장 값이다. 값이 없는 계정은 빈 슬라이스로 들어간다.
func (s *Store) ActiveTXTBySubdomain(ctx context.Context) (map[string]ActiveTXT, error) {
	rows, err := s.pool.Query(ctx, `SELECT a.id, a.subdomain::text, t.value
		FROM acme_accounts a LEFT JOIN txt_records t ON t.account_id = a.id
		WHERE a.revoked_at IS NULL
		ORDER BY a.id, t.created_at, t.id`)
	if err != nil {
		return nil, fmt.Errorf("store: active txt: %w", err)
	}
	defer rows.Close()
	out := map[string]ActiveTXT{}
	for rows.Next() {
		var id int64
		var sub string
		var val *string
		if err := rows.Scan(&id, &sub, &val); err != nil {
			return nil, fmt.Errorf("store: active txt: %w", err)
		}
		e := out[sub]
		e.AccountID = id
		if e.Values == nil {
			e.Values = []string{}
		}
		if val != nil {
			e.Values = append(e.Values, *val)
		}
		out[sub] = e
	}
	return out, rows.Err()
}

type AuditEntry struct {
	Actor    string
	Action   string
	Target   string
	SourceIP string
	Detail   string
}

func (s *Store) Audit(ctx context.Context, e AuditEntry) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_logs (actor, action, target, source_ip, detail) VALUES ($1, $2, $3, $4, $5)`,
		e.Actor, e.Action, e.Target, e.SourceIP, e.Detail)
	if err != nil {
		return fmt.Errorf("store: audit: %w", err)
	}
	return nil
}

func prefixStrings(ps []netip.Prefix) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}

func parsePrefixes(ss []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netutil.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
```

- [ ] **Step 5: 통과 확인**

Run: `make test`
Expected: PASS

- [ ] **Step 6: 커밋**

```bash
go mod tidy
git add go.mod go.sum internal/store
git commit -m "feat: add store for accounts, txt values and audit log" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: acme-dns 핵심 로직 (검증, 등록, 인증, TXT 갱신)

**Files:**
- Create: `internal/acmedns/validate.go`, `internal/acmedns/validate_test.go`
- Create: `internal/acmedns/service.go`, `internal/acmedns/service_test.go`

**Interfaces:**
- Consumes: `store.Store` 전체, `dnsprovider.Provider`, `memory.New`, `dbtest.New`, `netutil.NormalizeName`
- Produces:
  - `acmedns.ValidTXT(s string) bool`, `acmedns.ValidSubdomain(s string) bool`, `acmedns.ValidDomain(s string) bool`
  - `acmedns.NextValues(current []string, value string) []string`
  - `acmedns.Service{Store *store.Store; Provider dnsprovider.Provider; Zone string; TTL int}`
  - `acmedns.ErrForbidden`, `acmedns.ErrBadTXT`, `acmedns.ErrInvalidDomain`
  - `acmedns.Registration{Username, Password, Subdomain, FullDomain string; AllowFrom []netip.Prefix}`
  - `(*Service).Register(ctx, domain string, allowFrom []netip.Prefix) (Registration, error)` — 도메인 형식 오류는 `ErrInvalidDomain`, 중복은 `store.ErrActiveAccountExists`
  - `(*Service).Authenticate(ctx, username, key string) (store.Account, error)` — 실패는 모두 `ErrForbidden`
  - `(*Service).UpdateTXT(ctx, acct store.Account, value string) error` — 형식 오류는 `ErrBadTXT`, 공급자 오류는 `*dnsprovider.Error`를 감싼 채 반환
  - `(*Service).FQDN(subdomain string) string`

- [ ] **Step 1: 의존성 추가**

```bash
go get golang.org/x/crypto@latest
```

- [ ] **Step 2: 검증 함수 실패 테스트 작성**

`internal/acmedns/validate_test.go`:

```go
package acmedns_test

import (
	"slices"
	"strings"
	"testing"

	"ssl-checker/internal/acmedns"
)

func TestValidTXT(t *testing.T) {
	ok := []string{strings.Repeat("A", 43), "abcdefghijklmnopqrstuvwxyzABCDEFGHIJK_-0123"}
	bad := []string{"", strings.Repeat("A", 42), strings.Repeat("A", 44), strings.Repeat("+", 43), strings.Repeat("=", 43), strings.Repeat("가", 43)}
	for _, s := range ok {
		if !acmedns.ValidTXT(s) {
			t.Errorf("ValidTXT(%q) = false", s)
		}
	}
	for _, s := range bad {
		if acmedns.ValidTXT(s) {
			t.Errorf("ValidTXT(%q) = true", s)
		}
	}
}

func TestValidSubdomain(t *testing.T) {
	if !acmedns.ValidSubdomain("8e5700ea-a4bf-41c7-8a77-e990661dcc6a") {
		t.Error("valid uuid rejected")
	}
	for _, s := range []string{"", "www", "8E5700EA-A4BF-41C7-8A77-E990661DCC6A", "8e5700ea-a4bf-41c7-8a77-e990661dcc6", "8e5700ea.a4bf.41c7.8a77.e990661dcc6a"} {
		if acmedns.ValidSubdomain(s) {
			t.Errorf("ValidSubdomain(%q) = true", s)
		}
	}
}

func TestValidDomain(t *testing.T) {
	for _, s := range []string{"a.com", "sub.a.co.kr", "xn--3e0b707e.kr", "a-b.example"} {
		if !acmedns.ValidDomain(s) {
			t.Errorf("ValidDomain(%q) = false", s)
		}
	}
	for _, s := range []string{"", "com", "*.a.com", "-a.com", "a-.com", "a..com", "a.com.", "A.com", "a_b.com", strings.Repeat("a", 64) + ".com"} {
		if acmedns.ValidDomain(s) {
			t.Errorf("ValidDomain(%q) = true", s)
		}
	}
}

func TestNextValues(t *testing.T) {
	cases := []struct {
		current []string
		value   string
		want    []string
	}{
		{nil, "a", []string{"a"}},
		{[]string{"a"}, "b", []string{"a", "b"}},
		{[]string{"a", "b"}, "c", []string{"b", "c"}},
		{[]string{"a", "b"}, "a", []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := acmedns.NextValues(c.current, c.value); !slices.Equal(got, c.want) {
			t.Errorf("NextValues(%v, %q) = %v, want %v", c.current, c.value, got, c.want)
		}
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/acmedns/`
Expected: FAIL (`undefined: acmedns.ValidTXT`)

- [ ] **Step 4: 검증 함수 구현**

`internal/acmedns/validate.go`:

```go
// Package acmedns는 acme.sh의 dns_acmedns 플러그인이 호출하는 acme-dns 호환 API를 구현한다.
package acmedns

import (
	"regexp"
	"slices"
	"strings"
)

const (
	txtLen    = 43 // SHA-256을 base64url로 인코딩한 길이
	maxValues = 2  // a.com과 *.a.com을 함께 발급하면 값 2개가 동시에 필요하다
)

var (
	subdomainRE = regexp.MustCompile(`^[a-z0-9]{8}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{4}-[a-z0-9]{12}$`)
	labelRE     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ValidTXT는 ACME DNS-01 검증 값 형식(43자, [A-Za-z0-9_-])인지 본다.
func ValidTXT(s string) bool {
	if len(s) != txtLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// ValidSubdomain은 앱이 발급한 서브도메인(소문자 UUID) 형식인지 본다.
func ValidSubdomain(s string) bool { return subdomainRE.MatchString(s) }

// ValidDomain은 등록할 도메인 이름(예: a.com)을 검사한다. 소문자로 정규화된 이름만 받고 와일드카드는 받지 않는다.
func ValidDomain(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

// NextValues는 최근 값 2개 유지 규칙을 적용한다. current는 오래된 순이다.
// 이미 있는 값이면 그대로, 3번째 값이면 가장 오래된 값을 밀어낸다.
func NextValues(current []string, value string) []string {
	if slices.Contains(current, value) {
		return slices.Clone(current)
	}
	next := append(slices.Clone(current), value)
	if len(next) > maxValues {
		next = next[len(next)-maxValues:]
	}
	return next
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/acmedns/`
Expected: PASS

- [ ] **Step 6: Service 실패 테스트 작성**

`internal/acmedns/service_test.go`:

```go
package acmedns_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/store"
)

const testZone = "acme-chk.test"

func txt(c byte) string { return strings.Repeat(string(c), 43) }

func newService(t *testing.T) (*acmedns.Service, *memory.Provider, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.New(t)
	mem := memory.New()
	return &acmedns.Service{Store: store.New(pool), Provider: mem, Zone: testZone, TTL: 60}, mem, pool
}

func dbValues(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT value FROM txt_records ORDER BY value`)
	if err != nil {
		t.Fatal(err)
	}
	vals, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return vals
}

func registerAndAuth(t *testing.T, svc *acmedns.Service, domain string) (acmedns.Registration, store.Account) {
	t.Helper()
	ctx := context.Background()
	reg, err := svc.Register(ctx, domain, nil)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	acct, err := svc.Authenticate(ctx, reg.Username, reg.Password)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return reg, acct
}

func TestRegister(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	allow := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	reg, err := svc.Register(ctx, " A.com. ", allow)
	if err != nil {
		t.Fatal(err)
	}
	if !acmedns.ValidSubdomain(reg.Subdomain) || reg.FullDomain != reg.Subdomain+"."+testZone {
		t.Fatalf("registration = %+v", reg)
	}
	if len(reg.Password) != 40 || reg.Username == reg.Subdomain {
		t.Fatalf("credentials = %+v", reg)
	}
	if _, err := svc.Register(ctx, "a.com", nil); !errors.Is(err, store.ErrActiveAccountExists) {
		t.Fatalf("duplicate: err = %v", err)
	}
	for _, bad := range []string{"", "com", "*.a.com", "a..com"} {
		if _, err := svc.Register(ctx, bad, nil); !errors.Is(err, acmedns.ErrInvalidDomain) {
			t.Errorf("Register(%q) err = %v", bad, err)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if acct.Subdomain != reg.Subdomain || acct.Domain != "a.com" {
		t.Fatalf("account = %+v", acct)
	}
	cases := map[string][2]string{
		"wrong key":      {reg.Username, "wrong"},
		"empty key":      {reg.Username, ""},
		"long key":       {reg.Username, strings.Repeat("k", 100)},
		"unknown user":   {"11111111-1111-1111-1111-111111111111", reg.Password},
		"non-uuid user":  {"abc", reg.Password},
		"empty username": {"", ""},
	}
	for name, c := range cases {
		if _, err := svc.Authenticate(ctx, c[0], c[1]); !errors.Is(err, acmedns.ErrForbidden) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestUpdateTXTRotates(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	fqdn := svc.FQDN(acct.Subdomain)
	for _, v := range []string{txt('a'), txt('b'), txt('c')} {
		if err := svc.UpdateTXT(ctx, acct, v); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{txt('b'), txt('c')}
	if got := mem.Values(fqdn); !slices.Equal(got, want) {
		t.Fatalf("provider = %v", got)
	}
	if got := dbValues(t, pool); !slices.Equal(got, want) {
		t.Fatalf("db = %v", got)
	}
}

func TestUpdateTXTRejectsBadValue(t *testing.T) {
	svc, mem, _ := newService(t)
	_, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(context.Background(), acct, "short"); !errors.Is(err, acmedns.ErrBadTXT) {
		t.Fatalf("err = %v", err)
	}
	if got := mem.Values(svc.FQDN(acct.Subdomain)); len(got) != 0 {
		t.Fatalf("provider written: %v", got)
	}
}

func TestUpdateTXTProviderFailureLeavesDBUnchanged(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	mem.SetFailure(errors.New("cloudflare down"))
	err := svc.UpdateTXT(ctx, acct, txt('b'))
	var perr *dnsprovider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("err = %v, want *dnsprovider.Error", err)
	}
	if got := dbValues(t, pool); !slices.Equal(got, []string{txt('a')}) {
		t.Fatalf("db changed on provider failure: %v", got)
	}
	mem.SetFailure(nil)
	if err := svc.UpdateTXT(ctx, acct, txt('b')); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := dbValues(t, pool); !slices.Equal(got, []string{txt('a'), txt('b')}) {
		t.Fatalf("db after retry: %v", got)
	}
}

func TestUpdateTXTConcurrent(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	_, acct := registerAndAuth(t, svc, "a.com")
	values := []string{txt('a'), txt('b'), txt('c'), txt('d'), txt('e')}
	var wg sync.WaitGroup
	errs := make(chan error, len(values))
	for _, v := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.UpdateTXT(ctx, acct, v)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	db := dbValues(t, pool)
	prov := mem.Values(svc.FQDN(acct.Subdomain))
	if len(db) != 2 || !slices.Equal(db, prov) {
		t.Fatalf("db = %v, provider = %v; want the same 2 values", db, prov)
	}
}
```

- [ ] **Step 7: 실패 확인**

Run: `make test`
Expected: FAIL (`undefined: acmedns.Service`)

- [ ] **Step 8: Service 구현**

`internal/acmedns/service.go`:

```go
package acmedns

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

var (
	ErrForbidden     = errors.New("acmedns: forbidden")
	ErrBadTXT        = errors.New("acmedns: bad txt")
	ErrInvalidDomain = errors.New("acmedns: invalid domain")
)

// dummyHash는 없는 계정으로 인증할 때도 bcrypt 비교를 해서, 계정 존재 여부가 응답 시간으로 드러나지 않게 한다.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("ssl-checker-dummy-password"), bcrypt.DefaultCost)

type Service struct {
	Store    *store.Store
	Provider dnsprovider.Provider
	Zone     string
	TTL      int
}

// Registration의 Password는 여기서 한 번만 평문으로 나온다. DB에는 bcrypt 해시만 남는다.
type Registration struct {
	Username   string
	Password   string
	Subdomain  string
	FullDomain string
	AllowFrom  []netip.Prefix
}

func (s *Service) FQDN(subdomain string) string { return subdomain + "." + s.Zone }

// Register는 domain의 검증 계정을 만든다. 사용자명과 서브도메인은 무작위 UUID다.
func (s *Service) Register(ctx context.Context, domain string, allowFrom []netip.Prefix) (Registration, error) {
	domain = netutil.NormalizeName(domain)
	if !ValidDomain(domain) {
		return Registration{}, ErrInvalidDomain
	}
	password, err := newPassword()
	if err != nil {
		return Registration{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Registration{}, fmt.Errorf("acmedns: hash key: %w", err)
	}
	acct, err := s.Store.CreateAccount(ctx, store.NewAccount{
		Domain:    domain,
		Username:  uuid.New(),
		KeyHash:   string(hash),
		Subdomain: uuid.New(),
		AllowFrom: allowFrom,
	})
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		Username:   acct.Username,
		Password:   password,
		Subdomain:  acct.Subdomain,
		FullDomain: s.FQDN(acct.Subdomain),
		AllowFrom:  allowFrom,
	}, nil
}

// Authenticate는 X-Api-User, X-Api-Key로 활성 계정을 찾는다. 인증 실패는 이유와 관계없이 ErrForbidden이다.
func (s *Service) Authenticate(ctx context.Context, username, key string) (store.Account, error) {
	acct, err := s.Store.ActiveAccountByUsername(ctx, username)
	if errors.Is(err, store.ErrNotFound) {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(key))
		return store.Account{}, ErrForbidden
	}
	if err != nil {
		return store.Account{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(acct.KeyHash), []byte(key)) != nil {
		return store.Account{}, ErrForbidden
	}
	return acct, nil
}

// UpdateTXT는 계정을 잠근 채 다음 값 집합을 계산하고, 공급자 반영이 성공했을 때만 DB에 저장한다.
func (s *Service) UpdateTXT(ctx context.Context, acct store.Account, value string) error {
	if !ValidTXT(value) {
		return ErrBadTXT
	}
	fqdn := s.FQDN(acct.Subdomain)
	return s.Store.UpdateTXT(ctx, acct.ID, func(current []string) ([]string, error) {
		next := NextValues(current, value)
		if err := s.Provider.SetTXT(ctx, fqdn, next, s.TTL); err != nil {
			return nil, err
		}
		return next, nil
	})
}

// newPassword는 40자 무작위 키를 만든다 (원본 acme-dns와 같은 길이).
func newPassword() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("acmedns: random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
```

- [ ] **Step 9: 통과 확인**

Run: `make test`
Expected: PASS

- [ ] **Step 10: 커밋**

```bash
go mod tidy
git add go.mod go.sum internal/acmedns
git commit -m "feat: add acme-dns service with keep-latest-two TXT policy" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: HTTP 공통 (클라이언트 IP, 키별 제한, 관리자 인증)

**Files:**
- Create: `internal/httpx/clientip.go`, `internal/httpx/limit.go`, `internal/httpx/adminauth.go`, `internal/httpx/json.go`
- Create: `internal/httpx/httpx_test.go`

**Interfaces:**
- Produces:
  - `httpx.ClientIP{Trusted []netip.Prefix}`, `(ClientIP).From(r *http.Request) netip.Addr`
  - `httpx.NewKeyed(every time.Duration, burst int) *httpx.Keyed`, `(*Keyed).Allow(key string) bool`, `(*Keyed).Exhausted(key string) bool`
  - `httpx.NewAdminAuth(key string, ip ClientIP) *httpx.AdminAuth`, `(*AdminAuth).Wrap(next http.Handler) http.Handler` — `Authorization: Bearer <key>`; 실패 401 `{"error":"unauthorized"}`; IP별 실패 5회 후 429 `{"error":"too_many_attempts"}`
  - `httpx.WriteJSON(w http.ResponseWriter, status int, v any)`, `httpx.WriteError(w http.ResponseWriter, status int, code string)`

- [ ] **Step 1: 의존성 추가**

```bash
go get golang.org/x/time@latest
```

- [ ] **Step 2: 실패 테스트 작성**

`internal/httpx/httpx_test.go`:

```go
package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/httpx"
)

func TestClientIP(t *testing.T) {
	ip := httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	cases := []struct {
		name, remote string
		xff          []string
		want         string
	}{
		{"direct, no header", "203.0.113.9:5000", nil, "203.0.113.9"},
		{"untrusted remote ignores xff", "203.0.113.9:5000", []string{"192.168.5.7"}, "203.0.113.9"},
		{"trusted proxy uses xff", "10.0.0.2:5000", []string{"192.168.5.7"}, "192.168.5.7"},
		{"rightmost untrusted wins", "10.0.0.2:5000", []string{"192.168.5.7, 203.0.113.50"}, "203.0.113.50"},
		{"skips trusted hops", "10.0.0.2:5000", []string{"192.168.5.7, 10.0.0.3"}, "192.168.5.7"},
		{"multiple header lines", "10.0.0.2:5000", []string{"192.168.5.7", "10.0.0.3"}, "192.168.5.7"},
		{"garbage hop stops at proxy", "10.0.0.2:5000", []string{"192.168.5.7, garbage"}, "10.0.0.2"},
		{"trusted proxy without header", "10.0.0.2:5000", nil, "10.0.0.2"},
		{"ipv4-mapped remote", "[::ffff:203.0.113.9]:5000", nil, "203.0.113.9"},
		{"ipv6 remote", "[2001:db8::1]:5000", nil, "2001:db8::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			r.RemoteAddr = c.remote
			for _, h := range c.xff {
				r.Header.Add("X-Forwarded-For", h)
			}
			if got := ip.From(r).String(); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestKeyed(t *testing.T) {
	k := httpx.NewKeyed(time.Hour, 2)
	if !k.Allow("a") || !k.Allow("a") {
		t.Fatal("burst not allowed")
	}
	if k.Allow("a") {
		t.Fatal("third call allowed")
	}
	if !k.Exhausted("a") || k.Exhausted("b") {
		t.Fatal("Exhausted wrong")
	}
	if !k.Allow("b") {
		t.Fatal("keys not independent")
	}
}

func TestAdminAuth(t *testing.T) {
	key := strings.Repeat("k", 32)
	auth := httpx.NewAdminAuth(key, httpx.ClientIP{})
	h := auth.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	call := func(remote, header string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/register", nil)
		r.RemoteAddr = remote
		if header != "" {
			r.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	if rec := call("192.0.2.1:1", "Bearer "+key); rec.Code != http.StatusNoContent {
		t.Fatalf("valid key: %d", rec.Code)
	}
	for _, header := range []string{"", "Bearer wrong", key, "Basic " + key, "Bearer " + key + "x"} {
		rec := call("192.0.2.1:1", header)
		if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"unauthorized"`) {
			t.Fatalf("header %q: %d %s", header, rec.Code, rec.Body)
		}
	}
	// 위에서 5번 실패했으므로 같은 IP는 올바른 키도 잠시 막힌다.
	if rec := call("192.0.2.1:1", "Bearer "+key); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("after 5 failures: %d", rec.Code)
	}
	if rec := call("192.0.2.2:1", "Bearer "+key); rec.Code != http.StatusNoContent {
		t.Fatalf("other IP blocked: %d", rec.Code)
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/httpx/`
Expected: FAIL (`undefined: httpx.ClientIP`)

- [ ] **Step 4: 구현**

`internal/httpx/json.go`:

```go
// Package httpx는 API 서버가 공통으로 쓰는 HTTP 도우미를 모은다.
package httpx

import (
	"encoding/json"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError는 acme-dns와 같은 {"error": "<code>"} 형태로 응답한다.
func WriteError(w http.ResponseWriter, status int, code string) {
	WriteJSON(w, status, map[string]string{"error": code})
}
```

`internal/httpx/clientip.go`:

```go
package httpx

import (
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP는 요청을 보낸 클라이언트 주소를 정한다.
// X-Forwarded-For는 직접 연결한 주소가 Trusted에 속할 때만 보고, 오른쪽부터 첫 비신뢰 주소를 쓴다.
type ClientIP struct {
	Trusted []netip.Prefix
}

func (c ClientIP) From(r *http.Request) netip.Addr {
	addr := parseRemote(r.RemoteAddr)
	if !addr.IsValid() || !c.trusted(addr) {
		return addr
	}
	var hops []string
	for _, h := range r.Header.Values("X-Forwarded-For") {
		hops = append(hops, strings.Split(h, ",")...)
	}
	for i := len(hops) - 1; i >= 0; i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return addr // 해석할 수 없는 값부터는 믿지 않는다
		}
		hop = hop.Unmap()
		if !c.trusted(hop) {
			return hop
		}
		addr = hop
	}
	return addr
}

func (c ClientIP) trusted(a netip.Addr) bool {
	for _, p := range c.Trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func parseRemote(s string) netip.Addr {
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}
```

`internal/httpx/limit.go`:

```go
package httpx

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// maxKeys를 넘으면 표를 비운다. 사내망 전용 API라 키 수가 적다는 전제의 단순한 상한이다.
const maxKeys = 10000

// Keyed는 키(IP, 계정)별 토큰 버킷이다.
type Keyed struct {
	mu    sync.Mutex
	every rate.Limit
	burst int
	m     map[string]*rate.Limiter
}

func NewKeyed(every time.Duration, burst int) *Keyed {
	return &Keyed{every: rate.Every(every), burst: burst, m: map[string]*rate.Limiter{}}
}

func (k *Keyed) get(key string) *rate.Limiter {
	k.mu.Lock()
	defer k.mu.Unlock()
	l, ok := k.m[key]
	if !ok {
		if len(k.m) >= maxKeys {
			k.m = map[string]*rate.Limiter{}
		}
		l = rate.NewLimiter(k.every, k.burst)
		k.m[key] = l
	}
	return l
}

// Allow는 토큰 하나를 쓴다. 남은 토큰이 없으면 false다.
func (k *Keyed) Allow(key string) bool { return k.get(key).Allow() }

// Exhausted는 토큰을 쓰지 않고 남은 토큰이 없는지만 본다.
func (k *Keyed) Exhausted(key string) bool { return k.get(key).Tokens() < 1 }
```

`internal/httpx/adminauth.go`:

```go
package httpx

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// AdminAuth는 Authorization: Bearer <ADMIN_KEY>를 확인한다.
// 해시끼리 상수 시간으로 비교해 길이도 드러내지 않고, IP별 실패 5회 후에는 12초에 1회만 다시 시도할 수 있다.
type AdminAuth struct {
	keyHash  [32]byte
	ip       ClientIP
	failures *Keyed
}

func NewAdminAuth(key string, ip ClientIP) *AdminAuth {
	return &AdminAuth{keyHash: sha256.Sum256([]byte(key)), ip: ip, failures: NewKeyed(12*time.Second, 5)}
}

func (a *AdminAuth) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := a.ip.From(r).String()
		if a.failures.Exhausted(ip) {
			WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		sum := sha256.Sum256([]byte(token))
		if !ok || subtle.ConstantTimeCompare(sum[:], a.keyHash[:]) != 1 {
			a.failures.Allow(ip)
			WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/httpx/`
Expected: PASS

- [ ] **Step 6: 커밋**

```bash
go mod tidy
git add go.mod go.sum internal/httpx
git commit -m "feat: add client IP resolution, keyed rate limits and admin auth" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: acme-dns HTTP API, `/health`, `serve`·`healthcheck` 명령

**Files:**
- Create: `internal/acmedns/health.go`, `internal/acmedns/http.go`, `internal/acmedns/http_test.go`
- Create: `internal/app/provider.go`, `internal/app/serve.go`, `internal/app/healthcheck.go`, `internal/app/healthcheck_test.go`
- Modify: `cmd/ssl-checker/main.go` (전체 교체)

**Interfaces:**
- Consumes: `acmedns.Service`, `httpx.*`, `store.AuditEntry`, `dnsprovider.Error`, `dnsprovider.KindOf`, `config.Load(config.RoleAPI, …)`, `cloudflare.New`, `memory.New`, `db.Open`
- Produces:
  - `acmedns.Pinger` 인터페이스(`Ping(ctx) error`), `acmedns.HealthChecker{DB Pinger; Provider dnsprovider.Provider; CacheFor time.Duration}`, `(*HealthChecker).Check(ctx) (HealthStatus, bool)`, `acmedns.HealthStatus{DB, Provider string}` (JSON `db`, `provider`; 값 `ok` / `error` / `transient` / `config`)
  - `acmedns.Handler{Svc *Service; IP httpx.ClientIP; Admin *httpx.AdminAuth; UpdateLimit *httpx.Keyed; Health *HealthChecker; Log *slog.Logger}`, `(*Handler).Routes() http.Handler`
  - HTTP 계약:
    - `POST /update` → 200 `{"txt":"…"}` / 401 `{"error":"forbidden"}` / 400 `{"error":"bad_txt"}` / 429 `{"error":"rate_limited"}` / 500 `{"error":"dns_provider_error"}` 또는 `{"error":"db_error"}`
    - `POST /register` (Bearer ADMIN_KEY), 본문 `{"domain":"a.com","allowfrom":["10.0.0.0/8"]}` → 201 `{"username","password","fulldomain","subdomain","allowfrom"}` / 400 `malformed_json_payload`·`invalid_allowfrom_cidr`·`invalid_domain` / 409 `account_exists`
    - `GET /health` → 200 또는 503, 본문 `{"db":"ok","provider":"ok"}`
  - `app.Serve(ctx, args []string, getenv) error`, `app.Healthcheck(ctx, args []string) error`, `app.newProvider(cfg config.Config) (dnsprovider.Provider, error)`

`/update` 처리 순서는 원본 acme-dns를 따른다: 인증 → allowfrom → 본문 해석(실패해도 빈 값으로 계속) → subdomain 일치 → 값 검증. 앞의 세 단계 실패는 모두 401 `forbidden`이다. 호환 테스트(Task 11)에서 다르게 나오면 원본에 맞춘다.

- [ ] **Step 1: 핸들러 실패 테스트 작성**

`internal/acmedns/http_test.go`:

```go
package acmedns_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/store"
)

const adminKey = "test-admin-key-0123456789abcdef0123"

type fixture struct {
	t    *testing.T
	h    http.Handler
	mem  *memory.Provider
	pool *pgxpool.Pool
}

type fixtureOpts struct {
	trusted []netip.Prefix
	burst   int
}

func newFixture(t *testing.T, o fixtureOpts) *fixture {
	t.Helper()
	if o.burst == 0 {
		o.burst = 100
	}
	pool := dbtest.New(t)
	mem := memory.New()
	ip := httpx.ClientIP{Trusted: o.trusted}
	h := &acmedns.Handler{
		Svc:         &acmedns.Service{Store: store.New(pool), Provider: mem, Zone: testZone, TTL: 60},
		IP:          ip,
		Admin:       httpx.NewAdminAuth(adminKey, ip),
		UpdateLimit: httpx.NewKeyed(time.Hour, o.burst),
		Health:      &acmedns.HealthChecker{DB: pool, Provider: mem},
		Log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return &fixture{t: t, h: h.Routes(), mem: mem, pool: pool}
}

type req struct {
	method, path, body, remote string
	headers                    map[string]string
}

func (f *fixture) do(r req) *httptest.ResponseRecorder {
	f.t.Helper()
	hr := httptest.NewRequest(r.method, r.path, strings.NewReader(r.body))
	if r.remote != "" {
		hr.RemoteAddr = r.remote
	}
	for k, v := range r.headers {
		hr.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, hr)
	return rec
}

type registration struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullDomain string   `json:"fulldomain"`
	Subdomain  string   `json:"subdomain"`
	AllowFrom  []string `json:"allowfrom"`
}

func registerReq(body string) req {
	return req{method: "POST", path: "/register", body: body, headers: map[string]string{"Authorization": "Bearer " + adminKey}}
}

func (f *fixture) register(domain string, allow ...string) registration {
	f.t.Helper()
	body, _ := json.Marshal(map[string]any{"domain": domain, "allowfrom": allow})
	rec := f.do(registerReq(string(body)))
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("register: %d %s", rec.Code, rec.Body)
	}
	var reg registration
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		f.t.Fatal(err)
	}
	return reg
}

func updateReq(reg registration, value string) req {
	body, _ := json.Marshal(map[string]string{"subdomain": reg.Subdomain, "txt": value})
	return req{method: "POST", path: "/update", body: string(body),
		headers: map[string]string{"X-Api-User": reg.Username, "X-Api-Key": reg.Password}}
}

func expect(t *testing.T, rec *httptest.ResponseRecorder, status int, body map[string]string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q: %v", rec.Body, err)
	}
	if !maps.Equal(got, body) {
		t.Fatalf("body = %v, want %v", got, body)
	}
}

func (f *fixture) txtCount() int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM txt_records`).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestRegisterEndpoint(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	body := `{"domain":"a.com","allowfrom":["10.1.2.3/16","192.168.0.5"]}`
	rec := f.do(registerReq(body))
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v %s", rec.Code, rec.Header(), rec.Body)
	}
	var reg registration
	if err := json.Unmarshal(rec.Body.Bytes(), &reg); err != nil {
		t.Fatal(err)
	}
	if reg.FullDomain != reg.Subdomain+"."+testZone || len(reg.Password) != 40 {
		t.Fatalf("reg = %+v", reg)
	}
	if strings.Join(reg.AllowFrom, ",") != "10.1.0.0/16,192.168.0.5/32" {
		t.Fatalf("allowfrom = %v", reg.AllowFrom)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_logs WHERE action = 'register' AND target = 'a.com'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("audit rows = %d, %v", n, err)
	}
}

func TestRegisterRequiresAdminKey(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	for _, h := range []map[string]string{nil, {"Authorization": "Bearer wrong"}} {
		rec := f.do(req{method: "POST", path: "/register", body: `{"domain":"a.com"}`, headers: h})
		expect(t, rec, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	expect(t, f.do(registerReq(`{not json`)), http.StatusBadRequest, map[string]string{"error": "malformed_json_payload"})
	expect(t, f.do(registerReq(`{"domain":"a.com","allowfrom":["10.0.0.0/99"]}`)), http.StatusBadRequest, map[string]string{"error": "invalid_allowfrom_cidr"})
	expect(t, f.do(registerReq(`{"domain":"*.a.com"}`)), http.StatusBadRequest, map[string]string{"error": "invalid_domain"})
	f.register("a.com")
	expect(t, f.do(registerReq(`{"domain":"a.com"}`)), http.StatusConflict, map[string]string{"error": "account_exists"})
}

func TestUpdateStoresAndRotates(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	for _, v := range []string{txt('a'), txt('b'), txt('c')} {
		expect(t, f.do(updateReq(reg, v)), http.StatusOK, map[string]string{"txt": v})
	}
	got := f.mem.Values(reg.FullDomain)
	if strings.Join(got, ",") != txt('b')+","+txt('c') {
		t.Fatalf("provider = %v", got)
	}
}

func TestUpdateForbidden(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	other := f.register("b.com")
	forbidden := map[string]string{"error": "forbidden"}
	mutate := func(fn func(*req)) req { r := updateReq(reg, txt('a')); fn(&r); return r }
	cases := map[string]req{
		"wrong key":          mutate(func(r *req) { r.headers["X-Api-Key"] = "wrong" }),
		"long key":           mutate(func(r *req) { r.headers["X-Api-Key"] = strings.Repeat("k", 100) }),
		"unknown user":       mutate(func(r *req) { r.headers["X-Api-User"] = "11111111-1111-1111-1111-111111111111" }),
		"non-uuid user":      mutate(func(r *req) { r.headers["X-Api-User"] = "abc" }),
		"missing headers":    mutate(func(r *req) { r.headers = nil }),
		"other subdomain":    mutate(func(r *req) { r.body = `{"subdomain":"` + other.Subdomain + `","txt":"` + txt('a') + `"}` }),
		"malformed json":     mutate(func(r *req) { r.body = `{not json` }),
		"empty body":         mutate(func(r *req) { r.body = `` }),
		"oversized body":     mutate(func(r *req) { r.body = `{"subdomain":"` + reg.Subdomain + `","txt":"` + strings.Repeat("a", 5000) + `"}` }),
		"other account keys": mutate(func(r *req) { r.headers["X-Api-User"] = other.Username; r.headers["X-Api-Key"] = other.Password }),
	}
	for name, r := range cases {
		t.Run(name, func(t *testing.T) { expect(t, f.do(r), http.StatusUnauthorized, forbidden) })
	}
	if f.txtCount() != 0 || len(f.mem.Values(reg.FullDomain)) != 0 {
		t.Fatal("forbidden request wrote values")
	}
}

func TestUpdateBadTXT(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	for _, v := range []string{"", strings.Repeat("A", 42), strings.Repeat("+", 43)} {
		expect(t, f.do(updateReq(reg, v)), http.StatusBadRequest, map[string]string{"error": "bad_txt"})
	}
}

func TestUpdateAllowFrom(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com", "10.1.0.0/16")
	ok := updateReq(reg, txt('a'))
	ok.remote = "10.1.2.3:4000"
	expect(t, f.do(ok), http.StatusOK, map[string]string{"txt": txt('a')})
	denied := updateReq(reg, txt('b'))
	denied.remote = "10.2.0.1:4000"
	expect(t, f.do(denied), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
}

func TestUpdateBehindTrustedProxy(t *testing.T) {
	f := newFixture(t, fixtureOpts{trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}})
	reg := f.register("a.com", "192.168.5.0/24")
	cases := []struct {
		name, remote, xff string
		status            int
	}{
		{"proxy forwards allowed client", "10.0.0.2:4000", "192.168.5.7", http.StatusOK},
		{"untrusted client spoofs header", "203.0.113.9:4000", "192.168.5.7", http.StatusUnauthorized},
		{"spoofed leftmost hop ignored", "10.0.0.2:4000", "192.168.5.7, 203.0.113.50", http.StatusUnauthorized},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := updateReq(reg, txt('a'))
			r.remote = c.remote
			r.headers["X-Forwarded-For"] = c.xff
			if rec := f.do(r); rec.Code != c.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, c.status, rec.Body)
			}
		})
	}
}

func TestUpdateProviderFailure(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	f.mem.SetFailure(errors.New("cloudflare down"))
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusInternalServerError, map[string]string{"error": "dns_provider_error"})
	if f.txtCount() != 0 {
		t.Fatal("db written despite provider failure")
	}
	var detail string
	if err := f.pool.QueryRow(context.Background(), `SELECT detail FROM audit_logs WHERE action = 'update_failed'`).Scan(&detail); err != nil || !strings.HasPrefix(detail, "transient") {
		t.Fatalf("audit detail = %q, %v", detail, err)
	}
	f.mem.SetFailure(nil)
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
}

func TestUpdateRevokedAccount(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	if _, err := f.pool.Exec(context.Background(), `UPDATE acme_accounts SET revoked_at = now()`); err != nil {
		t.Fatal(err)
	}
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusUnauthorized, map[string]string{"error": "forbidden"})
}

func TestUpdateRateLimit(t *testing.T) {
	f := newFixture(t, fixtureOpts{burst: 2})
	reg := f.register("a.com")
	f.do(updateReq(reg, txt('a')))
	f.do(updateReq(reg, txt('b')))
	expect(t, f.do(updateReq(reg, txt('c'))), http.StatusTooManyRequests, map[string]string{"error": "rate_limited"})
}

func TestHealth(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	expect(t, f.do(req{method: "GET", path: "/health"}), http.StatusOK, map[string]string{"db": "ok", "provider": "ok"})
	f.mem.SetFailure(errors.New("down"))
	expect(t, f.do(req{method: "GET", path: "/health"}), http.StatusServiceUnavailable, map[string]string{"db": "ok", "provider": "transient"})
}
```

- [ ] **Step 2: 실패 확인**

Run: `make test`
Expected: FAIL (`undefined: acmedns.Handler`)

- [ ] **Step 3: health 구현**

`internal/acmedns/health.go`:

```go
package acmedns

import (
	"context"
	"sync"
	"time"

	"ssl-checker/internal/dnsprovider"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

// HealthChecker는 DB 연결과 DNS 공급자 Check를 확인한다.
// 공급자 확인은 외부 API를 부르므로 CacheFor 동안 결과를 재사용한다.
type HealthChecker struct {
	DB       Pinger
	Provider dnsprovider.Provider
	CacheFor time.Duration

	mu      sync.Mutex
	checked time.Time
	provErr error
}

type HealthStatus struct {
	DB       string `json:"db"`
	Provider string `json:"provider"`
}

func (h *HealthChecker) Check(ctx context.Context) (HealthStatus, bool) {
	st := HealthStatus{DB: "ok", Provider: "ok"}
	healthy := true
	if err := h.DB.Ping(ctx); err != nil {
		st.DB = "error"
		healthy = false
	}
	if err := h.providerErr(ctx); err != nil {
		st.Provider = dnsprovider.KindOf(err).String()
		healthy = false
	}
	return st, healthy
}

func (h *HealthChecker) providerErr(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.checked.IsZero() && time.Since(h.checked) < h.CacheFor {
		return h.provErr
	}
	h.provErr = h.Provider.Check(ctx)
	h.checked = time.Now()
	return h.provErr
}
```

- [ ] **Step 4: 핸들러 구현**

`internal/acmedns/http.go`:

```go
package acmedns

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"

	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const (
	maxBodyBytes = 4 << 10
	maxActorLen  = 64
)

type Handler struct {
	Svc         *Service
	IP          httpx.ClientIP
	Admin       *httpx.AdminAuth
	UpdateLimit *httpx.Keyed
	Health      *HealthChecker
	Log         *slog.Logger
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /update", h.update)
	mux.Handle("POST /register", h.Admin.Wrap(http.HandlerFunc(h.register)))
	mux.HandleFunc("GET /health", h.health)
	return mux
}

type updateRequest struct {
	Subdomain string `json:"subdomain"`
	TXT       string `json:"txt"`
}

// update는 acme.sh dns_acmedns가 부르는 유일한 API다. 검사 순서와 응답은 원본 acme-dns를 따른다.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := h.IP.From(r)
	user := truncate(r.Header.Get("X-Api-User"), maxActorLen)
	deny := func(reason string) {
		h.audit(ctx, store.AuditEntry{Actor: "account:" + user, Action: "update_denied", SourceIP: ip.String(), Detail: reason})
		httpx.WriteError(w, http.StatusUnauthorized, "forbidden")
	}

	acct, err := h.Svc.Authenticate(ctx, r.Header.Get("X-Api-User"), r.Header.Get("X-Api-Key"))
	if errors.Is(err, ErrForbidden) {
		deny("invalid_credentials")
		return
	}
	if err != nil {
		h.Log.Error("authenticate", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if !allowed(acct.AllowFrom, ip) {
		deny("ip_unauthorized")
		return
	}
	var req updateRequest
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req) // 해석 실패는 빈 값으로 두고 아래 일치 검사에서 거부한다
	if req.Subdomain != acct.Subdomain {
		deny("subdomain_mismatch")
		return
	}
	if !h.UpdateLimit.Allow(acct.Username) {
		httpx.WriteError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	target := h.Svc.FQDN(acct.Subdomain)
	actor := "account:" + acct.Username
	err = h.Svc.UpdateTXT(ctx, acct, req.TXT)
	switch {
	case errors.Is(err, ErrBadTXT):
		httpx.WriteError(w, http.StatusBadRequest, "bad_txt")
	case err != nil:
		code, detail := "db_error", err.Error()
		var perr *dnsprovider.Error
		if errors.As(err, &perr) {
			code, detail = "dns_provider_error", perr.Kind.String()+": "+err.Error()
		}
		h.Log.Error("update txt", "subdomain", acct.Subdomain, "err", err)
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update_failed", Target: target, SourceIP: ip.String(), Detail: detail})
		httpx.WriteError(w, http.StatusInternalServerError, code)
	default:
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update", Target: target, SourceIP: ip.String()})
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"txt": req.TXT})
	}
}

type registerRequest struct {
	Domain    string   `json:"domain"`
	AllowFrom []string `json:"allowfrom"`
}

type registerResponse struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullDomain string   `json:"fulldomain"`
	Subdomain  string   `json:"subdomain"`
	AllowFrom  []string `json:"allowfrom"`
}

// register는 관리자 전용이다. 응답의 password는 이때 한 번만 나온다.
func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req registerRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes)).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json_payload")
		return
	}
	prefixes := make([]netip.Prefix, 0, len(req.AllowFrom))
	for _, s := range req.AllowFrom {
		p, err := netutil.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom_cidr")
			return
		}
		prefixes = append(prefixes, p)
	}
	reg, err := h.Svc.Register(ctx, req.Domain, prefixes)
	switch {
	case errors.Is(err, ErrInvalidDomain):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	case errors.Is(err, store.ErrActiveAccountExists):
		httpx.WriteError(w, http.StatusConflict, "account_exists")
		return
	case err != nil:
		h.Log.Error("register", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "db_error")
		return
	}
	h.audit(ctx, store.AuditEntry{Actor: "admin", Action: "register", Target: netutil.NormalizeName(req.Domain),
		SourceIP: h.IP.From(r).String(), Detail: "subdomain=" + reg.Subdomain})

	allow := make([]string, 0, len(reg.AllowFrom))
	for _, p := range reg.AllowFrom {
		allow = append(allow, p.String())
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, registerResponse{
		Username: reg.Username, Password: reg.Password, FullDomain: reg.FullDomain, Subdomain: reg.Subdomain, AllowFrom: allow,
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	st, healthy := h.Health.Check(r.Context())
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	httpx.WriteJSON(w, status, st)
}

func (h *Handler) audit(ctx context.Context, e store.AuditEntry) {
	if err := h.Svc.Store.Audit(ctx, e); err != nil {
		h.Log.Error("audit", "action", e.Action, "err", err)
	}
}

func allowed(prefixes []netip.Prefix, ip netip.Addr) bool {
	if len(prefixes) == 0 {
		return true
	}
	for _, p := range prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
```

- [ ] **Step 5: 통과 확인**

Run: `make test`
Expected: PASS

- [ ] **Step 6: healthcheck 실패 테스트 작성**

`internal/app/healthcheck_test.go`:

```go
package app_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"ssl-checker/internal/app"
)

func TestHealthcheck(t *testing.T) {
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"db":"ok","provider":"config"}`))
	}))
	defer srv.Close()

	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err != nil {
		t.Fatalf("healthy: %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err == nil {
		t.Fatal("503 reported healthy")
	}
	srv.Close()
	if err := app.Healthcheck(context.Background(), []string{"--url", srv.URL + "/health"}); err == nil {
		t.Fatal("connection refused reported healthy")
	}
}
```

- [ ] **Step 7: 실패 확인**

Run: `go test ./internal/app/`
Expected: FAIL (`undefined: app.Healthcheck`)

- [ ] **Step 8: serve, healthcheck, provider 선택 구현**

`internal/app/healthcheck.go`:

```go
package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Healthcheck는 실행 중인 api의 /health를 호출한다. compose 헬스체크용이라 이미지에 curl이 필요 없다.
func Healthcheck(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	target := fs.String("url", "http://127.0.0.1:8080/health", "health endpoint URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *target, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}
```

`internal/app/provider.go`:

```go
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
```

`internal/app/serve.go`:

```go
package app

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"time"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/store"
)

const (
	updateEvery = 6 * time.Second // 계정별 /update 토큰 보충 주기
	updateBurst = 10
)

// Serve는 acme-dns 호환 API를 연다. SIGTERM을 받으면 진행 중인 요청을 마치고 끝낸다.
func Serve(ctx context.Context, args []string, getenv func(string) string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	listen := fs.String("listen", ":8080", "listen address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(config.RoleAPI, getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := newProvider(cfg)
	if err != nil {
		return err
	}

	ip := httpx.ClientIP{Trusted: cfg.TrustedProxies}
	h := &acmedns.Handler{
		Svc:         &acmedns.Service{Store: store.New(pool), Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL},
		IP:          ip,
		Admin:       httpx.NewAdminAuth(cfg.AdminKey, ip),
		UpdateLimit: httpx.NewKeyed(updateEvery, updateBurst),
		Health:      &acmedns.HealthChecker{DB: pool, Provider: provider, CacheFor: time.Minute},
		Log:         slog.Default(),
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("serving", "addr", *listen, "provider", cfg.DNSProvider, "zone", cfg.ChallengeZone)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

`cmd/ssl-checker/main.go` (전체 교체):

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ssl-checker/internal/app"
)

const usage = `usage: ssl-checker <command> [flags]

commands:
  migrate                    DB 마이그레이션을 적용하고 종료한다
  serve [--listen :8080]     acme-dns 호환 API를 연다
  healthcheck [--url URL]    실행 중인 api의 /health를 확인한다
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "migrate":
		err = app.Migrate(ctx, os.Getenv)
	case "serve":
		err = app.Serve(ctx, os.Args[2:], os.Getenv)
	case "healthcheck":
		err = app.Healthcheck(ctx, os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 9: 통과 확인**

Run: `make test && go vet ./...`
Expected: PASS, vet 경고 없음

- [ ] **Step 10: 로컬 수동 확인**

```bash
export DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable'
go run ./cmd/ssl-checker migrate
DNS_PROVIDER=memory CHALLENGE_ZONE=acme-chk.test ADMIN_KEY=local-admin-key-0123456789abcdef0123 \
  go run ./cmd/ssl-checker serve --listen 127.0.0.1:18080 &
sleep 2
curl -s -X POST localhost:18080/register -H 'Authorization: Bearer local-admin-key-0123456789abcdef0123' -d '{"domain":"a.com"}'
go run ./cmd/ssl-checker healthcheck --url http://127.0.0.1:18080/health && echo HEALTHY
kill %1
docker exec sslchk-testdb psql -U postgres -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
```

Expected: register 응답에 `username`, `password`(40자), `fulldomain`(`<uuid>.acme-chk.test`), 그리고 `HEALTHY`

- [ ] **Step 11: 커밋**

```bash
git add internal/acmedns internal/app cmd
git commit -m "feat: add acme-dns compatible HTTP API, health endpoint, serve and healthcheck commands" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: 고아 TXT 정리와 `worker` 명령

**Files:**
- Create: `internal/cleanup/cleanup.go`, `internal/cleanup/cleanup_test.go`
- Create: `internal/app/worker.go`
- Modify: `cmd/ssl-checker/main.go` (전체 교체)

**Interfaces:**
- Consumes: `store.ActiveTXTBySubdomain`, `store.UpdateTXT`, `store.Audit`, `dnsprovider.Provider`, `acmedns.ValidSubdomain`, `acmedns.Service`(테스트에서 계정 생성), `config.Load(config.RoleWorker, …)`, `newProvider`
- Produces:
  - `cleanup.Cleaner{Store *store.Store; Provider dnsprovider.Provider; Zone string; TTL int; Log *slog.Logger}`, `cleanup.Result{Deleted, Repaired int}`, `(*Cleaner).Run(ctx) (Result, error)`
  - `app.Worker(ctx, getenv) error`

정리 규칙: 공급자 레코드 중 이름이 `<UUID>.<zone>` 형태인 것만 본다. 활성 계정이 없는(모르는·폐기된) 이름은 지운다. 활성 계정이 있는데 값이 DB와 다르면 계정 행을 잠근 채 DB 값으로 다시 맞춘다. 잠금을 거치므로 진행 중인 `/update`(공급자에는 썼지만 아직 커밋 전)의 값을 지우지 않는다.

- [ ] **Step 1: 실패 테스트 작성**

`internal/cleanup/cleanup_test.go`:

```go
package cleanup_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
	"ssl-checker/internal/store"
)

const zone = "acme-chk.test"

func txt(c byte) string { return strings.Repeat(string(c), 43) }

type env struct {
	t    *testing.T
	ctx  context.Context
	pool *pgxpool.Pool
	st   *store.Store
	mem  *memory.Provider
	svc  *acmedns.Service
	c    *cleanup.Cleaner
}

func newEnv(t *testing.T) *env {
	pool := dbtest.New(t)
	st := store.New(pool)
	mem := memory.New()
	return &env{
		t: t, ctx: context.Background(), pool: pool, st: st, mem: mem,
		svc: &acmedns.Service{Store: st, Provider: mem, Zone: zone, TTL: 60},
		c:   &cleanup.Cleaner{Store: st, Provider: mem, Zone: zone, TTL: 60, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
	}
}

// account는 계정을 만들고 DB에 dbVals를, 공급자에 provVals를 직접 넣는다.
func (e *env) account(domain string, dbVals, provVals []string) store.Account {
	e.t.Helper()
	reg, err := e.svc.Register(e.ctx, domain, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	acct, err := e.st.ActiveAccountByUsername(e.ctx, reg.Username)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.st.UpdateTXT(e.ctx, acct.ID, func([]string) ([]string, error) { return dbVals, nil }); err != nil {
		e.t.Fatal(err)
	}
	if err := e.mem.SetTXT(e.ctx, reg.FullDomain, provVals, 60); err != nil {
		e.t.Fatal(err)
	}
	return acct
}

func (e *env) values(label string) []string { return e.mem.Values(label + "." + zone) }

func TestCleanup(t *testing.T) {
	e := newEnv(t)
	inSync := e.account("insync.com", []string{txt('a')}, []string{txt('a')})
	lost := e.account("lost.com", nil, []string{txt('y')})
	stale := e.account("stale.com", []string{txt('a'), txt('b')}, []string{txt('a'), txt('z')})
	revoked := e.account("revoked.com", []string{txt('r')}, []string{txt('r')})
	if _, err := e.pool.Exec(e.ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, revoked.ID); err != nil {
		t.Fatal(err)
	}
	orphan := uuid.NewString()
	if err := e.mem.SetTXT(e.ctx, orphan+"."+zone, []string{txt('o')}, 60); err != nil {
		t.Fatal(err)
	}

	res, err := e.c.Run(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 2 || res.Repaired != 2 {
		t.Fatalf("result = %+v, want 2 deleted (revoked, orphan) and 2 repaired (lost, stale)", res)
	}
	checks := map[string][]string{
		inSync.Subdomain:  {txt('a')},
		lost.Subdomain:    {},
		stale.Subdomain:   {txt('a'), txt('b')},
		revoked.Subdomain: {},
		orphan:            {},
	}
	for label, want := range checks {
		got := e.values(label)
		if got == nil {
			got = []string{}
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s = %v, want %v", label, got, want)
		}
	}
	var n int
	if err := e.pool.QueryRow(e.ctx, `SELECT count(*) FROM audit_logs WHERE action = 'txt_cleanup'`).Scan(&n); err != nil || n != 4 {
		t.Fatalf("audit rows = %d, %v", n, err)
	}

	again, err := e.c.Run(e.ctx)
	if err != nil || again != (cleanup.Result{}) {
		t.Fatalf("second run = %+v, %v; want no changes", again, err)
	}
}

func TestCleanupLeavesNonUUIDNames(t *testing.T) {
	e := newEnv(t)
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone, "8E5700EA-A4BF-41C7-8A77-E990661DCC6A-x." + zone} {
		if err := e.mem.SetTXT(e.ctx, fqdn, []string{"keep"}, 60); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.c.Run(e.ctx)
	if err != nil || res != (cleanup.Result{}) {
		t.Fatalf("result = %+v, %v", res, err)
	}
	for _, fqdn := range []string{zone, "www." + zone, "_dmarc." + zone} {
		if got := e.mem.Values(fqdn); !slices.Equal(got, []string{"keep"}) {
			t.Errorf("%s = %v; cleanup touched a non-challenge record", fqdn, got)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `make test`
Expected: FAIL (`undefined: cleanup.Cleaner`)

- [ ] **Step 3: 정리 구현**

`internal/cleanup/cleanup.go`:

```go
// Package cleanup은 DNS 공급자의 챌린지 TXT를 DB와 대조해 고아 레코드를 정리한다.
package cleanup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/dnsprovider"
	"ssl-checker/internal/store"
)

type Cleaner struct {
	Store    *store.Store
	Provider dnsprovider.Provider
	Zone     string
	TTL      int
	Log      *slog.Logger
}

type Result struct {
	Deleted  int // 활성 계정이 없는 이름의 TXT를 지운 수
	Repaired int // 활성 계정의 TXT를 DB 값으로 다시 맞춘 수
}

// Run은 이름이 <UUID>.<zone>인 레코드만 다룬다. 존의 다른 레코드는 건드리지 않는다.
// 한 이름의 실패가 나머지 정리를 막지 않도록, 오류는 모아서 마지막에 돌려준다.
func (c *Cleaner) Run(ctx context.Context) (Result, error) {
	var res Result
	recs, err := c.Provider.ListTXT(ctx, c.Zone)
	if err != nil {
		return res, fmt.Errorf("cleanup: list provider records: %w", err)
	}
	active, err := c.Store.ActiveTXTBySubdomain(ctx)
	if err != nil {
		return res, fmt.Errorf("cleanup: load active accounts: %w", err)
	}
	have := map[string][]string{}
	for _, r := range recs {
		label, ok := strings.CutSuffix(r.FQDN, "."+c.Zone)
		if !ok || !acmedns.ValidSubdomain(label) {
			continue
		}
		have[label] = append(have[label], r.Value)
	}

	var errs []error
	for _, label := range slices.Sorted(maps.Keys(have)) {
		fqdn := label + "." + c.Zone
		acct, ok := active[label]
		if ok && sameSet(have[label], acct.Values) {
			continue
		}
		if ok {
			// 계정 행을 잠근 채 커밋된 값으로 맞춘다. 진행 중인 /update가 있으면 끝날 때까지 기다린다.
			err := c.Store.UpdateTXT(ctx, acct.AccountID, func(current []string) ([]string, error) {
				return current, c.Provider.SetTXT(ctx, fqdn, current, c.TTL)
			})
			if err == nil {
				res.Repaired++
				c.audit(ctx, fqdn, "repaired")
				continue
			}
			if !errors.Is(err, store.ErrNotFound) {
				errs = append(errs, fmt.Errorf("cleanup: repair %s: %w", fqdn, err))
				continue
			}
			// 그사이 폐기됐다면 아래에서 지운다.
		}
		if err := c.Provider.DeleteTXT(ctx, fqdn); err != nil {
			errs = append(errs, fmt.Errorf("cleanup: delete %s: %w", fqdn, err))
			continue
		}
		res.Deleted++
		c.audit(ctx, fqdn, "deleted")
	}
	return res, errors.Join(errs...)
}

func (c *Cleaner) audit(ctx context.Context, fqdn, detail string) {
	if err := c.Store.Audit(ctx, store.AuditEntry{Actor: "worker", Action: "txt_cleanup", Target: fqdn, Detail: detail}); err != nil {
		c.Log.Error("audit", "err", err)
	}
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}
```

- [ ] **Step 4: 통과 확인**

Run: `make test`
Expected: PASS

- [ ] **Step 5: worker 명령 구현**

`internal/app/worker.go`:

```go
package app

import (
	"context"
	"log/slog"
	"time"

	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/store"
)

const cleanupEvery = 24 * time.Hour

// Worker는 주기 작업을 돌린다. 1단계에서는 하루 한 번 고아 TXT 정리만 한다(시작 직후 1회 포함).
// 작업 중복을 막기 위해 worker는 1개만 띄운다.
func Worker(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(config.RoleWorker, getenv)
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := newProvider(cfg)
	if err != nil {
		return err
	}
	cleaner := &cleanup.Cleaner{Store: store.New(pool), Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL, Log: slog.Default()}

	run := func() {
		res, err := cleaner.Run(ctx)
		if err != nil {
			slog.Error("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired, "err", err)
			return
		}
		slog.Info("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired)
	}
	run()
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}
```

`cmd/ssl-checker/main.go` (전체 교체):

```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ssl-checker/internal/app"
)

const usage = `usage: ssl-checker <command> [flags]

commands:
  migrate                    DB 마이그레이션을 적용하고 종료한다
  serve [--listen :8080]     acme-dns 호환 API를 연다
  worker                     주기 작업(고아 TXT 정리)을 돌린다
  healthcheck [--url URL]    실행 중인 api의 /health를 확인한다
`

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "migrate":
		err = app.Migrate(ctx, os.Getenv)
	case "serve":
		err = app.Serve(ctx, os.Args[2:], os.Getenv)
	case "worker":
		err = app.Worker(ctx, os.Getenv)
	case "healthcheck":
		err = app.Healthcheck(ctx, os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		slog.Error("command failed", "command", os.Args[1], "err", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 6: 빌드와 전체 테스트**

Run: `go build ./... && make test && go vet ./...`
Expected: PASS

- [ ] **Step 7: 커밋**

```bash
git add internal/cleanup internal/app cmd
git commit -m "feat: add orphan TXT cleanup and worker command" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: 이미지, compose 배포 구성, smoke 테스트

**Files:**
- Create: `Dockerfile`, `.dockerignore`
- Create: `deploy/compose.yaml`, `deploy/local.env.example`
- Create: `scripts/smoke.sh`
- Create: `README.md`

**Interfaces:**
- Consumes: 하위 명령 `migrate`, `serve --listen :8080`, `worker`, `healthcheck`
- Produces: 이미지 `${IMAGE}:${APP_VERSION}` (바이너리 `/usr/local/bin/ssl-checker`), Komodo Stack용 `deploy/compose.yaml`

- [ ] **Step 1: Dockerfile 작성**

`Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ssl-checker ./cmd/ssl-checker

# distroless/static: 셸·curl 없음. CA 인증서는 포함돼 Cloudflare HTTPS 호출이 된다.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ssl-checker /usr/local/bin/ssl-checker
USER nonroot
CMD ["ssl-checker", "serve", "--listen", ":8080"]
```

`.dockerignore`:

```
.git
.worktrees
docs
deploy
scripts
test
*.md
```

- [ ] **Step 2: compose 작성**

`deploy/compose.yaml`:

```yaml
name: ssl-checker

# 비밀값(ADMIN_KEY, CF_API_TOKEN, POSTGRES_PASSWORD)은 Komodo Environment에서 [[이름]] 보간으로만 넣는다.
# 이 파일과 env_file에 직접 쓰지 않는다.
x-common-env: &common-env
  DATABASE_URL: postgres://sslchk:${POSTGRES_PASSWORD}@db:5432/sslchk
  DNS_PROVIDER: ${DNS_PROVIDER}
  CF_API_TOKEN: ${CF_API_TOKEN:-}
  CF_ZONE_ID: ${CF_ZONE_ID:-}
  CHALLENGE_ZONE: ${CHALLENGE_ZONE}
  TXT_TTL: ${TXT_TTL:-60}

services:
  migrate:
    image: ${IMAGE}:${APP_VERSION}
    command: ["ssl-checker", "migrate"]
    environment:
      DATABASE_URL: postgres://sslchk:${POSTGRES_PASSWORD}@db:5432/sslchk
    depends_on:
      db:
        condition: service_healthy
    restart: "no"

  api:
    image: ${IMAGE}:${APP_VERSION}
    command: ["ssl-checker", "serve", "--listen", ":8080"]
    environment:
      <<: *common-env
      ADMIN_KEY: ${ADMIN_KEY}
      TRUSTED_PROXIES: ${TRUSTED_PROXIES:-}
    ports:
      - "${BIND_ADDR}:${API_PORT:-8080}:8080"
    depends_on:
      migrate:
        condition: service_completed_successfully
    healthcheck:
      test: ["CMD", "ssl-checker", "healthcheck"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    restart: unless-stopped

  worker:
    image: ${IMAGE}:${APP_VERSION}
    command: ["ssl-checker", "worker"]
    environment:
      <<: *common-env
      CHECK_INTERVAL: ${CHECK_INTERVAL:-6h}
    depends_on:
      migrate:
        condition: service_completed_successfully
    restart: unless-stopped

  db:
    image: postgres:17
    environment:
      POSTGRES_USER: sslchk
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      POSTGRES_DB: sslchk
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sslchk -h 127.0.0.1"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

volumes:
  pgdata:
```

`deploy/local.env.example` (로컬 smoke 전용, 비밀값 아님):

```dotenv
IMAGE=ssl-checker
APP_VERSION=dev
BIND_ADDR=127.0.0.1
API_PORT=18090
TRUSTED_PROXIES=
DNS_PROVIDER=memory
CHALLENGE_ZONE=acme-chk.test
TXT_TTL=60
CHECK_INTERVAL=6h
ADMIN_KEY=local-admin-key-0123456789abcdef0123
CF_API_TOKEN=
CF_ZONE_ID=
POSTGRES_PASSWORD=localpassword
```

- [ ] **Step 3: smoke 스크립트 작성**

`scripts/smoke.sh`:

```sh
#!/bin/sh
# 이미지를 빌드하고 compose 스택(memory 공급자)을 띄워 register → update → health를 확인한다.
set -eu
cd "$(dirname "$0")/.."

ENV_FILE=deploy/local.env.example
# shellcheck disable=SC1090
. "./$ENV_FILE"
COMPOSE="docker compose -p sslchk-smoke -f deploy/compose.yaml --env-file $ENV_FILE"
BASE="http://${BIND_ADDR}:${API_PORT}"

docker build -t "${IMAGE}:${APP_VERSION}" .
trap '$COMPOSE down -v >/dev/null 2>&1' EXIT
$COMPOSE up -d

i=0
until [ "$($COMPOSE ps api --format '{{.Health}}')" = "healthy" ]; do
  i=$((i + 1))
  if [ "$i" -gt 60 ]; then
    $COMPOSE logs
    echo "api did not become healthy" >&2
    exit 1
  fi
  sleep 2
done

field() { printf '%s' "$1" | sed -n "s/.*\"$2\":\"\([^\"]*\)\".*/\1/p"; }

reg=$(curl -fsS -X POST "$BASE/register" -H "Authorization: Bearer $ADMIN_KEY" -d '{"domain":"smoke.example"}')
user=$(field "$reg" username)
pass=$(field "$reg" password)
sub=$(field "$reg" subdomain)
txt=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA

resp=$(curl -fsS -X POST "$BASE/update" -H "X-Api-User: $user" -H "X-Api-Key: $pass" \
  -d "{\"subdomain\":\"$sub\",\"txt\":\"$txt\"}")
echo "$resp" | grep -q "\"txt\":\"$txt\"" || { echo "unexpected update response: $resp" >&2; exit 1; }

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/update" -H "X-Api-User: $user" -H "X-Api-Key: wrong" \
  -d "{\"subdomain\":\"$sub\",\"txt\":\"$txt\"}")
[ "$code" = 401 ] || { echo "wrong key returned $code" >&2; exit 1; }

$COMPOSE ps worker --format '{{.State}}' | grep -q running || { $COMPOSE logs worker; echo "worker not running" >&2; exit 1; }
echo "smoke OK"
```

```bash
chmod +x scripts/smoke.sh
```

- [ ] **Step 4: smoke 실행**

Run: `./scripts/smoke.sh`
Expected: 마지막 줄 `smoke OK`. 실패하면 출력된 `docker compose logs`로 원인을 찾아 고친다.

- [ ] **Step 5: README 작성**

`README.md`:

````markdown
# ssl-checker

사내 도메인의 DNS-01 검증을 대행하는 acme-dns 호환 API. 각 서버의 acme.sh가 `dns_acmedns` 플러그인으로 `/update`를 호출하면, 앱이 챌린지 전용 존(`CHALLENGE_ZONE`)의 TXT를 DNS 공급자(기본 Cloudflare)에 기록한다. 인증서와 개인키는 각 서버가 직접 다룬다.

설계 문서: 「ssl-checker 구현 계획서 (acme-dns 호환 API 방식)」 https://claude.ai/code/artifact/4b4d7215-cacb-4e14-967d-ddb156a69e9d

## 명령

| 명령 | 역할 |
| --- | --- |
| `ssl-checker migrate` | DB 마이그레이션 후 종료 |
| `ssl-checker serve --listen :8080` | `/update`, `/register`(ADMIN_KEY), `/health` |
| `ssl-checker worker` | 하루 한 번 고아 TXT 정리 |
| `ssl-checker healthcheck` | compose 헬스체크용 `/health` 확인 |

## 개발

```bash
make db-up && make test && make db-down   # 단위·통합 테스트
./scripts/smoke.sh                        # 이미지 + compose 스택 smoke
./scripts/compat.sh                       # 원본 acme-dns와 응답 대조
```

## 배포

`deploy/compose.yaml`을 Komodo git 기반 Stack으로 등록한다. 비밀값은 Environment에 `[[SSLCHK_ADMIN_KEY]]`처럼 보간으로만 넣는다. 변수 목록과 운영 절차는 설계 문서의 「배포」 절을 따른다. 첫 실서버 적용 전 `docs/runbooks/gate1-staging.md`의 스테이징 발급을 통과해야 한다.
````

- [ ] **Step 6: 커밋**

```bash
git add Dockerfile .dockerignore deploy scripts/smoke.sh README.md
git commit -m "feat: add container image, compose stack and smoke test" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 11: 원본 acme-dns 호환 테스트와 게이트 1 런북

**Files:**
- Create: `test/compat/compose.yaml`, `test/compat/acme-dns.cfg`, `test/compat/compat_test.go`
- Create: `scripts/compat.sh`
- Create: `docs/runbooks/gate1-staging.md`
- Modify (불일치가 나올 때만): `internal/acmedns/http.go`, `internal/acmedns/http_test.go`

**Interfaces:**
- Consumes: 이미지 `ssl-checker:dev`, HTTP 계약(Task 8)
- Produces: `go test -tags compat ./test/compat/` — 같은 요청을 원본 acme-dns와 ssl-checker에 보내 상태 코드와 JSON 본문이 같은지 확인

- [ ] **Step 1: 원본 acme-dns 설정과 compose 작성**

`test/compat/acme-dns.cfg`:

```toml
[general]
listen = "0.0.0.0:53"
protocol = "both"
domain = "auth.compat.test"
nsname = "auth.compat.test"
nsadmin = "admin.compat.test"
records = [
    "auth.compat.test. A 127.0.0.1",
    "auth.compat.test. NS auth.compat.test.",
]
debug = false

[database]
engine = "sqlite3"
connection = "/var/lib/acme-dns/acme-dns.db"

[api]
ip = "0.0.0.0"
disable_registration = false
port = "80"
tls = "none"
corsorigins = ["*"]
use_header = false
header_name = "X-Forwarded-For"

[logconfig]
loglevel = "debug"
logtype = "stdout"
logformat = "text"
```

`test/compat/compose.yaml`:

```yaml
name: sslchk-compat

services:
  acmedns:
    image: joohoi/acme-dns:latest
    volumes:
      - ./acme-dns.cfg:/etc/acme-dns/config.cfg:ro
    ports:
      - "127.0.0.1:18080:80"

  db:
    image: postgres:17
    environment:
      POSTGRES_USER: sslchk
      POSTGRES_PASSWORD: compat
      POSTGRES_DB: sslchk
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U sslchk -h 127.0.0.1"]
      interval: 2s
      timeout: 3s
      retries: 30

  migrate:
    image: ssl-checker:dev
    command: ["ssl-checker", "migrate"]
    environment:
      DATABASE_URL: postgres://sslchk:compat@db:5432/sslchk?sslmode=disable
    depends_on:
      db:
        condition: service_healthy

  api:
    image: ssl-checker:dev
    command: ["ssl-checker", "serve", "--listen", ":8080"]
    environment:
      DATABASE_URL: postgres://sslchk:compat@db:5432/sslchk?sslmode=disable
      DNS_PROVIDER: memory
      CHALLENGE_ZONE: acme-chk.test
      ADMIN_KEY: compat-admin-key-0123456789abcdef0123
    ports:
      - "127.0.0.1:18081:8080"
    depends_on:
      migrate:
        condition: service_completed_successfully
```

- [ ] **Step 2: 대조 테스트 작성**

`test/compat/compat_test.go`:

```go
//go:build compat

// Package compat은 같은 /update 요청을 원본 acme-dns와 ssl-checker에 보내 응답이 같은지 확인한다.
// scripts/compat.sh가 두 서버를 띄운 뒤 실행한다.
package compat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

var (
	origURL  = envOr("COMPAT_ACMEDNS_URL", "http://127.0.0.1:18080")
	oursURL  = envOr("COMPAT_SSLCHK_URL", "http://127.0.0.1:18081")
	adminKey = envOr("COMPAT_ADMIN_KEY", "compat-admin-key-0123456789abcdef0123")
)

type creds struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Subdomain string `json:"subdomain"`
}

type reply struct {
	Status int
	Body   any
}

func post(t *testing.T, url string, headers map[string]string, body string) reply {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &parsed); err != nil {
		parsed = map[string]any{"_raw": string(raw)}
	}
	return reply{Status: resp.StatusCode, Body: parsed}
}

func register(t *testing.T, url string, headers map[string]string, body string) creds {
	t.Helper()
	r := post(t, url+"/register", headers, body)
	if r.Status != http.StatusCreated {
		t.Fatalf("register %s: %d %v", url, r.Status, r.Body)
	}
	raw, _ := json.Marshal(r.Body)
	var c creds
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestUpdateMatchesAcmeDNS(t *testing.T) {
	orig := register(t, origURL, nil, "")
	ours := register(t, oursURL, map[string]string{"Authorization": "Bearer " + adminKey},
		fmt.Sprintf(`{"domain":"compat-%d.example"}`, time.Now().UnixNano()))

	valid := strings.Repeat("A", 43)
	auth := func(c creds) map[string]string {
		return map[string]string{"X-Api-User": c.Username, "X-Api-Key": c.Password, "Content-Type": "application/json"}
	}
	body := func(sub, txt string) string { return fmt.Sprintf(`{"subdomain":%q,"txt":%q}`, sub, txt) }
	stranger := uuid.NewString()

	cases := []struct {
		name  string
		build func(c creds) (map[string]string, string)
	}{
		{"valid", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, valid) }},
		{"second value", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("B", 43)) }},
		{"third value", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("C", 43)) }},
		{"same value again", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, valid) }},
		{"wrong key", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-Key"] = "wrong"
			return h, body(c.Subdomain, valid)
		}},
		{"unknown user", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-User"] = stranger
			return h, body(c.Subdomain, valid)
		}},
		{"non-uuid user", func(c creds) (map[string]string, string) {
			h := auth(c)
			h["X-Api-User"] = "abc"
			return h, body(c.Subdomain, valid)
		}},
		{"missing headers", func(c creds) (map[string]string, string) { return nil, body(c.Subdomain, valid) }},
		{"subdomain mismatch", func(c creds) (map[string]string, string) { return auth(c), body(stranger, valid) }},
		{"malformed json", func(c creds) (map[string]string, string) { return auth(c), "{not json" }},
		{"empty body", func(c creds) (map[string]string, string) { return auth(c), "" }},
		{"txt 42 chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("A", 42)) }},
		{"txt 44 chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("A", 44)) }},
		{"txt bad chars", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, strings.Repeat("+", 43)) }},
		{"txt empty", func(c creds) (map[string]string, string) { return auth(c), body(c.Subdomain, "") }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h1, b1 := c.build(orig)
			h2, b2 := c.build(ours)
			r1 := post(t, origURL+"/update", h1, b1)
			r2 := post(t, oursURL+"/update", h2, b2)
			if r1.Status != r2.Status || !reflect.DeepEqual(r1.Body, r2.Body) {
				t.Errorf("acme-dns: %d %v\nssl-checker: %d %v", r1.Status, r1.Body, r2.Status, r2.Body)
			}
		})
	}
}
```

- [ ] **Step 3: 실행 스크립트 작성**

`scripts/compat.sh`:

```sh
#!/bin/sh
# 원본 acme-dns와 ssl-checker(memory 공급자)를 띄우고 /update 응답을 대조한다.
set -eu
cd "$(dirname "$0")/.."
COMPOSE="docker compose -f test/compat/compose.yaml"

docker build -t ssl-checker:dev .
trap '$COMPOSE down -v >/dev/null 2>&1' EXIT
$COMPOSE up -d

wait_for() {
  i=0
  until curl -s -o /dev/null "$1"; do
    i=$((i + 1))
    if [ "$i" -gt 60 ]; then
      $COMPOSE logs
      echo "$1 not reachable" >&2
      exit 1
    fi
    sleep 1
  done
}
wait_for http://127.0.0.1:18080/health
wait_for http://127.0.0.1:18081/health

go test -tags compat -count=1 -v ./test/compat/
```

```bash
chmod +x scripts/compat.sh
```

- [ ] **Step 4: 대조 실행**

Run: `./scripts/compat.sh`
Expected: `--- PASS: TestUpdateMatchesAcmeDNS` (하위 테스트 15개 모두 PASS)

- [ ] **Step 5: 불일치가 있으면 원본에 맞춘다**

FAIL로 나온 하위 테스트마다 출력의 `acme-dns:` 쪽 상태 코드와 본문을 정답으로 보고 `internal/acmedns/http.go`의 `update`를 고친다. 그리고 같은 기대값을 `internal/acmedns/http_test.go`의 해당 테스트(`TestUpdateForbidden`, `TestUpdateBadTXT` 등)에도 반영한다. 예: 원본이 `bad_txt`를 401로 돌려준다면 `case errors.Is(err, ErrBadTXT)`의 상태 코드와 `TestUpdateBadTXT`의 기대값을 함께 바꾼다. 고친 뒤 `make test`와 `./scripts/compat.sh`를 다시 돌려 둘 다 PASS인지 확인한다. 불일치가 없으면 이 단계는 건너뛴다.

- [ ] **Step 6: 게이트 1 런북 작성**

`docs/runbooks/gate1-staging.md`:

````markdown
# 게이트 1: Let's Encrypt 스테이징 호환 테스트

통과 기준: Let's Encrypt **스테이징**에서 `dns_acmedns`로 테스트 도메인과 그 와일드카드를 한 인증서로 발급한다. 실서버 적용은 이 게이트를 통과한 뒤에만 한다.

## 준비물

- 챌린지 전용 존(예: `acme-chk.net`, 이름은 운영 전에 확정). Cloudflare에 존을 추가하고 등록기관에서 네임서버를 Cloudflare로 위임해 둔다.
- 그 존의 `Zone > DNS > Edit` 권한만 가진 API 토큰. 클라이언트 IP 필터에 앱 서버 IP를 넣는다.
- DNS를 직접 바꿀 수 있는 테스트 도메인(예: `test.a.com`). 운영 도메인을 쓰지 않는다.
- acme.sh 3.1.4 이상을 설치할 리눅스 머신 1대(앱 API에 사내망으로 닿아야 한다).

## 절차

1. 앱 배포: `DNS_PROVIDER=cloudflare`, `CHALLENGE_ZONE=<존>`, `CF_API_TOKEN=<토큰>`(필요하면 `CF_ZONE_ID`)으로 Stack을 배포한다. `curl http://<앱>:8080/health`가 `{"db":"ok","provider":"ok"}`인지 확인한다.
2. 계정 등록:
   ```bash
   curl -s -X POST http://<앱>:8080/register \
     -H "Authorization: Bearer $ADMIN_KEY" \
     -d '{"domain":"test.a.com","allowfrom":["<테스트 머신 IP>/32"]}'
   ```
   응답의 `username`, `password`, `subdomain`, `fulldomain`을 기록한다. `password`는 다시 볼 수 없다.
3. CNAME 등록: 테스트 도메인 DNS에 `_acme-challenge.test.a.com. CNAME <fulldomain>.`을 넣고 확인한다.
   ```bash
   dig +short CNAME _acme-challenge.test.a.com @1.1.1.1   # <fulldomain>. 이 나와야 한다
   ```
4. 발급 (테스트 머신):
   ```bash
   curl -fsSL https://get.acme.sh | sh -s email=ops@company.example
   ~/.acme.sh/acme.sh --version          # 3.1.4 이상
   export ACMEDNS_BASE_URL="http://<앱>:8080"
   export ACMEDNS_USERNAME="<username>"
   export ACMEDNS_PASSWORD="<password>"
   export ACMEDNS_SUBDOMAIN="<subdomain>"
   ~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d test.a.com -d '*.test.a.com'
   ```
5. 확인:
   - acme.sh 출력에 `Cert success.`가 있다.
   - `openssl x509 -in ~/.acme.sh/test.a.com_ecc/test.a.com.cer -noout -issuer -ext subjectAltName`의 발급자가 스테이징 CA이고, SAN에 `test.a.com`과 `*.test.a.com`이 모두 있다.
   - DB의 `audit_logs`에 `action='update'` 행이 2개 이상 있고 `update_failed`가 없다.
   - Cloudflare 존의 `<subdomain>.<존>` TXT가 2개다.
6. 기록: 날짜, acme.sh 버전, 앱 버전, 결과를 설계 문서의 「일정과 단계별 산출물」 옆에 남긴다.

## 정리

```bash
~/.acme.sh/acme.sh --remove -d test.a.com --staging
```

테스트 계정 폐기는 2단계의 관리 API(`DELETE /api/domains/{id}`)가 생기면 그것으로 한다. 그 전에는 `UPDATE acme_accounts SET revoked_at = now() WHERE subdomain = '<subdomain>'`로 폐기하고, 다음 worker 정리 때 TXT가 지워지는지 확인한다.

## 실패 시

| 증상 | 확인할 것 |
| --- | --- |
| acme.sh가 `/update` 응답을 실패로 봄 | `audit_logs`의 `update_denied` 사유(`invalid_credentials`, `ip_unauthorized`, `subdomain_mismatch`) |
| `update_failed` + `config` | 토큰 권한, 존 ID, 토큰 IP 필터 |
| `update_failed` + `transient` | Cloudflare 장애·요청 제한. 잠시 후 재시도 |
| CA 검증 실패(TXT 못 찾음) | CNAME 대상 오타, 챌린지 존 네임서버 위임 |
````

- [ ] **Step 7: 전체 확인**

Run: `make test && go vet ./... && ./scripts/smoke.sh && ./scripts/compat.sh`
Expected: 모두 PASS, `smoke OK`

- [ ] **Step 8: 커밋**

```bash
git add test/compat scripts/compat.sh docs/runbooks internal/acmedns
git commit -m "test: add acme-dns compatibility suite and gate 1 staging runbook" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

- [ ] **Step 9: 테스트 DB 정리**

Run: `make db-down`

---

## 이 계획 이후

- **게이트 1**은 `docs/runbooks/gate1-staging.md`를 실제 Cloudflare 존과 테스트 도메인으로 수행해야 통과한다. 챌린지 존 이름 확정(설계 문서의 열린 댓글)이 선행 조건이다.
- Komodo 쪽 등록(Build, git 기반 Stack, Periphery `[secrets]`, ResourceSync TOML)은 이 저장소의 `Dockerfile`과 `deploy/compose.yaml`을 가리키도록 Komodo에서 직접 설정한다. 사내 레지스트리 주소와 배포 서버가 정해지면 ResourceSync TOML을 별도 작업으로 추가한다.
- 2단계 계획: 관리 API(`/api/domains`, cname-check, bootstrap, rotate-key, 폐기), ADMIN_KEY 세션 관리 화면, CNAME 공개 리졸버 확인, CAA 확인, 일회용 부트스트랩 다운로드.
- 3단계 계획: `monitor_targets`·`check_results` 마이그레이션, IP별 TLS 점검(6시간 주기), 대시보드, CNAME 감시, 연속 `update_failed` 경고.
