# 모니터링 1: 점검 엔진과 조회 API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 등록된 도메인의 모니터링 대상을 worker가 주기적으로 TLS 점검하고 ACME CNAME을 확인해 저장하며, 누구나 그 상태를 `/api/...` JSON으로 조회할 수 있게 한다.

**Architecture:** 판정 규칙은 DB와 무관한 순수 함수(`internal/monitor`)로, TLS 접속은 `monitor.Prober`, 주기 실행은 `monitor.Runner`로 나눈다. CNAME 확인은 `internal/cnamecheck`가 공개 리졸버에 직접 질의한다. 저장은 `internal/store`가 맡고(store가 monitor 타입을 import, 반대 방향은 없음), 조회 API는 새 패키지 `internal/adminapi`가 기존 acme-dns 경로와 같은 `http.ServeMux`에 붙는다.

**Tech Stack:** Go 1.27, PostgreSQL 17 + pgx/v5, `crypto/tls`·`crypto/x509`, `golang.org/x/net/dns/dnsmessage`(새 의존성)

**Spec:** `docs/superpowers/specs/2026-10-07-monitoring-dashboard-design.md` (이 계획은 스펙의 「구현 계획 분할」 1번이다. 2번 관리 API·세션, 3번 대시보드·llms.txt는 별도 계획)

## Global Constraints

- 모듈 경로는 `ssl-checker`. import는 `ssl-checker/internal/...`.
- 주석·로그·문서는 한국어, 식별자는 영어. 주석은 "왜"만 짧게, 기존 파일의 밀도를 따른다.
- 새 외부 의존성은 `golang.org/x/net`(dnsmessage)만 허용한다.
- DB 테스트는 `internal/db/dbtest.New(t)`를 쓴다. `TEST_DATABASE_URL`이 없으면 건너뛴다. 실행: `make db-up` 후 `make test`.
- 판정 기준값: 만료 임박 **7일 미만** critical, 갱신 지연 **수명의 1/3 미만** warning, TLS **1.2 미만** warning, RSA **2048비트 미만**·ECDSA **256비트 미만** info, `/update` 연속 실패 **3회 이상** warning.
- 접속 타임아웃 **10초**, CNAME 질의 타임아웃 **5초**, 동시 접속 최대 **8**, 점검 기록 보관 **90일**.
- `CHECK_INTERVAL` 기본 **6h**, 최소 **5m**. `DNS_RESOLVER` 기본 **`1.1.1.1:53`**. `PUBLIC_URL` 기본 비움.
- 조회 응답의 시각은 RFC 3339 **UTC**. 키·키 해시는 어떤 조회 응답에도 넣지 않는다.
- 커밋 메시지 끝에 `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

- IP 목록에서 IP를 뺀 대상: 빠진 IP의 옛 결과가 상태·만료일에 계속 섞이면 안 된다 → Task 5 `TestLatestResultsDropsRemovedIPs`.
- TCP는 받지만 TLS 응답을 안 하는 서버: 타임아웃 안에 `CONNECT_FAILED`로 끝나고 주기 전체가 멈추지 않아야 한다 → Task 4 `TestProbeHandshakeTimeout`, `TestRunnerCheckAllRunsConcurrently`.
- 이미 만료된 인증서: `CERT_EXPIRED` 하나로만 판정하고 체인 실패(`CHAIN_INVALID`)로 오판하지 않아야 한다 → Task 4 `TestProbeExpired`.
- IPv6 IP를 넣은 대상: 접속 주소가 `[::1]:port`로 기록되고 저장·필터와 맞아야 한다 → Task 4 `TestProbeIPv6`, Task 5 `TestTargets`.
- 리졸버가 돌려준 CNAME의 대소문자·끝 점 차이: `ok`로 판정해야 한다 → Task 2 `TestCheck` 의 `case-and-dot` 케이스.

---

## 파일 구조

| 파일 | 책임 |
| --- | --- |
| `internal/db/migrations/0002_monitoring.sql` | 새 테이블, ACME 상태 컬럼, cascade 외래 키 |
| `internal/cnamecheck/cnamecheck.go` | `_acme-challenge` CNAME 직접 질의와 비교 |
| `internal/cnamecheck/cnametest/cnametest.go` | 테스트용 최소 UDP DNS 서버(다른 패키지 테스트도 씀) |
| `internal/monitor/status.go` | `Status`, 판정 코드, 정렬 도우미 |
| `internal/monitor/result.go` | `Target`, `Result` |
| `internal/monitor/evaluate.go` | 주소 하나의 판정 `Evaluate` |
| `internal/monitor/assess.go` | 대상·도메인 판정 `AssessTarget`, `AssessDomain` |
| `internal/monitor/probe.go` | TLS 접속과 인증서 수집 `Prober` |
| `internal/monitor/runner.go` | 점검 후 저장, 동시 실행 `Runner` |
| `internal/store/monitor.go` | 도메인·대상·점검 결과 저장 |
| `internal/store/acme_state.go` | ACME 계정의 CNAME·갱신 상태 |
| `internal/acmedns/http.go` | `/update` 성공·실패를 ACME 상태에 기록 |
| `internal/config/config.go` | `CHECK_INTERVAL`, `DNS_RESOLVER`, `PUBLIC_URL` |
| `internal/app/monitorcycle.go` | worker의 주기 작업(점검, CNAME, 정리) |
| `internal/app/worker.go` | 주기 작업 스케줄 |
| `internal/httpx/clientip.go` | `Proxied`: 직접 연결이 신뢰 프록시인지 |
| `internal/adminapi/api.go` | 조회 API 핸들러 |
| `internal/adminapi/view.go` | 응답 모양과 판정 합치기 |
| `internal/app/serve.go` | 상위 mux에 acme-dns 경로와 `/api` 등록 |

---

### Task 1: 마이그레이션 0002

**Files:**
- Create: `internal/db/migrations/0002_monitoring.sql`
- Modify: `internal/db/migrate_test.go` (`schema_migrations` 개수 기대값 1 → 2, 새 테스트 추가)

**Interfaces:**
- Produces: 테이블 `monitor_targets`, `check_results`, `acme_accounts`의 새 컬럼 `cname_status`, `cname_value`, `cname_checked_at`, `last_update_at`, `last_failure_at`, `consecutive_failures`. `domains.status` 삭제. `acme_accounts.domain_id`, `txt_records.account_id`는 `ON DELETE CASCADE`.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/db/migrate_test.go`의 `TestMigrateCreatesTablesAndIsIdempotent`에서 `if n != 1 {`를 `if n != 2 {`로 바꾸고(메시지에 숫자가 있으면 함께), 파일 끝에 추가:

```go
func TestMonitoringSchema(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()

	var hasStatus bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'domains' AND column_name = 'status')`).Scan(&hasStatus); err != nil {
		t.Fatal(err)
	}
	if hasStatus {
		t.Error("domains.status should be dropped")
	}

	// 도메인을 지우면 계정, TXT, 대상, 점검 기록이 함께 지워진다.
	var domainID, accountID, targetID int64
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO domains (fqdn) VALUES ('a.com') RETURNING id`).Scan(&domainID))
	must(pool.QueryRow(ctx, `INSERT INTO acme_accounts (domain_id, username, key_hash, subdomain)
		VALUES ($1, gen_random_uuid(), 'h', gen_random_uuid()) RETURNING id`, domainID).Scan(&accountID))
	_, err := pool.Exec(ctx, `INSERT INTO txt_records (account_id, value) VALUES ($1, 'v')`, accountID)
	must(err)
	must(pool.QueryRow(ctx, `INSERT INTO monitor_targets (domain_id, hostname) VALUES ($1, 'www.a.com') RETURNING id`, domainID).Scan(&targetID))
	_, err = pool.Exec(ctx, `INSERT INTO check_results (target_id, address, status) VALUES ($1, '10.0.0.1:443', 'ok')`, targetID)
	must(err)

	var cname string
	var failures int
	must(pool.QueryRow(ctx, `SELECT cname_status, consecutive_failures FROM acme_accounts WHERE id = $1`, accountID).Scan(&cname, &failures))
	if cname != "unknown" || failures != 0 {
		t.Fatalf("acme defaults = %q, %d", cname, failures)
	}

	_, err = pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, domainID)
	must(err)
	for _, table := range []string{"acme_accounts", "txt_records", "monitor_targets", "check_results"} {
		var n int
		must(pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Errorf("%s has %d rows after domain delete", table, n)
		}
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `make db-up; TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go test ./internal/db/ -run 'TestMonitoringSchema|TestMigrateCreates' -v`
Expected: FAIL (`domains.status should be dropped`, `monitor_targets` 없음, 마이그레이션 개수 1)

- [ ] **Step 3: 마이그레이션 작성**

`internal/db/migrations/0002_monitoring.sql`:

```sql
-- 모니터링: 도메인별 점검 대상과 점검 기록, ACME 계정의 CNAME·갱신 상태.
-- 도메인 상태는 대상과 ACME에서 계산하므로 저장하지 않는다.
ALTER TABLE domains DROP COLUMN status;

CREATE TABLE monitor_targets (
    id         bigserial PRIMARY KEY,
    domain_id  bigint      NOT NULL REFERENCES domains (id) ON DELETE CASCADE,
    hostname   text        NOT NULL,
    port       integer     NOT NULL DEFAULT 443 CHECK (port BETWEEN 1 AND 65535),
    sni        text        NOT NULL DEFAULT '',
    ips        text[]      NOT NULL DEFAULT '{}',
    memo       text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (domain_id, hostname, port, sni)
);

-- 인증서를 받지 못한 행도 넣기 쉽게 not_before/not_after만 NULL을 허용한다.
CREATE TABLE check_results (
    id          bigserial PRIMARY KEY,
    target_id   bigint      NOT NULL REFERENCES monitor_targets (id) ON DELETE CASCADE,
    address     text        NOT NULL,
    checked_at  timestamptz NOT NULL DEFAULT now(),
    status      text        NOT NULL,
    reasons     text[]      NOT NULL DEFAULT '{}',
    tls_version text        NOT NULL DEFAULT '',
    cipher      text        NOT NULL DEFAULT '',
    subject_cn  text        NOT NULL DEFAULT '',
    sans        text[]      NOT NULL DEFAULT '{}',
    issuer      text        NOT NULL DEFAULT '',
    serial      text        NOT NULL DEFAULT '',
    not_before  timestamptz,
    not_after   timestamptz,
    key_alg     text        NOT NULL DEFAULT '',
    key_bits    integer     NOT NULL DEFAULT 0,
    chain_ok    boolean     NOT NULL DEFAULT false,
    hostname_ok boolean     NOT NULL DEFAULT false,
    error       text        NOT NULL DEFAULT ''
);

CREATE INDEX check_results_target_time ON check_results (target_id, checked_at DESC);
CREATE INDEX check_results_time ON check_results (checked_at);

ALTER TABLE acme_accounts
    ADD COLUMN cname_status         text    NOT NULL DEFAULT 'unknown',
    ADD COLUMN cname_value          text    NOT NULL DEFAULT '',
    ADD COLUMN cname_checked_at     timestamptz,
    ADD COLUMN last_update_at       timestamptz,
    ADD COLUMN last_failure_at      timestamptz,
    ADD COLUMN consecutive_failures integer NOT NULL DEFAULT 0;

-- 도메인을 지우면 계정과 TXT 기록도 함께 지운다. 감사 로그는 텍스트라 남는다.
ALTER TABLE acme_accounts
    DROP CONSTRAINT acme_accounts_domain_id_fkey,
    ADD CONSTRAINT acme_accounts_domain_id_fkey FOREIGN KEY (domain_id) REFERENCES domains (id) ON DELETE CASCADE;
ALTER TABLE txt_records
    DROP CONSTRAINT txt_records_account_id_fkey,
    ADD CONSTRAINT txt_records_account_id_fkey FOREIGN KEY (account_id) REFERENCES acme_accounts (id) ON DELETE CASCADE;
```

`grep -rn "status" internal/store internal/acmedns internal/cleanup`로 `domains.status`를 읽거나 쓰는 코드가 없는지 확인한다(현재는 없다).

- [ ] **Step 4: 통과 확인**

Run: `make test`
Expected: 전체 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/db
git commit -m "feat: add monitoring schema migration" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: CNAME 확인 (`cnamecheck`)

**Files:**
- Create: `internal/cnamecheck/cnamecheck.go`, `internal/cnamecheck/cnamecheck_test.go`, `internal/cnamecheck/cnametest/cnametest.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `type cnamecheck.Status string`, 상수 `StatusUnknown="unknown"`, `StatusOK="ok"`, `StatusMissing="missing"`, `StatusMismatch="mismatch"`, `StatusError="error"`
  - `type cnamecheck.Result struct { Status Status; Value string; Err error }`
  - `type cnamecheck.Checker struct { Resolver string; Timeout time.Duration }`
  - `func (c *Checker) Check(ctx context.Context, domain, want string) Result`
  - `func cnamecheck.ChallengeName(domain string) string` → `"_acme-challenge." + domain`
  - `type cnametest.Answer struct { CNAME string; RCode dnsmessage.RCode; Drop bool }`
  - `func cnametest.Serve(t testing.TB, answers map[string]Answer) string` → `127.0.0.1:port`. 키는 소문자 FQDN + 끝 점(`"_acme-challenge.a.com."`). 없는 이름은 NOERROR + 답 없음.

- [ ] **Step 1: 의존성 추가**

Run: `go get golang.org/x/net@latest`

- [ ] **Step 2: 테스트 DNS 서버 작성**

`internal/cnamecheck/cnametest/cnametest.go`:

```go
// Package cnametest는 테스트에서 쓰는 최소 UDP DNS 서버를 띄운다.
package cnametest

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// Answer는 이름 하나에 대한 응답이다. CNAME이 있으면 CNAME 레코드 하나를 돌려주고,
// RCode로 NXDOMAIN 등을 흉내 낸다. Drop이면 응답하지 않는다(타임아웃 시험용).
type Answer struct {
	CNAME string
	RCode dnsmessage.RCode
	Drop  bool
}

// Serve는 answers로 응답하는 서버를 띄우고 주소를 돌려준다. 키는 소문자 FQDN에 끝 점을 붙인다.
func Serve(t testing.TB, answers map[string]Answer) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			var q dnsmessage.Message
			if q.Unpack(buf[:n]) != nil || len(q.Questions) != 1 {
				continue
			}
			a := answers[strings.ToLower(q.Questions[0].Name.String())]
			if a.Drop {
				continue
			}
			resp := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: q.ID, Response: true, RCode: a.RCode},
				Questions: q.Questions,
			}
			if a.CNAME != "" {
				resp.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: q.Questions[0].Name, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(a.CNAME)},
				}}
			}
			out, err := resp.Pack()
			if err != nil {
				continue
			}
			_, _ = pc.WriteTo(out, addr)
		}
	}()
	return pc.LocalAddr().String()
}
```

- [ ] **Step 3: 실패하는 테스트 작성**

`internal/cnamecheck/cnamecheck_test.go`:

```go
package cnamecheck_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
)

const want = "u1.acme.example.com"

func TestCheck(t *testing.T) {
	resolver := cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.ok.com.":       {CNAME: "u1.acme.example.com."},
		"_acme-challenge.case.com.":     {CNAME: "U1.ACME.Example.COM."},
		"_acme-challenge.other.com.":    {CNAME: "u2.acme.example.com."},
		"_acme-challenge.nx.com.":       {RCode: dnsmessage.RCodeNameError},
		"_acme-challenge.servfail.com.": {RCode: dnsmessage.RCodeServerFailure},
		"_acme-challenge.drop.com.":     {Drop: true},
	})
	c := &cnamecheck.Checker{Resolver: resolver, Timeout: 300 * time.Millisecond}
	cases := []struct {
		name, domain string
		status       cnamecheck.Status
		value        string
	}{
		{"ok", "ok.com", cnamecheck.StatusOK, want},
		{"case-and-dot", "case.com", cnamecheck.StatusOK, want},
		{"mismatch", "other.com", cnamecheck.StatusMismatch, "u2.acme.example.com"},
		{"no-record", "empty.com", cnamecheck.StatusMissing, ""},
		{"nxdomain", "nx.com", cnamecheck.StatusMissing, ""},
		{"servfail", "servfail.com", cnamecheck.StatusError, ""},
		{"timeout", "drop.com", cnamecheck.StatusError, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			got := c.Check(context.Background(), tc.domain, want+".")
			if got.Status != tc.status || got.Value != tc.value {
				t.Fatalf("Check(%s) = %+v, want %s %q", tc.domain, got, tc.status, tc.value)
			}
			if (tc.status == cnamecheck.StatusError) != (got.Err != nil) {
				t.Fatalf("Err = %v", got.Err)
			}
			if time.Since(start) > 2*time.Second {
				t.Fatalf("took %s", time.Since(start))
			}
		})
	}
}

func TestChallengeName(t *testing.T) {
	if got := cnamecheck.ChallengeName("a.com"); got != "_acme-challenge.a.com" {
		t.Fatal(got)
	}
}
```

- [ ] **Step 4: 실패 확인**

Run: `go test ./internal/cnamecheck/...`
Expected: FAIL (패키지 `cnamecheck` 없음)

- [ ] **Step 5: 구현**

`internal/cnamecheck/cnamecheck.go`:

```go
// Package cnamecheck는 공개 리졸버에 _acme-challenge CNAME을 직접 물어 ACME 위임이 걸려 있는지 확인한다.
// 사내 DNS가 아니라 Let's Encrypt가 보는 쪽에 맞추려고 지정한 리졸버에만 묻는다.
package cnamecheck

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"ssl-checker/internal/netutil"
)

type Status string

const (
	StatusUnknown  Status = "unknown" // 아직 확인하지 않음 (DB 기본값)
	StatusOK       Status = "ok"
	StatusMissing  Status = "missing"
	StatusMismatch Status = "mismatch"
	StatusError    Status = "error"
)

const DefaultTimeout = 5 * time.Second

type Result struct {
	Status Status
	Value  string // 조회된 CNAME 값(소문자, 끝 점 없음)
	Err    error  // Status가 error일 때만
}

type Checker struct {
	Resolver string        // host:port
	Timeout  time.Duration // 0이면 DefaultTimeout
}

func ChallengeName(domain string) string { return "_acme-challenge." + domain }

// Check는 domain의 _acme-challenge CNAME이 want를 가리키는지 본다. 대소문자와 끝 점은 무시한다.
func (c *Checker) Check(ctx context.Context, domain, want string) Result {
	value, found, err := c.lookup(ctx, ChallengeName(domain))
	switch {
	case err != nil:
		return Result{Status: StatusError, Err: err}
	case !found:
		return Result{Status: StatusMissing}
	case value == netutil.NormalizeName(want):
		return Result{Status: StatusOK, Value: value}
	default:
		return Result{Status: StatusMismatch, Value: value}
	}
}

// lookup은 CNAME 타입으로 직접 질의한다. net.Resolver.LookupCNAME은 A/AAAA를 묻고 체인을 끝까지
// 따라가므로, TXT만 있는 챌린지 대상에서는 결과가 불분명하다.
func (c *Checker) lookup(ctx context.Context, name string) (string, bool, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	qname, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: bad name %q: %w", name, err)
	}
	id := uint16(rand.Uint32())
	q := dnsmessage.Message{
		Header:    dnsmessage.Header{ID: id, RecursionDesired: true},
		Questions: []dnsmessage.Question{{Name: qname, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET}},
	}
	packed, err := q.Pack()
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: pack: %w", err)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "udp", c.Resolver)
	if err != nil {
		return "", false, fmt.Errorf("cnamecheck: dial %s: %w", c.Resolver, err)
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if _, err := conn.Write(packed); err != nil {
		return "", false, fmt.Errorf("cnamecheck: write: %w", err)
	}
	buf := make([]byte, 1232)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return "", false, fmt.Errorf("cnamecheck: read: %w", err)
		}
		var m dnsmessage.Message
		if m.Unpack(buf[:n]) != nil || m.ID != id || !m.Response {
			continue // 다른 질의의 응답이나 깨진 패킷은 무시한다
		}
		switch m.RCode {
		case dnsmessage.RCodeSuccess:
		case dnsmessage.RCodeNameError:
			return "", false, nil
		default:
			return "", false, fmt.Errorf("cnamecheck: %s rcode %s", name, m.RCode)
		}
		for _, a := range m.Answers {
			if a.Header.Type != dnsmessage.TypeCNAME || !strings.EqualFold(a.Header.Name.String(), qname.String()) {
				continue
			}
			if body, ok := a.Body.(*dnsmessage.CNAMEResource); ok {
				return netutil.NormalizeName(body.CNAME.String()), true, nil
			}
		}
		return "", false, nil
	}
}
```

- [ ] **Step 6: 통과 확인**

Run: `go mod tidy && go test ./internal/cnamecheck/... -v && go vet ./internal/cnamecheck/...`
Expected: PASS

- [ ] **Step 7: 커밋**

```bash
git add go.mod go.sum internal/cnamecheck
git commit -m "feat: add direct CNAME check against a public resolver" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: 판정 규칙 (`monitor` 순수 함수)

**Files:**
- Create: `internal/monitor/status.go`, `internal/monitor/result.go`, `internal/monitor/evaluate.go`, `internal/monitor/assess.go`
- Test: `internal/monitor/evaluate_test.go`, `internal/monitor/assess_test.go`

**Interfaces:**
- Consumes: `cnamecheck.Status`, `cnamecheck.StatusMissing`, `cnamecheck.StatusMismatch` (Task 2)
- Produces:
  - `type monitor.Status string` + `StatusUnknown/OK/Info/Warning/Critical`, `func (Status) Valid() bool`, `func Worst(ss ...Status) Status`
  - 코드 상수 `CodeConnectFailed`, `CodeCertExpired`, `CodeCertExpiresSoon`, `CodeChainInvalid`, `CodeHostnameMismatch`, `CodeRenewalOverdue`, `CodeTLSOutdated`, `CodeWeakKey`, `CodeSerialMismatch`, `CodeNotChecked`, `CodeCNAMEMissing`, `CodeCNAMEMismatch`, `CodeTXTUpdateFailing`; `func CodeStatus(code string) Status`
  - `type monitor.Target struct { ID int64; Hostname string; Port int; SNI string; IPs []netip.Addr }`, `func (Target) ServerName() string`, `func (Target) DialAddresses() []string`
  - `type monitor.Result struct { Address string; CheckedAt time.Time; Status Status; Reasons []string; TLSVersion, Cipher, SubjectCN string; SANs []string; Issuer, Serial string; NotBefore, NotAfter time.Time; KeyAlg string; KeyBits int; ChainOK, HostnameOK bool; Error string }` (JSON 태그 포함), `func (Result) HasCert() bool`
  - `func monitor.Evaluate(r Result, now time.Time) (Status, []string)`
  - `type monitor.Assessment struct { Status Status \`json:"status"\`; Reasons []string \`json:"reasons"\` }`
  - `func monitor.AssessTarget(latest []Result) Assessment`
  - `type monitor.ACMEHealth struct { CNAMEStatus cnamecheck.Status; ConsecutiveFailures int }`, `const TXTFailureThreshold = 3`
  - `func monitor.AssessDomain(targets []Assessment, acme *ACMEHealth) Assessment`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/monitor/evaluate_test.go`:

```go
package monitor

import (
	"net/netip"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

// good은 90일짜리 인증서가 60일 남은 정상 결과다.
func good() Result {
	return Result{
		Address: "10.0.0.1:443", CheckedAt: now, TLSVersion: "TLS 1.3",
		NotBefore: now.Add(-30 * day), NotAfter: now.Add(60 * day),
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true, Serial: "01",
	}
}

func TestEvaluate(t *testing.T) {
	cases := []struct {
		name   string
		mod    func(*Result)
		status Status
		codes  []string
	}{
		{"good", func(*Result) {}, StatusOK, []string{}},
		{"connect failed", func(r *Result) { *r = Result{Address: r.Address, Error: "connection refused"} }, StatusCritical, []string{CodeConnectFailed}},
		{"expired only", func(r *Result) { r.NotAfter = now.Add(-time.Hour) }, StatusCritical, []string{CodeCertExpired}},
		{"6 days left", func(r *Result) { r.NotAfter = now.Add(6 * day) }, StatusCritical, []string{CodeCertExpiresSoon}},
		{"7 days left is overdue not soon", func(r *Result) { r.NotBefore = now.Add(-83 * day); r.NotAfter = now.Add(7 * day) }, StatusWarning, []string{CodeRenewalOverdue}},
		{"31 of 90 days left", func(r *Result) { r.NotBefore = now.Add(-59 * day); r.NotAfter = now.Add(31 * day) }, StatusOK, []string{}},
		{"29 of 90 days left", func(r *Result) { r.NotBefore = now.Add(-61 * day); r.NotAfter = now.Add(29 * day) }, StatusWarning, []string{CodeRenewalOverdue}},
		{"chain invalid sorted first", func(r *Result) { r.ChainOK = false; r.NotBefore = now.Add(-61 * day); r.NotAfter = now.Add(29 * day) }, StatusCritical, []string{CodeChainInvalid, CodeRenewalOverdue}},
		{"hostname mismatch", func(r *Result) { r.HostnameOK = false }, StatusCritical, []string{CodeHostnameMismatch}},
		{"tls 1.1", func(r *Result) { r.TLSVersion = "TLS 1.1" }, StatusWarning, []string{CodeTLSOutdated}},
		{"tls 1.2", func(r *Result) { r.TLSVersion = "TLS 1.2" }, StatusOK, []string{}},
		{"rsa 2047", func(r *Result) { r.KeyAlg, r.KeyBits = "RSA", 2047 }, StatusInfo, []string{CodeWeakKey}},
		{"rsa 2048", func(r *Result) { r.KeyAlg, r.KeyBits = "RSA", 2048 }, StatusOK, []string{}},
		{"ecdsa 224", func(r *Result) { r.KeyBits = 224 }, StatusInfo, []string{CodeWeakKey}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := good()
			tc.mod(&r)
			st, codes := Evaluate(r, now)
			if st != tc.status || !slices.Equal(codes, tc.codes) {
				t.Fatalf("Evaluate = %s %v, want %s %v", st, codes, tc.status, tc.codes)
			}
		})
	}
}

func TestWorstAndValid(t *testing.T) {
	if Worst() != StatusUnknown || Worst(StatusOK, StatusUnknown) != StatusOK || Worst(StatusInfo, StatusCritical, StatusWarning) != StatusCritical {
		t.Fatal("Worst order wrong")
	}
	for _, s := range []Status{StatusUnknown, StatusOK, StatusInfo, StatusWarning, StatusCritical} {
		if !s.Valid() {
			t.Errorf("%s should be valid", s)
		}
	}
	if Status("bogus").Valid() || Status("").Valid() {
		t.Error("bogus status valid")
	}
}

func TestDialAddresses(t *testing.T) {
	tg := Target{Hostname: "www.a.com", Port: 8443}
	if got := tg.DialAddresses(); !slices.Equal(got, []string{"www.a.com:8443"}) {
		t.Fatal(got)
	}
	tg.IPs = []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("2001:db8::1")}
	if got := tg.DialAddresses(); !slices.Equal(got, []string{"10.0.0.1:8443", "[2001:db8::1]:8443"}) {
		t.Fatal(got)
	}
	if tg.ServerName() != "www.a.com" {
		t.Fatal(tg.ServerName())
	}
	tg.SNI = "sni.a.com"
	if tg.ServerName() != "sni.a.com" {
		t.Fatal(tg.ServerName())
	}
}
```

`internal/monitor/assess_test.go`:

```go
package monitor

import (
	"slices"
	"testing"

	"ssl-checker/internal/cnamecheck"
)

func res(status Status, serial string, codes ...string) Result {
	return Result{Status: status, Serial: serial, Reasons: append([]string{}, codes...)}
}

func TestAssessTarget(t *testing.T) {
	cases := []struct {
		name   string
		latest []Result
		want   Assessment
	}{
		{"not checked", nil, Assessment{StatusUnknown, []string{CodeNotChecked}}},
		{"same serial", []Result{res(StatusOK, "01"), res(StatusOK, "01")}, Assessment{StatusOK, []string{}}},
		{"serial differs", []Result{res(StatusOK, "01"), res(StatusOK, "02")}, Assessment{StatusWarning, []string{CodeSerialMismatch}}},
		{"failed address has no serial", []Result{res(StatusCritical, "", CodeConnectFailed), res(StatusOK, "01")}, Assessment{StatusCritical, []string{CodeConnectFailed}}},
		{"worst reason first", []Result{res(StatusWarning, "01", CodeRenewalOverdue), res(StatusCritical, "02", CodeChainInvalid)},
			Assessment{StatusCritical, []string{CodeChainInvalid, CodeRenewalOverdue, CodeSerialMismatch}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessTarget(tc.latest)
			if got.Status != tc.want.Status || !slices.Equal(got.Reasons, tc.want.Reasons) {
				t.Fatalf("AssessTarget = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAssessDomain(t *testing.T) {
	ok := Assessment{StatusOK, []string{}}
	crit := Assessment{StatusCritical, []string{CodeCertExpiresSoon}}
	cases := []struct {
		name    string
		targets []Assessment
		acme    *ACMEHealth
		want    Assessment
	}{
		{"nothing", nil, nil, Assessment{StatusUnknown, []string{}}},
		{"acme ok without targets stays unknown", nil, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK}, Assessment{StatusUnknown, []string{}}},
		{"cname missing without targets", nil, &ACMEHealth{CNAMEStatus: cnamecheck.StatusMissing}, Assessment{StatusWarning, []string{CodeCNAMEMissing}}},
		{"cname error is not a warning", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusError}, Assessment{StatusOK, []string{}}},
		{"two failures", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK, ConsecutiveFailures: 2}, Assessment{StatusOK, []string{}}},
		{"three failures", []Assessment{ok}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusOK, ConsecutiveFailures: 3}, Assessment{StatusWarning, []string{CodeTXTUpdateFailing}}},
		{"critical target and mismatch", []Assessment{ok, crit}, &ACMEHealth{CNAMEStatus: cnamecheck.StatusMismatch},
			Assessment{StatusCritical, []string{CodeCertExpiresSoon, CodeCNAMEMismatch}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AssessDomain(tc.targets, tc.acme)
			if got.Status != tc.want.Status || !slices.Equal(got.Reasons, tc.want.Reasons) {
				t.Fatalf("AssessDomain = %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/monitor/`
Expected: FAIL (정의 없음으로 컴파일 실패)

- [ ] **Step 3: 구현**

`internal/monitor/status.go`:

```go
// Package monitor는 TLS 인증서를 점검하고 상태를 판정한다.
package monitor

import "slices"

// Status는 판정 결과다. 나쁜 순서: critical > warning > info > ok > unknown.
type Status string

const (
	StatusUnknown  Status = "unknown"
	StatusOK       Status = "ok"
	StatusInfo     Status = "info"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
)

func (s Status) rank() int {
	switch s {
	case StatusOK:
		return 1
	case StatusInfo:
		return 2
	case StatusWarning:
		return 3
	case StatusCritical:
		return 4
	}
	return 0
}

// Valid는 s가 정의된 상태 값인지 알려 준다. 조회 API의 status 필터 검사에 쓴다.
func (s Status) Valid() bool { return s == StatusUnknown || s.rank() > 0 }

// Worst는 가장 나쁜 상태다. 인자가 없으면 unknown이다.
func Worst(ss ...Status) Status {
	w := StatusUnknown
	for _, s := range ss {
		if s.rank() > w.rank() {
			w = s
		}
	}
	return w
}

// 판정 코드. 화면과 llms.txt가 이 문자열을 그대로 보여 준다.
const (
	CodeConnectFailed    = "CONNECT_FAILED"
	CodeCertExpired      = "CERT_EXPIRED"
	CodeCertExpiresSoon  = "CERT_EXPIRES_SOON"
	CodeChainInvalid     = "CHAIN_INVALID"
	CodeHostnameMismatch = "HOSTNAME_MISMATCH"
	CodeRenewalOverdue   = "RENEWAL_OVERDUE"
	CodeTLSOutdated      = "TLS_OUTDATED"
	CodeWeakKey          = "WEAK_KEY"
	CodeSerialMismatch   = "SERIAL_MISMATCH"
	CodeNotChecked       = "NOT_CHECKED"
	CodeCNAMEMissing     = "CNAME_MISSING"
	CodeCNAMEMismatch    = "CNAME_MISMATCH"
	CodeTXTUpdateFailing = "TXT_UPDATE_FAILING"
)

var codeStatus = map[string]Status{
	CodeConnectFailed:    StatusCritical,
	CodeCertExpired:      StatusCritical,
	CodeCertExpiresSoon:  StatusCritical,
	CodeChainInvalid:     StatusCritical,
	CodeHostnameMismatch: StatusCritical,
	CodeRenewalOverdue:   StatusWarning,
	CodeTLSOutdated:      StatusWarning,
	CodeSerialMismatch:   StatusWarning,
	CodeCNAMEMissing:     StatusWarning,
	CodeCNAMEMismatch:    StatusWarning,
	CodeTXTUpdateFailing: StatusWarning,
	CodeWeakKey:          StatusInfo,
	CodeNotChecked:       StatusUnknown,
}

// CodeStatus는 코드 하나가 뜻하는 상태다. 모르는 코드는 unknown이다.
func CodeStatus(code string) Status {
	if s, ok := codeStatus[code]; ok {
		return s
	}
	return StatusUnknown
}

// sortCodes는 나쁜 코드가 앞에 오도록 안정 정렬한다.
func sortCodes(codes []string) {
	slices.SortStableFunc(codes, func(a, b string) int { return CodeStatus(b).rank() - CodeStatus(a).rank() })
}

func appendUnique(dst []string, codes ...string) []string {
	for _, c := range codes {
		if !slices.Contains(dst, c) {
			dst = append(dst, c)
		}
	}
	return dst
}
```

`internal/monitor/result.go`:

```go
package monitor

import (
	"net"
	"net/netip"
	"strconv"
	"time"
)

// Target은 점검할 TLS 엔드포인트 하나다.
type Target struct {
	ID       int64
	Hostname string
	Port     int
	SNI      string       // 비우면 Hostname
	IPs      []netip.Addr // 비우면 Hostname으로 한 번 접속한다
}

func (t Target) ServerName() string {
	if t.SNI != "" {
		return t.SNI
	}
	return t.Hostname
}

// DialAddresses는 접속할 주소다. IP 목록이 있으면 IP마다 하나, 없으면 hostname:port 하나.
// IP 주소 형식(IPv6는 [..]:port)은 저장된 결과를 대상 설정과 맞춰 볼 때도 같은 값을 쓴다.
func (t Target) DialAddresses() []string {
	if len(t.IPs) == 0 {
		return []string{net.JoinHostPort(t.Hostname, strconv.Itoa(t.Port))}
	}
	out := make([]string, len(t.IPs))
	for i, ip := range t.IPs {
		out[i] = netip.AddrPortFrom(ip, uint16(t.Port)).String()
	}
	return out
}

// Result는 접속 주소 하나의 점검 한 건이다. 인증서를 받지 못하면 NotBefore/NotAfter가 0이다.
type Result struct {
	Address    string    `json:"address"`
	CheckedAt  time.Time `json:"checked_at"`
	Status     Status    `json:"status"`
	Reasons    []string  `json:"reasons"`
	TLSVersion string    `json:"tls_version"`
	Cipher     string    `json:"cipher"`
	SubjectCN  string    `json:"subject_cn"`
	SANs       []string  `json:"sans"`
	Issuer     string    `json:"issuer"`
	Serial     string    `json:"serial"`
	NotBefore  time.Time `json:"not_before,omitzero"`
	NotAfter   time.Time `json:"not_after,omitzero"`
	KeyAlg     string    `json:"key_alg"`
	KeyBits    int       `json:"key_bits"`
	ChainOK    bool      `json:"chain_ok"`
	HostnameOK bool      `json:"hostname_ok"`
	Error      string    `json:"error"`
}

func (r Result) HasCert() bool { return !r.NotAfter.IsZero() }
```

`internal/monitor/evaluate.go`:

```go
package monitor

import "time"

const (
	expiresSoonWithin = 7 * 24 * time.Hour
	minRSABits        = 2048
	minECDSABits      = 256
)

// Evaluate는 접속 주소 하나의 점검 결과를 판정한다. 코드는 나쁜 것부터 돌려준다.
func Evaluate(r Result, now time.Time) (Status, []string) {
	codes := []string{}
	if r.Error != "" || !r.HasCert() {
		codes = append(codes, CodeConnectFailed)
	} else {
		left := r.NotAfter.Sub(now)
		switch {
		case left <= 0:
			codes = append(codes, CodeCertExpired)
		case left < expiresSoonWithin:
			codes = append(codes, CodeCertExpiresSoon)
		case left < r.NotAfter.Sub(r.NotBefore)/3:
			// ACME 클라이언트는 보통 수명이 1/3 남았을 때 갱신하므로, 정상이면 이미 바뀌었어야 한다.
			codes = append(codes, CodeRenewalOverdue)
		}
		if !r.ChainOK {
			codes = append(codes, CodeChainInvalid)
		}
		if !r.HostnameOK {
			codes = append(codes, CodeHostnameMismatch)
		}
		switch r.TLSVersion {
		case "SSL 3.0", "TLS 1.0", "TLS 1.1":
			codes = append(codes, CodeTLSOutdated)
		}
		if (r.KeyAlg == "RSA" && r.KeyBits < minRSABits) || (r.KeyAlg == "ECDSA" && r.KeyBits < minECDSABits) {
			codes = append(codes, CodeWeakKey)
		}
	}
	sortCodes(codes)
	st := StatusOK
	for _, c := range codes {
		st = Worst(st, CodeStatus(c))
	}
	return st, codes
}
```

`internal/monitor/assess.go`:

```go
package monitor

import "ssl-checker/internal/cnamecheck"

// Assessment는 대상이나 도메인의 종합 판정이다.
type Assessment struct {
	Status  Status   `json:"status"`
	Reasons []string `json:"reasons"`
}

func (a *Assessment) add(code string) {
	a.Status = Worst(a.Status, CodeStatus(code))
	a.Reasons = appendUnique(a.Reasons, code)
}

// TXTFailureThreshold번 이상 /update가 연달아 실패하면 TXT_UPDATE_FAILING을 붙인다.
const TXTFailureThreshold = 3

// AssessTarget은 대상의 접속 주소별 최신 결과를 합친다. 인증서를 받은 주소끼리 serial이 다르면
// 일부 서버에 새 인증서가 배포되지 않은 것이다.
func AssessTarget(latest []Result) Assessment {
	if len(latest) == 0 {
		return Assessment{Status: StatusUnknown, Reasons: []string{CodeNotChecked}}
	}
	a := Assessment{Status: StatusOK, Reasons: []string{}}
	serials := map[string]bool{}
	for _, r := range latest {
		a.Status = Worst(a.Status, r.Status)
		a.Reasons = appendUnique(a.Reasons, r.Reasons...)
		if r.Serial != "" {
			serials[r.Serial] = true
		}
	}
	if len(serials) > 1 {
		a.add(CodeSerialMismatch)
	}
	sortCodes(a.Reasons)
	return a
}

// ACMEHealth는 ACME가 켜진 도메인의 위임 상태다.
type ACMEHealth struct {
	CNAMEStatus         cnamecheck.Status
	ConsecutiveFailures int
}

// AssessDomain은 대상 판정과 ACME 상태를 합친다. acme가 nil이면 ACME가 꺼진 도메인이다.
// CNAME 조회 실패(error)는 일시적일 수 있어 경고로 보지 않는다.
func AssessDomain(targets []Assessment, acme *ACMEHealth) Assessment {
	a := Assessment{Status: StatusUnknown, Reasons: []string{}}
	for _, t := range targets {
		a.Status = Worst(a.Status, t.Status)
		a.Reasons = appendUnique(a.Reasons, t.Reasons...)
	}
	if acme != nil {
		switch acme.CNAMEStatus {
		case cnamecheck.StatusMissing:
			a.add(CodeCNAMEMissing)
		case cnamecheck.StatusMismatch:
			a.add(CodeCNAMEMismatch)
		}
		if acme.ConsecutiveFailures >= TXTFailureThreshold {
			a.add(CodeTXTUpdateFailing)
		}
	}
	sortCodes(a.Reasons)
	return a
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/monitor/ -v && go vet ./internal/monitor/`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/monitor
git commit -m "feat: add certificate status rules" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: TLS 점검기와 실행기 (`Prober`, `Runner`)

**Files:**
- Create: `internal/monitor/probe.go`, `internal/monitor/runner.go`
- Test: `internal/monitor/certs_test.go`(도우미), `internal/monitor/probe_test.go`, `internal/monitor/runner_test.go`

**Interfaces:**
- Consumes: `Target`, `Result`, `Evaluate` (Task 3)
- Produces:
  - `const monitor.DefaultTimeout = 10 * time.Second`
  - `type monitor.Prober struct { Roots *x509.CertPool; Timeout time.Duration; Now func() time.Time }`
  - `func (p *Prober) Probe(ctx context.Context, t Target) []Result` (주소마다 하나, 판정까지 채움)
  - `type monitor.Sink interface { SaveResults(ctx context.Context, targetID int64, rs []Result) error }`
  - `const monitor.DefaultConcurrency = 8`
  - `type monitor.Runner struct { Prober *Prober; Sink Sink; Concurrency int; Log *slog.Logger }`
  - `func (r *Runner) Check(ctx context.Context, t Target) ([]Result, error)`
  - `func (r *Runner) CheckAll(ctx context.Context, ts []Target) (failed int)`

- [ ] **Step 1: 테스트 도우미 작성**

`internal/monitor/certs_test.go`:

```go
package monitor

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func newCA(t *testing.T) *testCA {
	t.Helper()
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-200 * 24 * time.Hour), NotAfter: time.Now().Add(10 * 365 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

type leafOpts struct {
	dns       []string
	notBefore time.Time // 0이면 1시간 전
	notAfter  time.Time // 0이면 60일 뒤
	serial    int64     // 0이면 2
}

func (ca *testCA) issue(t *testing.T, o leafOpts) tls.Certificate {
	t.Helper()
	return makeLeaf(t, o, ca.cert, ca.key)
}

// selfSigned는 어떤 CA에도 묶이지 않은 인증서다.
func selfSigned(t *testing.T, dns ...string) tls.Certificate {
	t.Helper()
	return makeLeaf(t, leafOpts{dns: dns}, nil, nil)
}

func makeLeaf(t *testing.T, o leafOpts, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) tls.Certificate {
	t.Helper()
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(60 * 24 * time.Hour)
	}
	if o.serial == 0 {
		o.serial = 2
	}
	key := newKey(t)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(o.serial), Subject: pkix.Name{CommonName: o.dns[0]}, DNSNames: o.dns,
		NotBefore: o.notBefore, NotAfter: o.notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	signer := key
	if parent == nil {
		parent = tmpl
	} else {
		signer = parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, signer)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// serveTLS는 cert로 TLS를 받는 서버를 ln(nil이면 127.0.0.1의 빈 포트)에 띄우고 포트를 돌려준다.
func serveTLS(t *testing.T, cert tls.Certificate, ln net.Listener) int {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.NotFoundHandler())
	if ln != nil {
		srv.Listener.Close()
		srv.Listener = ln
	}
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// hangingPort는 TCP 연결은 받지만 아무 응답도 하지 않는 서버의 포트다.
func hangingPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
		for _, c := range conns {
			c.Close()
		}
	})
	return ln.Addr().(*net.TCPAddr).Port
}

func target(port int, host string, ips ...string) Target {
	tg := Target{ID: 1, Hostname: host, Port: port}
	for _, ip := range ips {
		tg.IPs = append(tg.IPs, netip.MustParseAddr(ip))
	}
	return tg
}
```

- [ ] **Step 2: 실패하는 테스트 작성**

`internal/monitor/probe_test.go`:

```go
package monitor

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"
)

func probeOne(t *testing.T, p *Prober, tg Target) Result {
	t.Helper()
	rs := p.Probe(context.Background(), tg)
	if len(rs) != 1 {
		t.Fatalf("got %d results", len(rs))
	}
	return rs[0]
}

func expectCodes(t *testing.T, r Result, status Status, codes ...string) {
	t.Helper()
	if codes == nil {
		codes = []string{}
	}
	if r.Status != status || !slices.Equal(r.Reasons, codes) {
		t.Fatalf("status %s %v (err=%q), want %s %v", r.Status, r.Reasons, r.Error, status, codes)
	}
}

func TestProbeTrusted(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}, serial: 0xab}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusOK)
	if r.Address != fmt.Sprintf("127.0.0.1:%d", port) || r.Serial != "ab" || r.Issuer != "Test CA" || r.SubjectCN != "a.test" ||
		!slices.Equal(r.SANs, []string{"a.test"}) || r.KeyAlg != "ECDSA" || r.KeyBits != 256 ||
		r.TLSVersion != "TLS 1.3" || r.Cipher == "" || !r.ChainOK || !r.HostnameOK || r.CheckedAt.IsZero() {
		t.Fatalf("result = %+v", r)
	}
}

func TestProbeSelfSigned(t *testing.T) {
	port := serveTLS(t, selfSigned(t, "a.test"), nil)
	r := probeOne(t, &Prober{Roots: newCA(t).pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeChainInvalid)
	if !r.HostnameOK {
		t.Fatal("hostname should still match")
	}
}

func TestProbeHostnameMismatch(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"b.test"}}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeHostnameMismatch)
}

func TestProbeUsesSNI(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"sni.test"}}), nil)
	tg := target(port, "a.test", "127.0.0.1")
	tg.SNI = "sni.test"
	expectCodes(t, probeOne(t, &Prober{Roots: ca.pool}, tg), StatusOK)
}

// 만료된 인증서는 체인 문제로 오판하지 않고 CERT_EXPIRED 하나만 붙어야 한다.
func TestProbeExpired(t *testing.T) {
	ca := newCA(t)
	cert := ca.issue(t, leafOpts{dns: []string{"a.test"}, notBefore: time.Now().Add(-90 * 24 * time.Hour), notAfter: time.Now().Add(-time.Hour)})
	port := serveTLS(t, cert, nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeCertExpired)
	if !r.ChainOK {
		t.Fatal("chain should be judged on trust, not expiry")
	}
}

func TestProbeConnectRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	r := probeOne(t, &Prober{}, target(port, "a.test", "127.0.0.1"))
	expectCodes(t, r, StatusCritical, CodeConnectFailed)
	if r.Error == "" || r.HasCert() {
		t.Fatalf("result = %+v", r)
	}
}

func TestProbeHandshakeTimeout(t *testing.T) {
	port := hangingPort(t)
	start := time.Now()
	r := probeOne(t, &Prober{Timeout: 300 * time.Millisecond}, target(port, "a.test", "127.0.0.1"))
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %s", time.Since(start))
	}
	expectCodes(t, r, StatusCritical, CodeConnectFailed)
}

func TestProbeIPv6(t *testing.T) {
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skip("no IPv6 loopback")
	}
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), ln)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "a.test", "::1"))
	expectCodes(t, r, StatusOK)
	if r.Address != fmt.Sprintf("[::1]:%d", port) {
		t.Fatalf("address = %s", r.Address)
	}
}

// IP 목록이 없으면 호스트명으로 한 번 접속하고, 실제로 붙은 주소를 기록한다.
func TestProbeHostnameMode(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"localhost"}}), nil)
	r := probeOne(t, &Prober{Roots: ca.pool}, target(port, "localhost"))
	expectCodes(t, r, StatusOK)
	if !strings.HasSuffix(r.Address, fmt.Sprintf(":%d", port)) || strings.HasPrefix(r.Address, "localhost") {
		t.Fatalf("address = %s", r.Address)
	}
}

func TestProbeMultipleIPs(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), nil)
	rs := (&Prober{Roots: ca.pool, Timeout: time.Second}).Probe(context.Background(), target(port, "a.test", "127.0.0.1", "127.0.0.2"))
	if len(rs) != 2 || rs[0].Address != fmt.Sprintf("127.0.0.1:%d", port) || rs[1].Address != fmt.Sprintf("127.0.0.2:%d", port) {
		t.Fatalf("results = %+v", rs)
	}
}
```

macOS는 `127.0.0.2`가 기본으로 열려 있지 않다. `TestProbeMultipleIPs`에서는 두 번째 주소가 `CONNECT_FAILED`여도 되니 주소 순서와 개수만 보고, 접속이 늘어지지 않게 `Timeout`을 1초로 둔다.

`internal/monitor/runner_test.go`:

```go
package monitor

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"
)

type memSink struct {
	mu      sync.Mutex
	saved   map[int64][]Result
	failFor int64
}

func (s *memSink) SaveResults(_ context.Context, id int64, rs []Result) error {
	if id == s.failFor {
		return errors.New("db down")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved[id] = rs
	return nil
}

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestRunnerCheckAll(t *testing.T) {
	ca := newCA(t)
	port := serveTLS(t, ca.issue(t, leafOpts{dns: []string{"a.test"}}), nil)
	sink := &memSink{saved: map[int64][]Result{}, failFor: 2}
	r := &Runner{Prober: &Prober{Roots: ca.pool}, Sink: sink, Log: discard}
	t1 := target(port, "a.test", "127.0.0.1")
	t2 := t1
	t2.ID = 2
	if failed := r.CheckAll(context.Background(), []Target{t1, t2}); failed != 1 {
		t.Fatalf("failed = %d", failed)
	}
	if rs := sink.saved[1]; len(rs) != 1 || rs[0].Status != StatusOK {
		t.Fatalf("saved = %+v", sink.saved)
	}

	rs, err := r.Check(context.Background(), t2)
	if err == nil || len(rs) != 1 {
		t.Fatalf("Check = %v, %v", rs, err)
	}
}

func TestRunnerCheckAllRunsConcurrently(t *testing.T) {
	port := hangingPort(t)
	var ts []Target
	for i := range 4 {
		tg := target(port, "a.test", "127.0.0.1")
		tg.ID = int64(i + 1)
		ts = append(ts, tg)
	}
	sink := &memSink{saved: map[int64][]Result{}}
	r := &Runner{Prober: &Prober{Timeout: 300 * time.Millisecond}, Sink: sink, Concurrency: 4, Log: discard}
	start := time.Now()
	r.CheckAll(context.Background(), ts)
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Fatalf("CheckAll took %s; targets did not run concurrently", elapsed)
	}
	if len(sink.saved) != 4 {
		t.Fatalf("saved %d targets", len(sink.saved))
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `go test ./internal/monitor/`
Expected: FAIL (`Prober`, `Runner` 정의 없음)

- [ ] **Step 4: 구현**

`internal/monitor/probe.go`:

```go
package monitor

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"time"
)

const DefaultTimeout = 10 * time.Second

// Prober는 TLS로 접속해 인증서를 받고 따로 검증한다.
type Prober struct {
	Roots   *x509.CertPool   // nil이면 시스템 신뢰 저장소
	Timeout time.Duration    // 주소 하나의 접속+핸드셰이크 상한. 0이면 DefaultTimeout
	Now     func() time.Time // nil이면 time.Now
}

// Probe는 대상의 접속 주소마다 결과 하나를 돌려준다. 판정(Status, Reasons)까지 채운다.
func (p *Prober) Probe(ctx context.Context, t Target) []Result {
	addrs := t.DialAddresses()
	out := make([]Result, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, p.probeAddr(ctx, t, addr))
	}
	return out
}

func (p *Prober) probeAddr(ctx context.Context, t Target, addr string) Result {
	now := time.Now()
	if p.Now != nil {
		now = p.Now()
	}
	r := Result{Address: addr, CheckedAt: now.UTC()}
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	d := tls.Dialer{Config: &tls.Config{
		ServerName: t.ServerName(),
		// 잘못된 인증서도 받아서 보여 줘야 하므로 핸드셰이크에서는 검증하지 않고 아래에서 따로 검증한다.
		InsecureSkipVerify: true, //nolint:gosec
		// 낡은 서버에도 붙어서 TLS_OUTDATED로 표시한다.
		MinVersion: tls.VersionTLS10,
	}}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		r.Error = err.Error()
		r.Status, r.Reasons = Evaluate(r, now)
		return r
	}
	defer conn.Close()
	tc := conn.(*tls.Conn)
	if len(t.IPs) == 0 {
		r.Address = tc.RemoteAddr().String() // 호스트명으로 접속하면 실제로 붙은 주소를 남긴다
	}
	fill(&r, tc.ConnectionState(), t.ServerName(), p.Roots, now)
	r.Status, r.Reasons = Evaluate(r, now)
	return r
}

func fill(r *Result, st tls.ConnectionState, name string, roots *x509.CertPool, now time.Time) {
	r.TLSVersion = tls.VersionName(st.Version)
	r.Cipher = tls.CipherSuiteName(st.CipherSuite)
	if len(st.PeerCertificates) == 0 {
		r.Error = "server sent no certificate"
		return
	}
	leaf := st.PeerCertificates[0]
	r.SubjectCN = leaf.Subject.CommonName
	r.SANs = append([]string{}, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		r.SANs = append(r.SANs, ip.String())
	}
	r.Issuer = leaf.Issuer.CommonName
	if r.Issuer == "" && len(leaf.Issuer.Organization) > 0 {
		r.Issuer = leaf.Issuer.Organization[0]
	}
	r.Serial = leaf.SerialNumber.Text(16)
	r.NotBefore, r.NotAfter = leaf.NotBefore.UTC(), leaf.NotAfter.UTC()
	r.KeyAlg, r.KeyBits = keyInfo(leaf.PublicKey)

	inter := x509.NewCertPool()
	for _, c := range st.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	// 만료는 CERT_EXPIRED로 따로 판정하므로, 체인은 만료 직전 시점 기준으로 신뢰 여부만 본다.
	at := now
	if !leaf.NotAfter.After(now) {
		at = leaf.NotAfter.Add(-time.Minute)
	}
	_, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: inter, CurrentTime: at})
	r.ChainOK = err == nil
	r.HostnameOK = leaf.VerifyHostname(name) == nil
}

func keyInfo(pub any) (string, int) {
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return "RSA", k.N.BitLen()
	case *ecdsa.PublicKey:
		return "ECDSA", k.Curve.Params().BitSize
	case ed25519.PublicKey:
		return "Ed25519", 256
	}
	return "unknown", 0
}
```

`internal/monitor/runner.go`:

```go
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Sink는 점검 결과를 저장한다. store.Store가 구현한다.
type Sink interface {
	SaveResults(ctx context.Context, targetID int64, rs []Result) error
}

const DefaultConcurrency = 8

type Runner struct {
	Prober      *Prober
	Sink        Sink
	Concurrency int // 0이면 DefaultConcurrency
	Log         *slog.Logger
}

// Check는 대상 하나를 점검하고 저장한다. 저장에 실패해도 결과는 돌려준다.
func (r *Runner) Check(ctx context.Context, t Target) ([]Result, error) {
	rs := r.Prober.Probe(ctx, t)
	if err := r.Sink.SaveResults(ctx, t.ID, rs); err != nil {
		return rs, fmt.Errorf("monitor: save target %d: %w", t.ID, err)
	}
	return rs, nil
}

// CheckAll은 대상들을 최대 Concurrency개씩 동시에 점검한다. 저장에 실패한 대상은 로그를 남기고 센다.
func (r *Runner) CheckAll(ctx context.Context, ts []Target) (failed int) {
	n := r.Concurrency
	if n <= 0 {
		n = DefaultConcurrency
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, t := range ts {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Go(func() {
			defer func() { <-sem }()
			if _, err := r.Check(ctx, t); err != nil {
				r.Log.Error("tls check", "target", t.ID, "err", err)
				mu.Lock()
				failed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return failed
}
```

- [ ] **Step 5: 통과 확인**

Run: `go test ./internal/monitor/ -race -v && go vet ./internal/monitor/`
Expected: PASS (`TestProbeIPv6`는 IPv6 loopback이 없으면 SKIP)

- [ ] **Step 6: 커밋**

```bash
git add internal/monitor
git commit -m "feat: add TLS prober and concurrent check runner" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: 저장소 확장 (도메인·대상·결과·ACME 상태)

**Files:**
- Create: `internal/store/monitor.go`, `internal/store/acme_state.go`
- Test: `internal/store/monitor_test.go`, `internal/store/acme_state_test.go`

**Interfaces:**
- Consumes: `monitor.Target`, `monitor.Result`, `monitor.Status` (Task 3), `cnamecheck.Status` (Task 2). 기존 `store.ErrNotFound`, `parsePrefixes`, 테스트 도우미 `newStore`, `create`.
- Produces:
  - `var store.ErrDomainExists, store.ErrTargetExists`
  - `type store.Domain struct { ID int64; FQDN, Memo string; CreatedAt time.Time }`
  - `func (s *Store) CreateDomain(ctx, fqdn, memo string) (Domain, error)`, `ListDomains(ctx) ([]Domain, error)` (fqdn순), `GetDomain(ctx, id int64) (Domain, error)`
  - `type store.Target struct { monitor.Target; DomainID int64; Memo string; CreatedAt time.Time }`
  - `type store.NewTarget struct { DomainID int64; Hostname string; Port int; SNI string; IPs []netip.Addr; Memo string }`
  - `func (s *Store) CreateTarget(ctx, in NewTarget) (Target, error)` (없는 도메인 → `ErrNotFound`, 중복 → `ErrTargetExists`), `ListTargets(ctx) ([]Target, error)`, `TargetsByDomain(ctx, domainID int64) ([]Target, error)`, `GetTarget(ctx, id int64) (Target, error)`
  - `func (s *Store) SaveResults(ctx, targetID int64, rs []monitor.Result) error` (`monitor.Sink` 구현)
  - `func (s *Store) LatestResults(ctx, targets []Target) (map[int64][]monitor.Result, error)` (모든 대상 키가 들어 있고 값은 비어 있어도 non-nil)
  - `func (s *Store) ResultHistory(ctx, targetID int64, limit int) ([]monitor.Result, error)` (최신순)
  - `func (s *Store) PruneResults(ctx, before time.Time) (int64, error)`
  - `type store.ACMEState struct { AccountID, DomainID int64; Domain, Username, Subdomain string; AllowFrom []netip.Prefix; CNAMEStatus cnamecheck.Status; CNAMEValue string; CNAMECheckedAt, LastUpdateAt, LastFailureAt *time.Time; ConsecutiveFailures int }`
  - `func (s *Store) ActiveACMEStates(ctx) ([]ACMEState, error)` (fqdn순, 폐기 계정 제외), `ACMEStateByDomain(ctx, domainID int64) (ACMEState, error)` (없으면 `ErrNotFound`)
  - `func (s *Store) SetCNAMEStatus(ctx, accountID int64, status cnamecheck.Status, value string, at time.Time) error`
  - `func (s *Store) RecordUpdateResult(ctx, accountID int64, ok bool) error`
  - 모든 시각 필드는 UTC로 돌려준다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/store/monitor_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func mustDomain(t *testing.T, st *store.Store, fqdn string) store.Domain {
	t.Helper()
	d, err := st.CreateDomain(context.Background(), fqdn, "")
	if err != nil {
		t.Fatalf("CreateDomain: %v", err)
	}
	return d
}

func mustTarget(t *testing.T, st *store.Store, domainID int64, host string, ips ...string) store.Target {
	t.Helper()
	in := store.NewTarget{DomainID: domainID, Hostname: host, Port: 443}
	for _, ip := range ips {
		in.IPs = append(in.IPs, netip.MustParseAddr(ip))
	}
	tg, err := st.CreateTarget(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateTarget: %v", err)
	}
	return tg
}

func okResult(addr string, at time.Time, serial string) monitor.Result {
	return monitor.Result{
		Address: addr, CheckedAt: at, Status: monitor.StatusOK, Reasons: []string{},
		TLSVersion: "TLS 1.3", Cipher: "TLS_AES_128_GCM_SHA256", SubjectCN: "a.com", SANs: []string{"a.com"},
		Issuer: "R11", Serial: serial, NotBefore: at.Add(-24 * time.Hour), NotAfter: at.Add(60 * 24 * time.Hour),
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true,
	}
}

func save(t *testing.T, st *store.Store, targetID int64, rs ...monitor.Result) {
	t.Helper()
	if err := st.SaveResults(context.Background(), targetID, rs); err != nil {
		t.Fatalf("SaveResults: %v", err)
	}
}

func TestDomains(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	b := mustDomain(t, st, "b.com")
	a, err := st.CreateDomain(ctx, "a.com", "memo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateDomain(ctx, "a.com", ""); !errors.Is(err, store.ErrDomainExists) {
		t.Fatalf("duplicate = %v", err)
	}
	list, err := st.ListDomains(ctx)
	if err != nil || len(list) != 2 || list[0].ID != a.ID || list[1].ID != b.ID || list[0].Memo != "memo" {
		t.Fatalf("ListDomains = %+v, %v", list, err)
	}
	got, err := st.GetDomain(ctx, a.ID)
	if err != nil || got.FQDN != "a.com" || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("GetDomain = %+v, %v", got, err)
	}
	if _, err := st.GetDomain(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestTargets(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	d := mustDomain(t, st, "a.com")
	in := store.NewTarget{DomainID: d.ID, Hostname: "www.a.com", Port: 8443, SNI: "a.com",
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.1"), netip.MustParseAddr("2001:db8::1")}, Memo: "web"}
	tg, err := st.CreateTarget(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.GetTarget(ctx, tg.ID)
	if err != nil || got.Hostname != "www.a.com" || got.Port != 8443 || got.SNI != "a.com" || got.DomainID != d.ID ||
		got.Memo != "web" || !slices.Equal(got.IPs, in.IPs) {
		t.Fatalf("GetTarget = %+v, %v", got, err)
	}
	if addrs := got.DialAddresses(); !slices.Equal(addrs, []string{"10.0.0.1:8443", "[2001:db8::1]:8443"}) {
		t.Fatalf("DialAddresses = %v", addrs)
	}
	if _, err := st.CreateTarget(ctx, in); !errors.Is(err, store.ErrTargetExists) {
		t.Fatalf("duplicate = %v", err)
	}
	if _, err := st.CreateTarget(ctx, store.NewTarget{DomainID: 99999, Hostname: "x", Port: 443}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown domain = %v", err)
	}
	other := mustDomain(t, st, "b.com")
	mustTarget(t, st, other.ID, "b.com")
	byDomain, err := st.TargetsByDomain(ctx, d.ID)
	if err != nil || len(byDomain) != 1 || byDomain[0].ID != tg.ID {
		t.Fatalf("TargetsByDomain = %+v, %v", byDomain, err)
	}
	if all, err := st.ListTargets(ctx); err != nil || len(all) != 2 {
		t.Fatalf("ListTargets = %+v, %v", all, err)
	}
	if _, err := st.GetTarget(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestResultsLatestAndHistory(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1")
	first := okResult("10.0.0.1:443", t0.Add(-2*time.Hour), "01")
	failed := monitor.Result{Address: "10.0.0.1:443", CheckedAt: t0.Add(-time.Hour), Status: monitor.StatusCritical,
		Reasons: []string{monitor.CodeConnectFailed}, Error: "connection refused"}
	save(t, st, tg.ID, first)
	save(t, st, tg.ID, failed)

	latest, err := st.LatestResults(ctx, []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	l := latest[tg.ID]
	if len(l) != 1 || l[0].Error != "connection refused" || l[0].HasCert() || l[0].Status != monitor.StatusCritical ||
		!slices.Equal(l[0].Reasons, []string{monitor.CodeConnectFailed}) || l[0].SANs == nil || len(l[0].SANs) != 0 {
		t.Fatalf("latest = %+v", l)
	}

	hist, err := st.ResultHistory(ctx, tg.ID, 10)
	if err != nil || len(hist) != 2 || hist[0].Status != monitor.StatusCritical {
		t.Fatalf("history = %+v, %v", hist, err)
	}
	h := hist[1]
	if h.Serial != "01" || !h.NotAfter.Equal(first.NotAfter) || !h.CheckedAt.Equal(first.CheckedAt) || h.CheckedAt.Location() != time.UTC ||
		!h.ChainOK || h.KeyBits != 256 || !slices.Equal(h.SANs, []string{"a.com"}) || h.Cipher != first.Cipher {
		t.Fatalf("history[1] = %+v", h)
	}
	if hist, _ := st.ResultHistory(ctx, tg.ID, 1); len(hist) != 1 {
		t.Fatalf("limit ignored: %d", len(hist))
	}
}

// IP 목록에서 뺀 주소의 옛 결과는 최신 결과에 섞이면 안 된다.
func TestLatestResultsDropsRemovedIPs(t *testing.T) {
	st, _ := newStore(t)
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1", "10.0.0.2")
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0, "01"), okResult("10.0.0.2:443", t0, "02"))
	tg.IPs = tg.IPs[:1] // 설정에서 10.0.0.2를 뺐다
	latest, err := st.LatestResults(context.Background(), []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	if l := latest[tg.ID]; len(l) != 1 || l[0].Address != "10.0.0.1:443" {
		t.Fatalf("latest = %+v", l)
	}
}

func TestLatestResultsHostnameMode(t *testing.T) {
	st, _ := newStore(t)
	d := mustDomain(t, st, "a.com")
	tg := mustTarget(t, st, d.ID, "www.a.com")
	empty := mustTarget(t, st, d.ID, "api.a.com")
	save(t, st, tg.ID, okResult("192.0.2.1:443", t0.Add(-time.Hour), "01"))
	save(t, st, tg.ID, okResult("192.0.2.2:443", t0, "02"))
	latest, err := st.LatestResults(context.Background(), []store.Target{tg, empty})
	if err != nil {
		t.Fatal(err)
	}
	if l := latest[tg.ID]; len(l) != 1 || l[0].Serial != "02" {
		t.Fatalf("latest = %+v", l)
	}
	if l, ok := latest[empty.ID]; !ok || l == nil || len(l) != 0 {
		t.Fatalf("unchecked target = %v, %v", l, ok)
	}
}

func TestPruneResults(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	tg := mustTarget(t, st, mustDomain(t, st, "a.com").ID, "www.a.com", "10.0.0.1")
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0.Add(-91*24*time.Hour), "01"))
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0, "02"))
	n, err := st.PruneResults(ctx, t0.Add(-90*24*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("PruneResults = %d, %v", n, err)
	}
	if hist, _ := st.ResultHistory(ctx, tg.ID, 10); len(hist) != 1 || hist[0].Serial != "02" {
		t.Fatalf("history = %+v", hist)
	}
}
```

`internal/store/acme_state_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/store"
)

func TestACMEStates(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	allow := netip.MustParsePrefix("10.0.0.0/24")
	a := create(t, st, "a.com", allow)

	states, err := st.ActiveACMEStates(ctx)
	if err != nil || len(states) != 1 {
		t.Fatalf("ActiveACMEStates = %+v, %v", states, err)
	}
	s := states[0]
	if s.AccountID != a.ID || s.DomainID != a.DomainID || s.Domain != "a.com" || s.Subdomain != a.Subdomain || s.Username != a.Username ||
		s.CNAMEStatus != cnamecheck.StatusUnknown || s.ConsecutiveFailures != 0 || s.LastUpdateAt != nil || s.CNAMECheckedAt != nil ||
		!slices.Equal(s.AllowFrom, []netip.Prefix{allow}) {
		t.Fatalf("state = %+v", s)
	}

	for range 2 {
		if err := st.RecordUpdateResult(ctx, a.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	s, err = st.ACMEStateByDomain(ctx, a.DomainID)
	if err != nil || s.ConsecutiveFailures != 2 || s.LastFailureAt == nil {
		t.Fatalf("after failures = %+v, %v", s, err)
	}
	if err := st.RecordUpdateResult(ctx, a.ID, true); err != nil {
		t.Fatal(err)
	}
	s, _ = st.ACMEStateByDomain(ctx, a.DomainID)
	if s.ConsecutiveFailures != 0 || s.LastUpdateAt == nil {
		t.Fatalf("after success = %+v", s)
	}

	if err := st.SetCNAMEStatus(ctx, a.ID, cnamecheck.StatusMismatch, "x.other.test", t0); err != nil {
		t.Fatal(err)
	}
	s, _ = st.ACMEStateByDomain(ctx, a.DomainID)
	if s.CNAMEStatus != cnamecheck.StatusMismatch || s.CNAMEValue != "x.other.test" || s.CNAMECheckedAt == nil || !s.CNAMECheckedAt.Equal(t0) {
		t.Fatalf("after cname = %+v", s)
	}

	b := mustDomain(t, st, "b.com")
	if _, err := st.ACMEStateByDomain(ctx, b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("no account = %v", err)
	}

	if _, err := pool.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if states, _ := st.ActiveACMEStates(ctx); len(states) != 0 {
		t.Fatalf("revoked account listed: %+v", states)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `make test 2>&1 | grep -A3 store`
Expected: FAIL (정의 없음으로 컴파일 실패)

- [ ] **Step 3: 구현**

`internal/store/monitor.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"ssl-checker/internal/monitor"
)

var (
	ErrDomainExists = errors.New("store: domain already exists")
	ErrTargetExists = errors.New("store: target already exists")
)

const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
)

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

type Domain struct {
	ID        int64
	FQDN      string
	Memo      string
	CreatedAt time.Time
}

func (s *Store) CreateDomain(ctx context.Context, fqdn, memo string) (Domain, error) {
	d := Domain{FQDN: fqdn, Memo: memo}
	err := s.pool.QueryRow(ctx, `INSERT INTO domains (fqdn, memo) VALUES ($1, $2) RETURNING id, created_at`, fqdn, memo).
		Scan(&d.ID, &d.CreatedAt)
	switch {
	case pgCode(err) == codeUniqueViolation:
		return Domain{}, ErrDomainExists
	case err != nil:
		return Domain{}, fmt.Errorf("store: create domain: %w", err)
	}
	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

func scanDomain(row pgx.CollectableRow) (Domain, error) {
	var d Domain
	err := row.Scan(&d.ID, &d.FQDN, &d.Memo, &d.CreatedAt)
	d.CreatedAt = d.CreatedAt.UTC()
	return d, err
}

func (s *Store) ListDomains(ctx context.Context) ([]Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, fqdn, memo, created_at FROM domains ORDER BY fqdn`)
	if err != nil {
		return nil, fmt.Errorf("store: list domains: %w", err)
	}
	ds, err := pgx.CollectRows(rows, scanDomain)
	if err != nil {
		return nil, fmt.Errorf("store: list domains: %w", err)
	}
	return ds, nil
}

func (s *Store) GetDomain(ctx context.Context, id int64) (Domain, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, fqdn, memo, created_at FROM domains WHERE id = $1`, id)
	if err != nil {
		return Domain{}, fmt.Errorf("store: get domain: %w", err)
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDomain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, fmt.Errorf("store: get domain: %w", err)
	}
	return d, nil
}

// Target은 저장된 모니터링 대상이다. 점검에 필요한 값은 monitor.Target에 있다.
type Target struct {
	monitor.Target
	DomainID  int64
	Memo      string
	CreatedAt time.Time
}

type NewTarget struct {
	DomainID int64
	Hostname string
	Port     int
	SNI      string
	IPs      []netip.Addr
	Memo     string
}

// CreateTarget은 대상을 추가한다. 도메인이 없으면 ErrNotFound, 같은 (호스트, 포트, SNI)가 있으면 ErrTargetExists다.
func (s *Store) CreateTarget(ctx context.Context, in NewTarget) (Target, error) {
	t := Target{
		Target:   monitor.Target{Hostname: in.Hostname, Port: in.Port, SNI: in.SNI, IPs: in.IPs},
		DomainID: in.DomainID,
		Memo:     in.Memo,
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO monitor_targets (domain_id, hostname, port, sni, ips, memo)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`,
		in.DomainID, in.Hostname, in.Port, in.SNI, addrStrings(in.IPs), in.Memo).Scan(&t.ID, &t.CreatedAt)
	switch pgCode(err) {
	case codeUniqueViolation:
		return Target{}, ErrTargetExists
	case codeForeignKeyViolation:
		return Target{}, ErrNotFound
	}
	if err != nil {
		return Target{}, fmt.Errorf("store: create target: %w", err)
	}
	t.CreatedAt = t.CreatedAt.UTC()
	return t, nil
}

const targetCols = `id, domain_id, hostname, port, sni, ips, memo, created_at`

func scanTarget(row pgx.CollectableRow) (Target, error) {
	var t Target
	var ips []string
	if err := row.Scan(&t.ID, &t.DomainID, &t.Hostname, &t.Port, &t.SNI, &ips, &t.Memo, &t.CreatedAt); err != nil {
		return Target{}, err
	}
	t.CreatedAt = t.CreatedAt.UTC()
	for _, s := range ips {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return Target{}, fmt.Errorf("target %d ip %q: %w", t.ID, s, err)
		}
		t.IPs = append(t.IPs, a)
	}
	return t, nil
}

func (s *Store) queryTargets(ctx context.Context, query string, args ...any) ([]Target, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: targets: %w", err)
	}
	ts, err := pgx.CollectRows(rows, scanTarget)
	if err != nil {
		return nil, fmt.Errorf("store: targets: %w", err)
	}
	return ts, nil
}

func (s *Store) ListTargets(ctx context.Context) ([]Target, error) {
	return s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets ORDER BY domain_id, hostname, port, id`)
}

func (s *Store) TargetsByDomain(ctx context.Context, domainID int64) ([]Target, error) {
	return s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets WHERE domain_id = $1 ORDER BY hostname, port, id`, domainID)
}

func (s *Store) GetTarget(ctx context.Context, id int64) (Target, error) {
	ts, err := s.queryTargets(ctx, `SELECT `+targetCols+` FROM monitor_targets WHERE id = $1`, id)
	if err != nil {
		return Target{}, err
	}
	if len(ts) == 0 {
		return Target{}, ErrNotFound
	}
	return ts[0], nil
}

const resultCols = `address, checked_at, status, reasons, tls_version, cipher, subject_cn, sans, issuer, serial,
	not_before, not_after, key_alg, key_bits, chain_ok, hostname_ok, error`

// SaveResults는 점검 한 번의 결과를 한 트랜잭션으로 저장한다. monitor.Sink를 구현한다.
func (s *Store) SaveResults(ctx context.Context, targetID int64, rs []monitor.Result) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		for _, r := range rs {
			_, err := tx.Exec(ctx, `INSERT INTO check_results (target_id, `+resultCols+`)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)`,
				targetID, r.Address, r.CheckedAt, string(r.Status), nonNil(r.Reasons), r.TLSVersion, r.Cipher, r.SubjectCN,
				nonNil(r.SANs), r.Issuer, r.Serial, nullTime(r.NotBefore), nullTime(r.NotAfter), r.KeyAlg, r.KeyBits,
				r.ChainOK, r.HostnameOK, r.Error)
			if err != nil {
				return fmt.Errorf("store: save result %s: %w", r.Address, err)
			}
		}
		return nil
	})
}

// scanResult는 resultCols 순서의 행을 읽는다. prefix는 그 앞에 오는 열(예: target_id)의 대상이다.
func scanResult(row pgx.Row, prefix ...any) (monitor.Result, error) {
	var r monitor.Result
	var status string
	var nb, na *time.Time
	dst := append(prefix, &r.Address, &r.CheckedAt, &status, &r.Reasons, &r.TLSVersion, &r.Cipher, &r.SubjectCN, &r.SANs,
		&r.Issuer, &r.Serial, &nb, &na, &r.KeyAlg, &r.KeyBits, &r.ChainOK, &r.HostnameOK, &r.Error)
	if err := row.Scan(dst...); err != nil {
		return monitor.Result{}, err
	}
	r.Status = monitor.Status(status)
	r.CheckedAt = r.CheckedAt.UTC()
	if nb != nil {
		r.NotBefore = nb.UTC()
	}
	if na != nil {
		r.NotAfter = na.UTC()
	}
	r.Reasons, r.SANs = nonNil(r.Reasons), nonNil(r.SANs)
	return r, nil
}

// LatestResults는 대상마다 지금 설정에 맞는 접속 주소별 최신 결과다. 모든 대상의 키가 들어 있다.
func (s *Store) LatestResults(ctx context.Context, targets []Target) (map[int64][]monitor.Result, error) {
	ids := make([]int64, len(targets))
	for i, t := range targets {
		ids[i] = t.ID
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (target_id, address) target_id, `+resultCols+`
		FROM check_results WHERE target_id = ANY($1)
		ORDER BY target_id, address, checked_at DESC, id DESC`, ids)
	if err != nil {
		return nil, fmt.Errorf("store: latest results: %w", err)
	}
	defer rows.Close()
	byTarget := map[int64][]monitor.Result{}
	for rows.Next() {
		var id int64
		r, err := scanResult(rows, &id)
		if err != nil {
			return nil, fmt.Errorf("store: latest results: %w", err)
		}
		byTarget[id] = append(byTarget[id], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: latest results: %w", err)
	}
	out := make(map[int64][]monitor.Result, len(targets))
	for _, t := range targets {
		out[t.ID] = currentResults(t, byTarget[t.ID])
	}
	return out, nil
}

// currentResults는 대상의 지금 설정에 맞는 결과만 남긴다. IP 목록에서 빠진 주소의 옛 결과는 버리고,
// IP 목록이 없으면 호스트명으로 접속한 가장 최근 결과 하나만 쓴다(풀린 주소가 바뀌었을 수 있다).
func currentResults(t Target, rs []monitor.Result) []monitor.Result {
	out := []monitor.Result{}
	if len(t.IPs) == 0 {
		for _, r := range rs {
			if len(out) == 0 || r.CheckedAt.After(out[0].CheckedAt) {
				out = []monitor.Result{r}
			}
		}
		return out
	}
	by := make(map[string]monitor.Result, len(rs))
	for _, r := range rs {
		by[r.Address] = r
	}
	for _, a := range t.DialAddresses() {
		if r, ok := by[a]; ok {
			out = append(out, r)
		}
	}
	return out
}

func (s *Store) ResultHistory(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+resultCols+` FROM check_results WHERE target_id = $1
		ORDER BY checked_at DESC, id DESC LIMIT $2`, targetID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: result history: %w", err)
	}
	defer rows.Close()
	out := []monitor.Result{}
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, fmt.Errorf("store: result history: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PruneResults(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM check_results WHERE checked_at < $1`, before)
	if err != nil {
		return 0, fmt.Errorf("store: prune results: %w", err)
	}
	return tag.RowsAffected(), nil
}

func addrStrings(as []netip.Addr) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.String())
	}
	return out
}

func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
```

`internal/store/acme_state.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"ssl-checker/internal/cnamecheck"
)

// ACMEState는 활성 ACME 계정의 위임 상태다. 키와 키 해시는 담지 않는다.
type ACMEState struct {
	AccountID           int64
	DomainID            int64
	Domain              string
	Username            string
	Subdomain           string
	AllowFrom           []netip.Prefix
	CNAMEStatus         cnamecheck.Status
	CNAMEValue          string
	CNAMECheckedAt      *time.Time
	LastUpdateAt        *time.Time
	LastFailureAt       *time.Time
	ConsecutiveFailures int
}

const acmeStateQuery = `SELECT a.id, a.domain_id, d.fqdn, a.username::text, a.subdomain::text, a.allowfrom,
	a.cname_status, a.cname_value, a.cname_checked_at, a.last_update_at, a.last_failure_at, a.consecutive_failures
	FROM acme_accounts a JOIN domains d ON d.id = a.domain_id
	WHERE a.revoked_at IS NULL`

func scanACMEState(row pgx.CollectableRow) (ACMEState, error) {
	var s ACMEState
	var allow []string
	var cname string
	if err := row.Scan(&s.AccountID, &s.DomainID, &s.Domain, &s.Username, &s.Subdomain, &allow, &cname, &s.CNAMEValue,
		&s.CNAMECheckedAt, &s.LastUpdateAt, &s.LastFailureAt, &s.ConsecutiveFailures); err != nil {
		return ACMEState{}, err
	}
	s.CNAMEStatus = cnamecheck.Status(cname)
	s.CNAMECheckedAt, s.LastUpdateAt, s.LastFailureAt = utcPtr(s.CNAMECheckedAt), utcPtr(s.LastUpdateAt), utcPtr(s.LastFailureAt)
	var err error
	if s.AllowFrom, err = parsePrefixes(allow); err != nil {
		return ACMEState{}, fmt.Errorf("account %d allowfrom: %w", s.AccountID, err)
	}
	return s, nil
}

func (s *Store) ActiveACMEStates(ctx context.Context) ([]ACMEState, error) {
	rows, err := s.pool.Query(ctx, acmeStateQuery+` ORDER BY d.fqdn`)
	if err != nil {
		return nil, fmt.Errorf("store: acme states: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanACMEState)
	if err != nil {
		return nil, fmt.Errorf("store: acme states: %w", err)
	}
	return out, nil
}

// ACMEStateByDomain은 도메인의 활성 계정 상태다. ACME가 꺼져 있으면 ErrNotFound다.
func (s *Store) ACMEStateByDomain(ctx context.Context, domainID int64) (ACMEState, error) {
	rows, err := s.pool.Query(ctx, acmeStateQuery+` AND a.domain_id = $1`, domainID)
	if err != nil {
		return ACMEState{}, fmt.Errorf("store: acme state: %w", err)
	}
	st, err := pgx.CollectExactlyOneRow(rows, scanACMEState)
	if errors.Is(err, pgx.ErrNoRows) {
		return ACMEState{}, ErrNotFound
	}
	if err != nil {
		return ACMEState{}, fmt.Errorf("store: acme state: %w", err)
	}
	return st, nil
}

func (s *Store) SetCNAMEStatus(ctx context.Context, accountID int64, status cnamecheck.Status, value string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE acme_accounts SET cname_status = $2, cname_value = $3, cname_checked_at = $4 WHERE id = $1`,
		accountID, string(status), value, at)
	if err != nil {
		return fmt.Errorf("store: set cname status: %w", err)
	}
	return nil
}

// RecordUpdateResult는 /update 결과를 남긴다. 성공하면 연속 실패 수를 0으로 되돌린다.
func (s *Store) RecordUpdateResult(ctx context.Context, accountID int64, ok bool) error {
	q := `UPDATE acme_accounts SET last_failure_at = now(), consecutive_failures = consecutive_failures + 1 WHERE id = $1`
	if ok {
		q = `UPDATE acme_accounts SET last_update_at = now(), consecutive_failures = 0 WHERE id = $1`
	}
	if _, err := s.pool.Exec(ctx, q, accountID); err != nil {
		return fmt.Errorf("store: record update result: %w", err)
	}
	return nil
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
```

- [ ] **Step 4: 통과 확인**

Run: `make test && go vet ./...`
Expected: 전체 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/store
git commit -m "feat: store domains, targets, check results, and acme state" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: `/update` 결과를 ACME 상태에 기록

**Files:**
- Modify: `internal/acmedns/http.go` (`update`의 `case err != nil:`과 `default:`, 새 메서드 `recordUpdate`)
- Test: `internal/acmedns/http_test.go`

**Interfaces:**
- Consumes: `store.RecordUpdateResult`, `store.ActiveACMEStates` (Task 5). 기존 테스트 도우미 `newFixture`, `f.register`, `updateReq`, `expect`, `txt`, `f.mem.SetFailure`.
- Produces: `/update`가 `update`(성공) 때 `RecordUpdateResult(ok=true)`, `update_failed`(공급자·DB 오류) 때 `RecordUpdateResult(ok=false)`를 부른다. 인증 거부·`bad_txt`·`rate_limited`는 세지 않는다.

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/acmedns/http_test.go` 끝에 추가:

```go
func (f *fixture) acmeState(domain string) store.ACMEState {
	f.t.Helper()
	states, err := store.New(f.pool).ActiveACMEStates(context.Background())
	if err != nil {
		f.t.Fatal(err)
	}
	for _, s := range states {
		if s.Domain == domain {
			return s
		}
	}
	f.t.Fatalf("no acme state for %s", domain)
	return store.ACMEState{}
}

func TestUpdateRecordsACMEState(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	expect(t, f.do(updateReq(reg, txt('a'))), http.StatusOK, map[string]string{"txt": txt('a')})
	if s := f.acmeState("a.com"); s.LastUpdateAt == nil || s.ConsecutiveFailures != 0 || s.LastFailureAt != nil {
		t.Fatalf("after success = %+v", s)
	}

	f.mem.SetFailure(errors.New("cloudflare down"))
	for range 3 {
		expect(t, f.do(updateReq(reg, txt('b'))), http.StatusInternalServerError, map[string]string{"error": "dns_provider_error"})
	}
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 3 || s.LastFailureAt == nil {
		t.Fatalf("after failures = %+v", s)
	}

	f.mem.SetFailure(nil)
	expect(t, f.do(updateReq(reg, txt('b'))), http.StatusOK, map[string]string{"txt": txt('b')})
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 0 {
		t.Fatalf("after recovery = %+v", s)
	}
}

func TestUpdateRejectedValueIsNotAFailure(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	reg := f.register("a.com")
	expect(t, f.do(updateReq(reg, "short")), http.StatusBadRequest, map[string]string{"error": "bad_txt"})
	if s := f.acmeState("a.com"); s.ConsecutiveFailures != 0 || s.LastFailureAt != nil {
		t.Fatalf("bad_txt counted as failure: %+v", s)
	}
}
```

(`TestUpdateBadTXT`가 쓰는 잘못된 값이 `"short"`와 다르면 그 값을 그대로 쓴다.)

- [ ] **Step 2: 실패 확인**

Run: `make test 2>&1 | grep -B2 -A6 RecordsACMEState`
Expected: FAIL (`LastUpdateAt`이 nil)

- [ ] **Step 3: 구현**

`internal/acmedns/http.go`의 `update`에서:

```go
	case err != nil:
		code, detail := "db_error", err.Error()
		var perr *dnsprovider.Error
		if errors.As(err, &perr) {
			code, detail = "dns_provider_error", perr.Kind.String()+": "+err.Error()
		}
		h.Log.Error("update txt", "subdomain", acct.Subdomain, "err", err)
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update_failed", Target: target, SourceIP: ip.String(), Detail: detail})
		h.recordUpdate(ctx, acct.ID, false)
		httpx.WriteError(w, http.StatusInternalServerError, code)
	default:
		h.audit(ctx, store.AuditEntry{Actor: actor, Action: "update", Target: target, SourceIP: ip.String()})
		h.recordUpdate(ctx, acct.ID, true)
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"txt": req.TXT})
```

`audit` 메서드 아래에 추가:

```go
// recordUpdate는 대시보드에 보여 줄 TXT 갱신 상태를 남긴다. 실패해도 응답은 바꾸지 않는다.
func (h *Handler) recordUpdate(ctx context.Context, accountID int64, ok bool) {
	if err := h.Svc.Store.RecordUpdateResult(context.WithoutCancel(ctx), accountID, ok); err != nil {
		h.Log.Error("record update result", "account", accountID, "err", err)
	}
}
```

- [ ] **Step 4: 통과 확인**

Run: `make test`
Expected: 전체 PASS (기존 `/update` 테스트 포함)

- [ ] **Step 5: 커밋**

```bash
git add internal/acmedns
git commit -m "feat: record TXT update success and failure for the dashboard" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: worker 주기 작업과 새 설정

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go`
- Create: `internal/app/monitorcycle.go`, `internal/app/monitorcycle_test.go`
- Modify: `internal/app/worker.go`, `cmd/ssl-checker/main.go`(usage 문구)

**Interfaces:**
- Consumes: `monitor.Runner`, `monitor.Prober`, `monitor.Target` (Task 4), `cnamecheck.Checker`, `cnametest.Serve` (Task 2), `store.ListTargets`, `store.ActiveACMEStates`, `store.SetCNAMEStatus`, `store.PruneResults`, `store.LatestResults`, `store.CreateTarget`, `store.CreateAccount` (Task 5, 기존)
- Produces:
  - `config.Config`의 새 필드 `CheckInterval time.Duration`, `DNSResolver string` (api·worker 모두 읽음)
  - `type app.monitorCycle struct { Store *store.Store; Runner *monitor.Runner; CNAME *cnamecheck.Checker; Zone string; Now func() time.Time; Log *slog.Logger }`, 메서드 `run(ctx)`, `prune(ctx)`
  - `const app.resultRetention = 90 * 24 * time.Hour`

- [ ] **Step 1: 설정 실패 테스트 작성**

`internal/config/config_test.go` 끝에 추가(`"time"` import 추가):

```go
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
		"8.8.8.8:53":         "8.8.8.8:53",
		"9.9.9.9":            "9.9.9.9:53",
		"[2606:4700::1111]":  "[2606:4700::1111]:53",
		"2606:4700::1111":    "[2606:4700::1111]:53",
		"dns.internal:5353":  "dns.internal:5353",
		"  1.0.0.1:53  ":     "1.0.0.1:53",
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
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/config/`
Expected: FAIL (`CheckInterval` 필드 없음)

- [ ] **Step 3: 설정 구현**

`internal/config/config.go`: import에 `"net"`, `"time"` 추가. 상수 블록에 추가:

```go
	defaultCheckInterval = 6 * time.Hour
	minCheckInterval     = 5 * time.Minute
	defaultDNSResolver   = "1.1.1.1:53"
```

`Config`에 필드 추가:

```go
	CheckInterval  time.Duration
	DNSResolver    string
```

`Load`에서 `TXT_TTL` 처리 바로 뒤(`if role == RoleAPI {` 앞)에 추가:

```go
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
```

파일 끝에 추가:

```go
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
```

Run: `go test ./internal/config/` → PASS

- [ ] **Step 4: 주기 작업 실패 테스트 작성**

`internal/app/monitorcycle_test.go`:

```go
package app

import (
	"context"
	"crypto/x509"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func TestMonitorCycleChecksTargetsAndCNAMEs(t *testing.T) {
	st := store.New(dbtest.New(t))
	ctx := context.Background()

	// httptest 인증서는 example.com과 127.0.0.1을 덮는 자체 서명 인증서다. 그 자체를 루트로 믿게 한다.
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())

	sub := uuid.New()
	acct, err := st.CreateAccount(ctx, store.NewAccount{Domain: "a.com", Username: uuid.New(), KeyHash: "h", Subdomain: sub})
	if err != nil {
		t.Fatal(err)
	}
	tg, err := st.CreateTarget(ctx, store.NewTarget{DomainID: acct.DomainID, Hostname: "example.com", Port: port,
		IPs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}})
	if err != nil {
		t.Fatal(err)
	}
	resolver := cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.a.com.": {CNAME: sub.String() + ".acme.test."},
	})

	c := &monitorCycle{
		Store:  st,
		Runner: &monitor.Runner{Prober: &monitor.Prober{Roots: roots}, Sink: st, Log: quiet},
		CNAME:  &cnamecheck.Checker{Resolver: resolver},
		Zone:   "acme.test",
		Log:    quiet,
	}
	c.run(ctx)

	latest, err := st.LatestResults(ctx, []store.Target{tg})
	if err != nil {
		t.Fatal(err)
	}
	if rs := latest[tg.ID]; len(rs) != 1 || !rs[0].ChainOK || !rs[0].HostnameOK || rs[0].Error != "" {
		t.Fatalf("results = %+v", rs)
	}
	s, err := st.ACMEStateByDomain(ctx, acct.DomainID)
	if err != nil || s.CNAMEStatus != cnamecheck.StatusOK || s.CNAMECheckedAt == nil {
		t.Fatalf("acme state = %+v, %v", s, err)
	}
}

func TestMonitorCyclePrune(t *testing.T) {
	st := store.New(dbtest.New(t))
	ctx := context.Background()
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	d, err := st.CreateDomain(ctx, "a.com", "")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := st.CreateTarget(ctx, store.NewTarget{DomainID: d.ID, Hostname: "a.com", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{now.Add(-91 * 24 * time.Hour), now.Add(-time.Hour)} {
		if err := st.SaveResults(ctx, tg.ID, []monitor.Result{{Address: "192.0.2.1:443", CheckedAt: at, Status: monitor.StatusOK}}); err != nil {
			t.Fatal(err)
		}
	}
	c := &monitorCycle{Store: st, Now: func() time.Time { return now }, Log: quiet}
	c.prune(ctx)
	hist, err := st.ResultHistory(ctx, tg.ID, 10)
	if err != nil || len(hist) != 1 {
		t.Fatalf("history after prune = %+v, %v", hist, err)
	}
}
```

- [ ] **Step 5: 실패 확인**

Run: `make test 2>&1 | grep -A3 internal/app`
Expected: FAIL (`monitorCycle` 정의 없음)

- [ ] **Step 6: 주기 작업 구현**

`internal/app/monitorcycle.go`:

```go
package app

import (
	"context"
	"log/slog"
	"time"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

// resultRetention보다 오래된 점검 기록은 하루 한 번 지운다.
const resultRetention = 90 * 24 * time.Hour

// monitorCycle은 worker가 CHECK_INTERVAL마다 돌리는 작업이다.
type monitorCycle struct {
	Store  *store.Store
	Runner *monitor.Runner
	CNAME  *cnamecheck.Checker
	Zone   string
	Now    func() time.Time
	Log    *slog.Logger
}

func (c *monitorCycle) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// run은 모든 대상을 TLS 점검하고, ACME가 켜진 도메인의 CNAME을 확인한다. 한쪽이 실패해도 다른 쪽은 돈다.
func (c *monitorCycle) run(ctx context.Context) {
	targets, err := c.Store.ListTargets(ctx)
	if err != nil {
		c.Log.Error("tls check: list targets", "err", err)
	} else {
		ts := make([]monitor.Target, len(targets))
		for i, t := range targets {
			ts[i] = t.Target
		}
		failed := c.Runner.CheckAll(ctx, ts)
		c.Log.Info("tls check", "targets", len(ts), "save_failed", failed)
	}

	states, err := c.Store.ActiveACMEStates(ctx)
	if err != nil {
		c.Log.Error("cname check: list accounts", "err", err)
		return
	}
	for _, s := range states {
		res := c.CNAME.Check(ctx, s.Domain, s.Subdomain+"."+c.Zone)
		if res.Err != nil {
			c.Log.Warn("cname check", "domain", s.Domain, "err", res.Err)
		}
		if err := c.Store.SetCNAMEStatus(ctx, s.AccountID, res.Status, res.Value, c.now()); err != nil {
			c.Log.Error("cname check: save", "domain", s.Domain, "err", err)
		}
	}
	c.Log.Info("cname check", "domains", len(states))
}

func (c *monitorCycle) prune(ctx context.Context) {
	n, err := c.Store.PruneResults(ctx, c.now().Add(-resultRetention))
	if err != nil {
		c.Log.Error("prune check results", "err", err)
		return
	}
	c.Log.Info("prune check results", "deleted", n)
}
```

`internal/app/worker.go` 전체를 바꾼다:

```go
package app

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"ssl-checker/internal/cleanup"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/config"
	"ssl-checker/internal/db"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

const cleanupEvery = 24 * time.Hour

// Worker는 주기 작업을 돌린다(시작 직후 1회 포함).
//   - CHECK_INTERVAL마다: 모든 대상의 TLS 점검, ACME 도메인의 CNAME 확인
//   - 하루 한 번: 고아 TXT 정리, 90일 지난 점검 기록 삭제
//
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
	st := store.New(pool)
	log := slog.Default()
	cleaner := &cleanup.Cleaner{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL, Log: log}
	cycle := &monitorCycle{
		Store:  st,
		Runner: &monitor.Runner{Prober: &monitor.Prober{}, Sink: st, Log: log},
		CNAME:  &cnamecheck.Checker{Resolver: cfg.DNSResolver},
		Zone:   cfg.ChallengeZone,
		Log:    log,
	}

	daily := func() {
		res, err := cleaner.Run(ctx)
		if err != nil {
			log.Error("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired, "err", err)
		} else {
			log.Info("txt cleanup", "deleted", res.Deleted, "repaired", res.Repaired)
		}
		cycle.prune(ctx)
	}
	var wg sync.WaitGroup
	wg.Go(func() { every(ctx, cleanupEvery, daily) })
	wg.Go(func() { every(ctx, cfg.CheckInterval, func() { cycle.run(ctx) }) })
	wg.Wait()
	return nil
}

// every는 fn을 바로 한 번 실행하고, 이후 interval마다 실행한다. ctx가 끝나면 돌아온다.
// 한 번의 실행이 interval보다 길면 밀린 틱은 하나로 합쳐진다(time.Ticker 동작).
func every(ctx context.Context, interval time.Duration, fn func()) {
	fn()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn()
		}
	}
}
```

`cmd/ssl-checker/main.go`의 usage에서 두 줄을 바꾼다:

```
  serve [--listen :8080]     acme-dns 호환 API와 조회 API(/api)를 연다
  worker                     주기 작업(TLS 점검, CNAME 확인, TXT·기록 정리)을 돌린다
```

- [ ] **Step 7: 통과 확인**

Run: `make test && go vet ./...`
Expected: 전체 PASS

- [ ] **Step 8: 커밋**

```bash
git add internal/config internal/app cmd/ssl-checker
git commit -m "feat: run periodic TLS and CNAME checks in the worker" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: 조회 API (`/api/...`)

**Files:**
- Modify: `internal/config/config.go`, `internal/config/config_test.go` (`PUBLIC_URL`)
- Modify: `internal/httpx/clientip.go`, `internal/httpx/httpx_test.go` (`Proxied`)
- Create: `internal/adminapi/api.go`, `internal/adminapi/view.go`, `internal/adminapi/api_test.go`
- Modify: `internal/app/serve.go`

**Interfaces:**
- Consumes: `store.ListDomains`, `GetDomain`, `ListTargets`, `TargetsByDomain`, `GetTarget`, `LatestResults`, `ResultHistory`, `ActiveACMEStates`, `ACMEStateByDomain`, `SaveResults`, `CreateDomain`, `CreateTarget`, `CreateAccount`, `SetCNAMEStatus` (Task 5), `monitor.AssessTarget`, `AssessDomain`, `ACMEHealth`, `Assessment`, `Status.Valid` (Task 3), `config.CheckInterval` (Task 7)
- Produces:
  - `config.Config.PublicURL string` (api만, `scheme://host[:port]`로 정규화)
  - `func (c httpx.ClientIP) Proxied(r *http.Request) bool`
  - `type adminapi.API struct { Store *store.Store; Zone string; CheckInterval time.Duration; PublicURL string; IP httpx.ClientIP; Now func() time.Time; Log *slog.Logger }`
  - `func (a *API) Register(mux *http.ServeMux)` → `GET /api/summary`, `GET /api/domains`, `GET /api/domains/{id}`, `GET /api/targets/{id}/results`, `GET /api/config`
  - `func (a *API) PublicURLFor(r *http.Request) string` (계획 2의 일회성 자격 증명과 계획 3의 llms.txt가 쓴다)
  - 오류 코드: 400 `invalid_status`, 400 `invalid_limit`, 404 `not_found`, 500 `db_error`

- [ ] **Step 1: 실패하는 테스트 작성 (설정, httpx)**

`internal/config/config_test.go` 끝에 추가:

```go
func TestPublicURL(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"https://ssl-checker.example.com":     "https://ssl-checker.example.com",
		"https://ssl-checker.example.com/":    "https://ssl-checker.example.com",
		"http://10.0.0.5:8080":                "http://10.0.0.5:8080",
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
```

`internal/httpx/httpx_test.go` 끝에 추가:

```go
func TestProxied(t *testing.T) {
	c := httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}}
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.1.2.3:5000"
	if !c.Proxied(r) {
		t.Error("trusted peer not proxied")
	}
	r.RemoteAddr = "192.0.2.1:5000"
	if c.Proxied(r) {
		t.Error("untrusted peer proxied")
	}
	if (httpx.ClientIP{}).Proxied(r) {
		t.Error("no trusted proxies but proxied")
	}
}
```

(`httpx_test.go`에 `net/netip`, `net/http/httptest` import가 없으면 추가한다.)

- [ ] **Step 2: 설정·httpx 구현**

`internal/config/config.go`: import에 `"net/url"` 추가, `Config`에 `PublicURL string` 추가. `if role == RoleAPI {` 블록 안 끝에 추가:

```go
		publicURL, err := parsePublicURL(getenv("PUBLIC_URL"))
		if err != nil {
			errs = append(errs, fmt.Errorf("PUBLIC_URL: %w", err))
		}
		c.PublicURL = publicURL
```

파일 끝에 추가:

```go
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
```

`internal/httpx/clientip.go`의 `trusted` 메서드 위에 추가:

```go
// Proxied는 요청이 신뢰하는 프록시에서 직접 왔는지 알려 준다. 그때만 X-Forwarded-* 헤더를 믿는다.
func (c ClientIP) Proxied(r *http.Request) bool {
	a := parseRemote(r.RemoteAddr)
	return a.IsValid() && c.trusted(a)
}
```

Run: `go test ./internal/config/ ./internal/httpx/` → PASS

- [ ] **Step 3: 조회 API 실패 테스트 작성**

`internal/adminapi/api_test.go`:

```go
package adminapi_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"ssl-checker/internal/adminapi"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

var now = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

type fixture struct {
	t   *testing.T
	st  *store.Store
	api *adminapi.API
	h   http.Handler
}

func newFixture(t *testing.T) *fixture {
	st := store.New(dbtest.New(t))
	api := &adminapi.API{
		Store: st, Zone: "acme.test", CheckInterval: 6 * time.Hour,
		IP:  httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		Now: func() time.Time { return now }, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &fixture{t: t, st: st, api: api, h: mux}
}

func (f *fixture) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func (f *fixture) getJSON(path string, v any) {
	f.t.Helper()
	rec := f.get(path)
	if rec.Code != http.StatusOK {
		f.t.Fatalf("GET %s = %d %s", path, rec.Code, rec.Body)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		f.t.Fatalf("GET %s: %v", path, err)
	}
}

func expectError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), `"error":"`+code+`"`) {
		t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, status, code)
	}
}

func result(addr string, notAfter time.Time, status monitor.Status, codes ...string) monitor.Result {
	return monitor.Result{
		Address: addr, CheckedAt: now.Add(-time.Hour), Status: status, Reasons: append([]string{}, codes...),
		TLSVersion: "TLS 1.3", Issuer: "R11", Serial: "0a", NotBefore: notAfter.Add(-90 * day), NotAfter: notAfter,
		KeyAlg: "ECDSA", KeyBits: 256, ChainOK: true, HostnameOK: true, SANs: []string{},
	}
}

type seeded struct {
	a, b, c      store.Domain
	aTarget      store.Target
	cSubdomain   string
}

// seed: a.com 정상(60일 남음), b.com 긴급(3일 남음), c.com 대상 없음 + ACME CNAME 없음.
func (f *fixture) seed() seeded {
	f.t.Helper()
	ctx := context.Background()
	var s seeded
	var err error
	must := func(err error) {
		f.t.Helper()
		if err != nil {
			f.t.Fatal(err)
		}
	}
	s.a, err = f.st.CreateDomain(ctx, "a.com", "main")
	must(err)
	s.b, err = f.st.CreateDomain(ctx, "b.com", "")
	must(err)
	s.aTarget, err = f.st.CreateTarget(ctx, store.NewTarget{DomainID: s.a.ID, Hostname: "www.a.com", Port: 443,
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.1")}})
	must(err)
	bTarget, err := f.st.CreateTarget(ctx, store.NewTarget{DomainID: s.b.ID, Hostname: "b.com", Port: 443,
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.2")}})
	must(err)
	must(f.st.SaveResults(ctx, s.aTarget.ID, []monitor.Result{result("10.0.0.1:443", now.Add(60*day), monitor.StatusOK)}))
	must(f.st.SaveResults(ctx, bTarget.ID, []monitor.Result{result("10.0.0.2:443", now.Add(3*day), monitor.StatusCritical, monitor.CodeCertExpiresSoon)}))

	sub := uuid.New()
	acct, err := f.st.CreateAccount(ctx, store.NewAccount{Domain: "c.com", Username: uuid.New(), KeyHash: "secret-hash", Subdomain: sub,
		AllowFrom: []netip.Prefix{netip.MustParsePrefix("10.0.1.0/24")}})
	must(err)
	must(f.st.SetCNAMEStatus(ctx, acct.ID, cnamecheck.StatusMissing, "", now))
	s.c, err = f.st.GetDomain(ctx, acct.DomainID)
	must(err)
	s.cSubdomain = sub.String()
	return s
}

func TestSummary(t *testing.T) {
	f := newFixture(t)
	f.seed()
	var got map[string]any
	f.getJSON("/api/summary", &got)
	want := map[string]float64{"domains": 3, "critical": 1, "warning": 1, "info": 0, "ok": 1, "unknown": 0}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
	if got["checked_at"] != now.Add(-time.Hour).Format(time.RFC3339) {
		t.Errorf("checked_at = %v", got["checked_at"])
	}
}

type domainItem struct {
	ID               int64      `json:"id"`
	FQDN             string     `json:"fqdn"`
	Memo             string     `json:"memo"`
	Status           string     `json:"status"`
	Reasons          []string   `json:"reasons"`
	EarliestNotAfter *time.Time `json:"earliest_not_after"`
	DaysLeft         *int       `json:"days_left"`
	Targets          int        `json:"targets"`
	ACME             struct {
		Enabled     bool   `json:"enabled"`
		CNAMEStatus string `json:"cname_status"`
	} `json:"acme"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
}

func TestListDomains(t *testing.T) {
	f := newFixture(t)
	s := f.seed()
	var resp struct{ Domains []domainItem }
	f.getJSON("/api/domains", &resp)
	if len(resp.Domains) != 3 {
		t.Fatalf("domains = %+v", resp.Domains)
	}
	a, b, c := resp.Domains[0], resp.Domains[1], resp.Domains[2]
	if a.ID != s.a.ID || a.Status != "ok" || a.Memo != "main" || a.DaysLeft == nil || *a.DaysLeft != 60 || a.Targets != 1 || a.ACME.Enabled || a.LastCheckedAt == nil {
		t.Errorf("a.com = %+v", a)
	}
	if b.Status != "critical" || len(b.Reasons) != 1 || b.Reasons[0] != monitor.CodeCertExpiresSoon || b.DaysLeft == nil || *b.DaysLeft != 3 ||
		b.EarliestNotAfter == nil || !b.EarliestNotAfter.Equal(now.Add(3*day)) {
		t.Errorf("b.com = %+v", b)
	}
	if c.Status != "warning" || c.Reasons[0] != monitor.CodeCNAMEMissing || !c.ACME.Enabled || c.ACME.CNAMEStatus != "missing" ||
		c.Targets != 0 || c.DaysLeft != nil || c.EarliestNotAfter != nil || c.LastCheckedAt != nil {
		t.Errorf("c.com = %+v", c)
	}

	f.getJSON("/api/domains?status=critical", &resp)
	if len(resp.Domains) != 1 || resp.Domains[0].FQDN != "b.com" {
		t.Errorf("status filter = %+v", resp.Domains)
	}
	expectError(t, f.get("/api/domains?status=bogus"), http.StatusBadRequest, "invalid_status")
}

func TestListDomainsEmpty(t *testing.T) {
	f := newFixture(t)
	rec := f.get("/api/domains")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"domains":[]}` {
		t.Fatalf("empty list = %d %s", rec.Code, rec.Body)
	}
}

func TestGetDomain(t *testing.T) {
	f := newFixture(t)
	s := f.seed()

	var a struct {
		FQDN    string `json:"fqdn"`
		Status  string `json:"status"`
		Targets []struct {
			ID       int64    `json:"id"`
			Hostname string   `json:"hostname"`
			Port     int      `json:"port"`
			IPs      []string `json:"ips"`
			Status   string   `json:"status"`
			Results  []struct {
				Address  string    `json:"address"`
				NotAfter time.Time `json:"not_after"`
			} `json:"results"`
		} `json:"targets"`
		ACME map[string]any `json:"acme"`
	}
	f.getJSON("/api/domains/"+strconv.FormatInt(s.a.ID, 10), &a)
	if a.FQDN != "a.com" || a.Status != "ok" || len(a.Targets) != 1 {
		t.Fatalf("a.com = %+v", a)
	}
	tg := a.Targets[0]
	if tg.ID != s.aTarget.ID || tg.Hostname != "www.a.com" || tg.Port != 443 || len(tg.IPs) != 1 || tg.IPs[0] != "10.0.0.1" ||
		tg.Status != "ok" || len(tg.Results) != 1 || tg.Results[0].Address != "10.0.0.1:443" {
		t.Fatalf("target = %+v", tg)
	}
	if len(a.ACME) != 1 || a.ACME["enabled"] != false {
		t.Fatalf("acme off = %v", a.ACME)
	}

	rec := f.get("/api/domains/" + strconv.FormatInt(s.c.ID, 10))
	body := rec.Body.String()
	if strings.Contains(body, "secret-hash") || strings.Contains(body, "password") || strings.Contains(body, "key_hash") {
		t.Fatalf("secret leaked: %s", body)
	}
	var c struct {
		Status string `json:"status"`
		Targets []any `json:"targets"`
		ACME   struct {
			Enabled             bool     `json:"enabled"`
			Subdomain           string   `json:"subdomain"`
			FullDomain          string   `json:"fulldomain"`
			CNAMERecord         string   `json:"cname_record"`
			AllowFrom           []string `json:"allowfrom"`
			CNAMEStatus         string   `json:"cname_status"`
			ConsecutiveFailures *int     `json:"consecutive_failures"`
		} `json:"acme"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	full := s.cSubdomain + ".acme.test"
	if c.Status != "warning" || c.Targets == nil || len(c.Targets) != 0 || !c.ACME.Enabled || c.ACME.Subdomain != s.cSubdomain ||
		c.ACME.FullDomain != full || c.ACME.CNAMERecord != "_acme-challenge.c.com. CNAME "+full+"." ||
		len(c.ACME.AllowFrom) != 1 || c.ACME.AllowFrom[0] != "10.0.1.0/24" || c.ACME.CNAMEStatus != "missing" ||
		c.ACME.ConsecutiveFailures == nil || *c.ACME.ConsecutiveFailures != 0 {
		t.Fatalf("c.com = %+v", c)
	}

	expectError(t, f.get("/api/domains/99999"), http.StatusNotFound, "not_found")
	expectError(t, f.get("/api/domains/abc"), http.StatusNotFound, "not_found")
}

func TestTargetResults(t *testing.T) {
	f := newFixture(t)
	s := f.seed()
	ctx := context.Background()
	for i := range 2 {
		r := result("10.0.0.1:443", now.Add(60*day), monitor.StatusOK)
		r.CheckedAt = now.Add(time.Duration(i+1) * time.Minute)
		if err := f.st.SaveResults(ctx, s.aTarget.ID, []monitor.Result{r}); err != nil {
			t.Fatal(err)
		}
	}
	path := "/api/targets/" + strconv.FormatInt(s.aTarget.ID, 10) + "/results"
	var resp struct {
		Results []struct {
			CheckedAt time.Time `json:"checked_at"`
		} `json:"results"`
	}
	f.getJSON(path, &resp)
	if len(resp.Results) != 3 || !resp.Results[0].CheckedAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("results = %+v", resp.Results)
	}
	f.getJSON(path+"?limit=2", &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("limit=2 gave %d", len(resp.Results))
	}
	f.getJSON(path+"?limit=100000", &resp) // 500으로 줄인다
	expectError(t, f.get(path+"?limit=0"), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.get(path+"?limit=x"), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.get("/api/targets/99999/results"), http.StatusNotFound, "not_found")
}

func TestConfig(t *testing.T) {
	f := newFixture(t)
	cfg := func(r *http.Request) map[string]string {
		t.Helper()
		rec := httptest.NewRecorder()
		f.h.ServeHTTP(rec, r)
		var m map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
		return m
	}
	proxied := func() *http.Request {
		r := httptest.NewRequest("GET", "/api/config", nil)
		r.RemoteAddr = "10.0.0.5:4000"
		r.Header.Set("X-Forwarded-Proto", "https")
		r.Header.Set("X-Forwarded-Host", "ssl.example.com")
		return r
	}

	m := cfg(proxied())
	if m["challenge_zone"] != "acme.test" || m["check_interval"] != "6h0m0s" || m["public_url"] != "https://ssl.example.com" {
		t.Fatalf("proxied config = %v", m)
	}
	direct := proxied()
	direct.RemoteAddr = "192.0.2.9:4000" // 신뢰하지 않는 곳에서 온 X-Forwarded-*는 무시한다
	if m := cfg(direct); m["public_url"] != "http://example.com" {
		t.Fatalf("untrusted forwarded headers used: %v", m)
	}
	f.api.PublicURL = "https://fixed.example.com"
	if m := cfg(proxied()); m["public_url"] != "https://fixed.example.com" {
		t.Fatalf("PUBLIC_URL ignored: %v", m)
	}
}
```

- [ ] **Step 4: 실패 확인**

Run: `make test 2>&1 | grep -A3 adminapi`
Expected: FAIL (패키지 `adminapi` 없음)

- [ ] **Step 5: 조회 API 구현**

`internal/adminapi/view.go`:

```go
package adminapi

import (
	"math"
	"time"

	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

type acmeSummary struct {
	Enabled     bool              `json:"enabled"`
	CNAMEStatus cnamecheck.Status `json:"cname_status,omitempty"`
}

type domainSummary struct {
	ID   int64  `json:"id"`
	FQDN string `json:"fqdn"`
	Memo string `json:"memo"`
	monitor.Assessment
	EarliestNotAfter *time.Time  `json:"earliest_not_after"`
	DaysLeft         *int        `json:"days_left"`
	Targets          int         `json:"targets"`
	ACME             acmeSummary `json:"acme"`
	LastCheckedAt    *time.Time  `json:"last_checked_at"`
}

type targetView struct {
	ID       int64    `json:"id"`
	Hostname string   `json:"hostname"`
	Port     int      `json:"port"`
	SNI      string   `json:"sni"`
	IPs      []string `json:"ips"`
	Memo     string   `json:"memo"`
	monitor.Assessment
	Results []monitor.Result `json:"results"`
}

type acmeDetail struct {
	Enabled             bool              `json:"enabled"`
	Username            string            `json:"username"`
	Subdomain           string            `json:"subdomain"`
	FullDomain          string            `json:"fulldomain"`
	CNAMERecord         string            `json:"cname_record"`
	AllowFrom           []string          `json:"allowfrom"`
	CNAMEStatus         cnamecheck.Status `json:"cname_status"`
	CNAMEValue          string            `json:"cname_value"`
	CNAMECheckedAt      *time.Time        `json:"cname_checked_at"`
	LastUpdateAt        *time.Time        `json:"last_update_at"`
	LastFailureAt       *time.Time        `json:"last_failure_at"`
	ConsecutiveFailures int               `json:"consecutive_failures"`
}

type domainDetail struct {
	ID        int64     `json:"id"`
	FQDN      string    `json:"fqdn"`
	Memo      string    `json:"memo"`
	CreatedAt time.Time `json:"created_at"`
	monitor.Assessment
	Targets []targetView `json:"targets"`
	ACME    any          `json:"acme"` // 꺼져 있으면 {"enabled":false}만
}

// assess는 도메인의 대상별 판정과 도메인 판정을 함께 계산한다. acme가 nil이면 ACME가 꺼져 있다.
func assess(targets []store.Target, latest map[int64][]monitor.Result, acme *store.ACMEState) (monitor.Assessment, []monitor.Assessment) {
	per := make([]monitor.Assessment, len(targets))
	for i, t := range targets {
		per[i] = monitor.AssessTarget(latest[t.ID])
	}
	var h *monitor.ACMEHealth
	if acme != nil {
		h = &monitor.ACMEHealth{CNAMEStatus: acme.CNAMEStatus, ConsecutiveFailures: acme.ConsecutiveFailures}
	}
	return monitor.AssessDomain(per, h), per
}

func summarize(d store.Domain, targets []store.Target, latest map[int64][]monitor.Result, acme *store.ACMEState, now time.Time) domainSummary {
	overall, _ := assess(targets, latest, acme)
	out := domainSummary{ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, Assessment: overall, Targets: len(targets), ACME: acmeSummary{Enabled: acme != nil}}
	if acme != nil {
		out.ACME.CNAMEStatus = acme.CNAMEStatus
	}
	for _, t := range targets {
		for _, r := range latest[t.ID] {
			if r.HasCert() && (out.EarliestNotAfter == nil || r.NotAfter.Before(*out.EarliestNotAfter)) {
				na := r.NotAfter
				out.EarliestNotAfter = &na
			}
			if out.LastCheckedAt == nil || r.CheckedAt.After(*out.LastCheckedAt) {
				c := r.CheckedAt
				out.LastCheckedAt = &c
			}
		}
	}
	if out.EarliestNotAfter != nil {
		days := int(math.Floor(out.EarliestNotAfter.Sub(now).Hours() / 24))
		out.DaysLeft = &days
	}
	return out
}

func (a *API) acmeView(fqdn string, st *store.ACMEState) any {
	if st == nil {
		return map[string]bool{"enabled": false}
	}
	full := st.Subdomain + "." + a.Zone
	allow := make([]string, 0, len(st.AllowFrom))
	for _, p := range st.AllowFrom {
		allow = append(allow, p.String())
	}
	return acmeDetail{
		Enabled: true, Username: st.Username, Subdomain: st.Subdomain, FullDomain: full,
		CNAMERecord: cnamecheck.ChallengeName(fqdn) + ". CNAME " + full + ".",
		AllowFrom:   allow, CNAMEStatus: st.CNAMEStatus, CNAMEValue: st.CNAMEValue, CNAMECheckedAt: st.CNAMECheckedAt,
		LastUpdateAt: st.LastUpdateAt, LastFailureAt: st.LastFailureAt, ConsecutiveFailures: st.ConsecutiveFailures,
	}
}

func targetViews(targets []store.Target, latest map[int64][]monitor.Result, per []monitor.Assessment) []targetView {
	out := make([]targetView, len(targets))
	for i, t := range targets {
		ips := make([]string, 0, len(t.IPs))
		for _, ip := range t.IPs {
			ips = append(ips, ip.String())
		}
		rs := latest[t.ID]
		if rs == nil {
			rs = []monitor.Result{}
		}
		out[i] = targetView{ID: t.ID, Hostname: t.Hostname, Port: t.Port, SNI: t.SNI, IPs: ips, Memo: t.Memo, Assessment: per[i], Results: rs}
	}
	return out
}
```

`internal/adminapi/api.go`:

```go
// Package adminapi는 대시보드와 에이전트가 쓰는 /api JSON API다. 이 계획에서는 조회만 연다.
package adminapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/store"
)

const (
	defaultResultLimit = 50
	maxResultLimit     = 500
)

type API struct {
	Store         *store.Store
	Zone          string        // CHALLENGE_ZONE
	CheckInterval time.Duration // 화면에 보여 주기만 한다. 점검은 worker가 한다
	PublicURL     string        // 비우면 요청에서 만든다
	IP            httpx.ClientIP
	Now           func() time.Time
	Log           *slog.Logger
}

// Register는 조회 경로를 mux에 붙인다. 조회는 인증 없이 누구나 할 수 있다.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/summary", a.summary)
	mux.HandleFunc("GET /api/domains", a.listDomains)
	mux.HandleFunc("GET /api/domains/{id}", a.getDomain)
	mux.HandleFunc("GET /api/targets/{id}/results", a.targetResults)
	mux.HandleFunc("GET /api/config", a.config)
}

func (a *API) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

func (a *API) dbError(w http.ResponseWriter, op string, err error) {
	a.Log.Error(op, "err", err)
	httpx.WriteError(w, http.StatusInternalServerError, "db_error")
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

// allDomains는 목록·요약에 쓰는 도메인별 요약이다. 도메인 수가 적어 매번 전체를 계산한다.
func (a *API) allDomains(ctx context.Context) ([]domainSummary, error) {
	domains, err := a.Store.ListDomains(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := a.Store.ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		return nil, err
	}
	states, err := a.Store.ActiveACMEStates(ctx)
	if err != nil {
		return nil, err
	}
	byDomain := map[int64][]store.Target{}
	for _, t := range targets {
		byDomain[t.DomainID] = append(byDomain[t.DomainID], t)
	}
	acme := map[int64]*store.ACMEState{}
	for i := range states {
		acme[states[i].DomainID] = &states[i]
	}
	now := a.now()
	out := make([]domainSummary, 0, len(domains))
	for _, d := range domains {
		out = append(out, summarize(d, byDomain[d.ID], latest, acme[d.ID], now))
	}
	return out, nil
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	ds, err := a.allDomains(r.Context())
	if err != nil {
		a.dbError(w, "summary", err)
		return
	}
	counts := map[monitor.Status]int{}
	var checked *time.Time
	for _, d := range ds {
		counts[d.Status]++
		if d.LastCheckedAt != nil && (checked == nil || d.LastCheckedAt.After(*checked)) {
			checked = d.LastCheckedAt
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"domains": len(ds), "critical": counts[monitor.StatusCritical], "warning": counts[monitor.StatusWarning],
		"info": counts[monitor.StatusInfo], "ok": counts[monitor.StatusOK], "unknown": counts[monitor.StatusUnknown],
		"checked_at": checked,
	})
}

func (a *API) listDomains(w http.ResponseWriter, r *http.Request) {
	filter := monitor.Status(r.URL.Query().Get("status"))
	if filter != "" && !filter.Valid() {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_status")
		return
	}
	ds, err := a.allDomains(r.Context())
	if err != nil {
		a.dbError(w, "list domains", err)
		return
	}
	out := []domainSummary{}
	for _, d := range ds {
		if filter == "" || d.Status == filter {
			out = append(out, d)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"domains": out})
}

func (a *API) getDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	d, err := a.Store.GetDomain(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return
	}
	targets, err := a.Store.TargetsByDomain(ctx, id)
	if err != nil {
		a.dbError(w, "get domain targets", err)
		return
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		a.dbError(w, "get domain results", err)
		return
	}
	var acme *store.ACMEState
	switch st, err := a.Store.ACMEStateByDomain(ctx, id); {
	case err == nil:
		acme = &st
	case !errors.Is(err, store.ErrNotFound):
		a.dbError(w, "get domain acme", err)
		return
	}
	overall, per := assess(targets, latest, acme)
	httpx.WriteJSON(w, http.StatusOK, domainDetail{
		ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, CreatedAt: d.CreatedAt, Assessment: overall,
		Targets: targetViews(targets, latest, per), ACME: a.acmeView(d.FQDN, acme),
	})
}

func (a *API) targetResults(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	limit := defaultResultLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
			return
		}
		limit = min(n, maxResultLimit)
	}
	ctx := r.Context()
	if _, err := a.Store.GetTarget(ctx, id); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	} else if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	rs, err := a.Store.ResultHistory(ctx, id, limit)
	if err != nil {
		a.dbError(w, "target results", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": rs})
}

func (a *API) config(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"challenge_zone": a.Zone,
		"check_interval": a.CheckInterval.String(),
		"public_url":     a.PublicURLFor(r),
	})
}

// PublicURLFor는 클라이언트가 api에 닿는 주소다. PUBLIC_URL이 있으면 그 값, 없으면 요청에서 만든다.
// X-Forwarded-Proto/Host는 신뢰하는 프록시에서 직접 온 요청일 때만 믿는다.
func (a *API) PublicURLFor(r *http.Request) string {
	if a.PublicURL != "" {
		return a.PublicURL
	}
	scheme, host := "http", r.Host
	if r.TLS != nil {
		scheme = "https"
	}
	if a.IP.Proxied(r) {
		if p := firstValue(r.Header.Get("X-Forwarded-Proto")); p == "http" || p == "https" {
			scheme = p
		}
		if h := firstValue(r.Header.Get("X-Forwarded-Host")); h != "" {
			host = h
		}
	}
	return scheme + "://" + host
}

func firstValue(s string) string {
	v, _, _ := strings.Cut(s, ",")
	return strings.TrimSpace(v)
}
```

- [ ] **Step 6: serve 연결**

`internal/app/serve.go`: import에 `"ssl-checker/internal/adminapi"` 추가. `h := &acmedns.Handler{...}`를 만드는 부분과 `srv`를 아래로 바꾼다:

```go
	ip := httpx.ClientIP{Trusted: cfg.TrustedProxies}
	st := store.New(pool)
	h := &acmedns.Handler{
		Svc:           &acmedns.Service{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL},
		IP:            ip,
		Admin:         httpx.NewAdminAuth(cfg.AdminKey, ip),
		UpdateLimit:   httpx.NewKeyed(updateEvery, updateBurst),
		AuthFailLimit: httpx.NewKeyed(authFailEvery, authFailBurst),
		Health:        &acmedns.HealthChecker{DB: pool, Provider: provider, CacheFor: time.Minute},
		Log:           slog.Default(),
	}
	api := &adminapi.API{Store: st, Zone: cfg.ChallengeZone, CheckInterval: cfg.CheckInterval, PublicURL: cfg.PublicURL, IP: ip, Log: slog.Default()}

	// acme-dns 경로는 기존 핸들러에 그대로 넘기고, /api는 같은 mux에 붙인다.
	mux := http.NewServeMux()
	legacy := h.Routes()
	mux.Handle("POST /update", legacy)
	mux.Handle("POST /register", legacy)
	mux.Handle("GET /health", legacy)
	api.Register(mux)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           mux,
```

(`srv`의 나머지 필드는 그대로 둔다.)

- [ ] **Step 7: 통과 확인**

Run: `gofmt -l internal/ && make test && go vet ./...`
Expected: gofmt 출력 없음, 전체 PASS

- [ ] **Step 8: 커밋**

```bash
git add internal/config internal/httpx internal/adminapi internal/app
git commit -m "feat: add public read API for domains and check results" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: 배포 설정, smoke, 문서

**Files:**
- Modify: `deploy/compose.yaml`, `deploy/production.env.example`, `deploy/komodo/ssl-checker.toml`
- Modify: `scripts/smoke.sh`
- Modify: `docs/guide/configuration.md`, `docs/guide/api.md`

**Interfaces:**
- Consumes: 환경변수 `CHECK_INTERVAL`, `DNS_RESOLVER`, `PUBLIC_URL` (Task 7, 8), `/api/summary`, `/api/domains`, `/api/config` (Task 8)

- [ ] **Step 1: smoke 확장 (실패 확인용)**

`scripts/smoke.sh`에서 `code=$(curl ... wrong ...)` 검사 블록 뒤, worker 검사 앞에 추가:

```sh
summary=$(curl -fsS "$BASE/api/summary")
echo "$summary" | grep -q '"domains":1' || { echo "unexpected summary: $summary" >&2; exit 1; }
domains=$(curl -fsS "$BASE/api/domains")
echo "$domains" | grep -q '"fqdn":"smoke.example"' || { echo "unexpected domains: $domains" >&2; exit 1; }
echo "$domains" | grep -q '"enabled":true' || { echo "acme not shown as enabled: $domains" >&2; exit 1; }
apicfg=$(curl -fsS "$BASE/api/config")
echo "$apicfg" | grep -q "\"challenge_zone\":\"$CHALLENGE_ZONE\"" || { echo "unexpected config: $apicfg" >&2; exit 1; }
```

- [ ] **Step 2: compose와 환경 파일**

`deploy/compose.yaml`의 `x-common-env`에 두 줄 추가(`TXT_TTL` 아래):

```yaml
  CHECK_INTERVAL: ${CHECK_INTERVAL:-6h}
  DNS_RESOLVER: ${DNS_RESOLVER:-1.1.1.1:53}
```

`worker`의 `environment`는 `<<: *common-env`만 남는다(이미 그렇다). `api`의 `environment`에 추가:

```yaml
      PUBLIC_URL: ${PUBLIC_URL:-}
```

`deploy/production.env.example`의 `TRUSTED_PROXIES=` 블록 뒤에 추가:

```dotenv
# 클라이언트가 api에 닿는 외부 주소. 화면·자격 증명 안내에 쓴다. 비우면 요청에서 만든다.
# 예: https://ssl-checker.example.com
PUBLIC_URL=

# 선택. 인증서 점검·CNAME 확인 주기(최소 5m)와 CNAME 확인에 쓸 공개 리졸버
CHECK_INTERVAL=6h
DNS_RESOLVER=1.1.1.1:53
```

`deploy/komodo/ssl-checker.toml`의 `environment`에 `TRUSTED_PROXIES` 줄 아래 추가:

```
PUBLIC_URL=https://ssl-checker.example.com
```

`description`을 `"인증서 모니터링 + DNS-01 검증 대행 (acme-dns 호환 API)"`로 바꾼다.

- [ ] **Step 3: smoke 실행**

Run: `./scripts/smoke.sh 2>&1 | tail -3`
Expected: `smoke OK`

- [ ] **Step 4: 문서**

`docs/guide/configuration.md`의 환경변수 표(`## 환경변수`)에 세 줄 추가(다른 행과 같은 5열 형식, `TXT_TTL` 행 아래):

```markdown
| `CHECK_INTERVAL` | 인증서 점검과 ACME CNAME 확인 주기. Go duration 형식이고 최소 `5m`입니다. api는 `/api/config`에 보여 주기만 합니다. | api, worker | `6h` | 선택 |
| `DNS_RESOLVER` | ACME CNAME 확인에 쓸 리졸버 `host:port`. Let's Encrypt가 보는 공개 DNS 기준으로 확인하려고 사내 DNS 대신 공개 리졸버를 씁니다. 포트 없는 IP는 53번으로 봅니다. | api, worker | `1.1.1.1:53` | 선택 |
| `PUBLIC_URL` | 클라이언트가 api에 닿는 외부 주소. 화면과 자격 증명 안내의 `ACMEDNS_BASE_URL`에 씁니다. 비우면 요청에서 만듭니다(신뢰 프록시의 `X-Forwarded-Proto`·`X-Forwarded-Host` 포함). | api | `https://ssl-checker.example.com` | 선택 |
```

같은 파일의 `### 바이너리를 직접 실행할 때` 표에서 `ssl-checker serve`와 `ssl-checker worker` 행의 괄호 목록에 `CHECK_INTERVAL`, `DNS_RESOLVER`를, serve 행에는 `PUBLIC_URL`도 추가한다.

`docs/guide/api.md` 맨 끝(`## GET /health` 섹션 뒤)에 추가:

````markdown
## 조회 API (`/api`)

인증서 점검 결과와 ACME 상태를 JSON으로 봅니다. 인증 없이 누구나 부를 수 있습니다. 시각은 RFC 3339 UTC입니다. 키와 키 해시는 어떤 응답에도 들어가지 않습니다.

| 요청 | 내용 |
| --- | --- |
| `GET /api/summary` | 도메인 수와 상태별 개수, 마지막 점검 시각 |
| `GET /api/domains` | 도메인 목록. `?status=critical`처럼 상태로 거를 수 있습니다 |
| `GET /api/domains/{id}` | 대상별·접속 주소별 최신 점검 결과와 ACME 상태 |
| `GET /api/targets/{id}/results?limit=50` | 대상의 점검 이력, 최신순. `limit` 최대 500 |
| `GET /api/config` | `challenge_zone`, `check_interval`, `public_url` |

상태는 `critical` > `warning` > `info` > `ok` > `unknown` 순으로 나쁩니다. 도메인 상태는 대상과 ACME 상태 중 가장 나쁜 것입니다.

| 코드 | 상태 | 뜻 |
| --- | --- | --- |
| `CONNECT_FAILED` | critical | 접속이나 TLS 핸드셰이크 실패(10초) |
| `CERT_EXPIRED` | critical | 인증서 만료 |
| `CERT_EXPIRES_SOON` | critical | 만료까지 7일 미만 |
| `CHAIN_INVALID` | critical | 체인 검증 실패(자체 서명 포함) |
| `HOSTNAME_MISMATCH` | critical | 인증서가 접속 이름을 덮지 않음 |
| `RENEWAL_OVERDUE` | warning | 남은 기간이 수명의 1/3 미만. 정상이면 이미 갱신됐을 시점 |
| `SERIAL_MISMATCH` | warning | 같은 대상의 서버들이 서로 다른 인증서를 냄(일부 배포 누락) |
| `TLS_OUTDATED` | warning | TLS 1.2 미만으로 협상됨 |
| `CNAME_MISSING` / `CNAME_MISMATCH` | warning | ACME를 켰는데 `_acme-challenge` CNAME이 없거나 다른 곳을 가리킴 |
| `TXT_UPDATE_FAILING` | warning | `/update`가 3번 이상 연달아 실패 |
| `WEAK_KEY` | info | RSA 2048비트 미만, ECDSA 256비트 미만 |
| `NOT_CHECKED` | unknown | 아직 점검 결과가 없음 |

오류는 `{"error":"<코드>"}`입니다: 400 `invalid_status`·`invalid_limit`, 404 `not_found`, 500 `db_error`.

```bash
curl -s <앱>/api/domains?status=critical
```

도메인과 모니터링 대상을 추가·수정하는 API는 다음 버전에서 추가됩니다.
````

- [ ] **Step 5: 전체 확인**

Run: `make test && go vet ./... && ./scripts/smoke.sh 2>&1 | tail -1`
Expected: 전체 PASS, `smoke OK`

- [ ] **Step 6: 커밋**

```bash
git add deploy scripts docs/guide
git commit -m "docs: document monitoring settings and read API" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
