# 모니터링 2: 관리 API와 세션 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 관리자가 `ADMIN_KEY`로 로그인(세션 쿠키)하거나 Bearer 키로, 도메인·모니터링 대상을 등록·수정·삭제하고 도메인별 ACME를 켜고·끄고·키를 재발급할 수 있게 한다. 모든 쓰기는 감사 로그에 남는다.

**Architecture:** `internal/session`이 서버에 상태를 두지 않는 HMAC 쿠키를 만든다. `internal/adminapi`에 관리자 확인(`Auth`), 쓰기 핸들러, 감사 로그 조회를 더한다. ACME 키 재발급·폐기는 `acmedns.Service`에, 그 저장은 `store`에 둔다. 대상을 만들거나 접속 설정을 바꾸면 그 자리에서 한 번 점검한다.

**Tech Stack:** Go 1.27, PostgreSQL 17 + pgx/v5, `crypto/hmac`, 기존 `monitor`/`cnamecheck`/`acmedns`/`store` 패키지. 새 외부 의존성 없음.

**Spec:** `docs/superpowers/specs/2026-10-07-monitoring-dashboard-design.md` (「구현 계획 분할」 2번). 계획 1(`docs/superpowers/plans/2026-10-07-monitoring-1-engine-and-read-api.md`)은 main에 병합됐다.

## Global Constraints

- 모듈 경로 `ssl-checker`. 주석·로그·문서는 한국어, 식별자는 영어. 새 외부 의존성 금지.
- DB 테스트는 `dbtest.New(t)`. 실행: 테스트 DB가 떠 있으면 `make test`.
- 관리자 확인: 유효한 세션 쿠키 `sslchk_session` 또는 `Authorization: Bearer <ADMIN_KEY>`. 권한 없음은 401 `unauthorized`(이유 구분 없음).
- 틀린 키(로그인·Bearer)는 IP별 **5번**, 이후 **12초에 1번**. 초과는 429 `too_many_attempts`.
- `/api`의 GET·HEAD가 아닌 요청은 `Content-Type: application/json`만 받는다(본문이 없어도). 아니면 415 `unsupported_media_type`. 본문 최대 **64KiB**, 모르는 필드는 400 `malformed_json`.
- 세션 쿠키: 값 `<만료 유닉스초>.<HMAC-SHA256(서명키, 만료 문자열)의 base64url(패딩 없음)>`, 서명키 `SHA-256("sslchk-session:" + ADMIN_KEY)`, 수명 **12시간**, `HttpOnly`, `SameSite=Strict`, `Path=/`. `Secure`는 공개 주소가 `http://`일 때만 뺀다(아래 「스펙과 다른 점」).
- 일회성 자격 증명 응답은 `Cache-Control: no-store`. 비밀번호는 그 응답에만 나온다.
- 모든 쓰기는 `audit_logs`에 actor `admin`, source_ip, 대상으로 남긴다. 키·비밀번호·쿠키 값은 어떤 로그·응답에도 남기지 않는다(자격 증명 응답 제외).
- 커밋 메시지 끝: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

### 스펙과 다른 점·보완 (이 계획에서 정함)

- **쿠키 `Secure`:** 스펙은 항상 `Secure`다. 그러면 `compose.ports.yaml`로 http에 직접 연 배포에서 브라우저가 쿠키를 보내지 않아 로그인이 아예 안 된다. 공개 주소(`PublicURLFor`)가 `http://`면 `Secure`를 빼고, 문서에 "화면 로그인은 HTTPS 권장"을 적는다.
- **대상 추가·접속 설정 변경 시 즉시 점검:** 새 IP가 결과 없이 "정상"으로 보이는 문제(계획 1 이월). `POST /api/domains/{id}/targets`와 hostname·port·sni·ips를 바꾼 `PATCH /api/targets/{id}`는 그 자리에서 점검하고 결과를 응답에 넣는다.
- **주소별 동시 점검:** "지금 점검"이 요청 제한 시간(30초) 안에 끝나도록 `Prober.Probe`가 대상의 주소들을 동시에 점검한다. 대상당 IP는 최대 **16개**.
- **`PublicURLFor` 호스트 검증(계획 1 이월):** `X-Forwarded-Host`·`Host`가 `이름[:포트]`·`[IPv6][:포트]` 형태가 아니면 쓰지 않는다. 둘 다 못 쓰면 빈 문자열이고, 자격 증명 안내에는 `<ssl-checker 주소>`를 넣는다.
- **오류 코드 추가:** 400 `malformed_json`, `invalid_memo`, `invalid_allowfrom`, `invalid_before_id`; 409 `acme_disabled`(ACME가 꺼진 도메인에 재발급·수정·확인·끄기); 500 `dns_provider_error`(ACME 끄기·도메인 삭제 중 공급자 실패).
- **`/register`:** 지금도 "도메인이 없으면 만들고 ACME를 켠다"이므로 바꾸지 않는다.

## Review Focus

- 접속 설정을 바꾼 대상(IP 교체): 응답과 상세 화면에 새 IP 결과만 보이고 옛 IP 결과가 섞이지 않아야 한다 → Task 6 `TestPatchTargetRechecks`.
- 공급자 장애 중 ACME 끄기·도메인 삭제: 아무것도 지우지 않고 500 `dns_provider_error`, 계정·도메인은 그대로여야 한다 → Task 4 `TestDisableProviderFailureKeepsAccount`, Task 6 `TestDeleteDomainProviderFailure`.
- `ADMIN_KEY`를 바꾼 뒤의 옛 쿠키, 로그아웃 쿠키: 옛 쿠키는 거부되고, 로그아웃 응답은 쿠키를 즉시 만료시켜야 한다 → Task 2 `TestOtherKeyRejected`, Task 5 `TestLoginLogout`.
- 유효한 쿠키를 가진 브라우저가 다른 사이트의 폼 전송(`application/x-www-form-urlencoded`)을 당함: 415로 거부해야 한다 → Task 5 `TestWriteRequiresJSON`.
- 접속이 안 되는 IP가 여러 개인 대상의 "지금 점검": 타임아웃 한 번 정도에 끝나야 한다 → Task 1 `TestProbeAddressesConcurrently`, Task 6 `TestCreateTargetChecksImmediately`.

---

## 파일 구조

| 파일 | 책임 |
| --- | --- |
| `internal/monitor/probe.go` | 주소별 동시 점검 |
| `internal/session/session.go` | 쿠키 서명·검증 |
| `internal/store/monitor.go` | 도메인 메모 수정·삭제, 대상 수정·삭제 |
| `internal/store/accounts.go` (새) | 키 교체, 계정 폐기, allowfrom 수정 |
| `internal/store/audit.go` (새) | 감사 로그 조회 |
| `internal/acmedns/service.go` | `Rotate`, `Disable` |
| `internal/adminapi/auth.go` (새) | `Auth`, 관리자 확인, JSON 요구, 본문 읽기, 감사 기록 |
| `internal/adminapi/session.go` (새) | `/api/session` 로그인·상태·로그아웃 |
| `internal/adminapi/audit.go` (새) | `GET /api/audit` |
| `internal/adminapi/api.go` | 경로 표, catch-all, `PublicURLFor` 검증, 상세 응답 공통화 |
| `internal/adminapi/write.go` (새) | 도메인·대상 쓰기 |
| `internal/adminapi/validate.go` (새) | 입력 검사 |
| `internal/adminapi/acme.go` (새) | ACME 켜기·수정·재발급·확인·끄기, 자격 증명 응답 |
| `internal/app/serve.go` | 새 의존성 연결 |

---

### Task 1: 주소별 동시 점검

**Files:**
- Modify: `internal/monitor/probe.go` (`Probe`)
- Test: `internal/monitor/probe_test.go`

**Interfaces:**
- Consumes: 기존 `Prober.probeAddr`, 테스트 도우미 `hangingPort`, `target`, `expectCodes`
- Produces: `Prober.Probe(ctx, t) []Result`의 동작만 바뀐다(순서는 `DialAddresses()` 그대로, 주소들을 동시에 점검).

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/monitor/probe_test.go` 끝에 추가:

```go
// 접속이 안 되는 주소가 여러 개여도 점검 한 번이 타임아웃 한 번 정도에 끝나야 한다.
func TestProbeAddressesConcurrently(t *testing.T) {
	port := hangingPort(t)
	tg := target(port, "a.test", "127.0.0.1", "127.0.0.1", "127.0.0.1")
	start := time.Now()
	rs := (&Prober{Timeout: 300 * time.Millisecond}).Probe(context.Background(), tg)
	if elapsed := time.Since(start); elapsed > 800*time.Millisecond {
		t.Fatalf("Probe took %s; addresses were not probed concurrently", elapsed)
	}
	if len(rs) != 3 {
		t.Fatalf("got %d results", len(rs))
	}
	for _, r := range rs {
		expectCodes(t, r, StatusCritical, CodeConnectFailed)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/monitor/ -run TestProbeAddressesConcurrently -v`
Expected: FAIL (`Probe took 9..`ms 수준, 약 900ms)

- [ ] **Step 3: 구현**

`internal/monitor/probe.go`의 import에 `"sync"`를 더하고 `Probe`를 바꾼다:

```go
// Probe는 대상의 접속 주소마다 결과 하나를 돌려준다. 판정(Status, Reasons)까지 채운다.
// 주소들을 동시에 점검해 한 번의 점검이 Timeout 정도로 끝나게 한다("지금 점검"은 요청 제한 시간 안에 끝나야 한다).
func (p *Prober) Probe(ctx context.Context, t Target) []Result {
	addrs := t.DialAddresses()
	out := make([]Result, len(addrs))
	var wg sync.WaitGroup
	for i, addr := range addrs {
		wg.Go(func() { out[i] = p.probeAddr(ctx, t, addr) })
	}
	wg.Wait()
	return out
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/monitor/ -race -count=3`
Expected: PASS (기존 `TestProbeMultipleIPs`의 주소 순서 검사 포함)

- [ ] **Step 5: 커밋**

```bash
git add internal/monitor
git commit -m "feat: probe a target's addresses concurrently" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: 세션 쿠키 서명 (`session`)

**Files:**
- Create: `internal/session/session.go`, `internal/session/session_test.go`

**Interfaces:**
- Produces:
  - `const session.CookieName = "sslchk_session"`, `const session.DefaultTTL = 12 * time.Hour`
  - `type session.Signer struct { TTL time.Duration; Now func() time.Time; /* key unexported */ }`
  - `func session.New(adminKey string) *Signer`
  - `func (s *Signer) Issue() (value string, expires time.Time)`
  - `func (s *Signer) Valid(value string) bool`

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/session/session_test.go`:

```go
package session_test

import (
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/session"
)

const key = "admin-key-0123456789abcdef0123456789"

func at(t time.Time) func() time.Time { return func() time.Time { return t } }

var t0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

func TestIssueAndValidate(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	v, exp := s.Issue()
	if !exp.Equal(t0.Add(12 * time.Hour)) {
		t.Fatalf("expires = %s", exp)
	}
	if !s.Valid(v) {
		t.Fatal("fresh cookie rejected")
	}
	s.Now = at(t0.Add(12*time.Hour - time.Second))
	if !s.Valid(v) {
		t.Fatal("cookie rejected before expiry")
	}
	s.Now = at(t0.Add(12 * time.Hour))
	if s.Valid(v) {
		t.Fatal("expired cookie accepted")
	}
}

func TestTamperedRejected(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	v, _ := s.Issue()
	payload, sig, _ := strings.Cut(v, ".")
	later := "9999999999"
	for _, bad := range []string{
		later + "." + sig,             // 만료를 늘림
		payload + "." + sig[:len(sig)-1] + "A", // 서명 변조
		payload + "." + sig + "x",
		"", ".", "abc", payload, "1.2.3", "-1." + sig,
	} {
		if bad == v {
			continue
		}
		if s.Valid(bad) {
			t.Errorf("accepted %q", bad)
		}
	}
}

// ADMIN_KEY를 바꾸면 이전에 발급한 쿠키는 모두 무효가 된다.
func TestOtherKeyRejected(t *testing.T) {
	a := session.New(key)
	b := session.New(key + "-rotated")
	a.Now, b.Now = at(t0), at(t0)
	v, _ := a.Issue()
	if b.Valid(v) {
		t.Fatal("cookie from old key accepted")
	}
}

func TestCustomTTL(t *testing.T) {
	s := session.New(key)
	s.Now = at(t0)
	s.TTL = time.Hour
	if _, exp := s.Issue(); !exp.Equal(t0.Add(time.Hour)) {
		t.Fatalf("expires = %s", exp)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `go test ./internal/session/`
Expected: FAIL (패키지 없음)

- [ ] **Step 3: 구현**

`internal/session/session.go`:

```go
// Package session은 관리자 로그인 쿠키를 서명하고 검증한다. 서버에 세션을 저장하지 않으므로
// ADMIN_KEY를 바꾸면 모든 쿠키가 무효가 되고, 로그아웃은 브라우저의 쿠키만 지운다.
package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

const (
	CookieName = "sslchk_session"
	DefaultTTL = 12 * time.Hour
)

type Signer struct {
	TTL time.Duration    // 0이면 DefaultTTL
	Now func() time.Time // nil이면 time.Now
	key [32]byte
}

// New의 서명키는 ADMIN_KEY에서 만든다. 서명키가 키 자체와 다르도록 접두어를 붙여 해시한다.
func New(adminKey string) *Signer {
	return &Signer{key: sha256.Sum256([]byte("sslchk-session:" + adminKey))}
}

func (s *Signer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Issue는 쿠키 값과 만료 시각이다. 값은 "<만료 유닉스초>.<HMAC-SHA256 base64url>"이다.
func (s *Signer) Issue() (string, time.Time) {
	ttl := s.TTL
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	exp := s.now().Add(ttl).Truncate(time.Second)
	payload := strconv.FormatInt(exp.Unix(), 10)
	return payload + "." + s.mac(payload), exp
}

// Valid는 서명이 맞고 아직 만료되지 않은 값인지 본다.
func (s *Signer) Valid(value string) bool {
	payload, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || exp <= 0 {
		return false
	}
	if !hmac.Equal([]byte(sig), []byte(s.mac(payload))) {
		return false
	}
	return s.now().Unix() < exp
}

func (s *Signer) mac(payload string) string {
	m := hmac.New(sha256.New, s.key[:])
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
```

- [ ] **Step 4: 통과 확인**

Run: `go test ./internal/session/ -v && go vet ./internal/session/`
Expected: PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/session
git commit -m "feat: add stateless signed admin session cookie" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: 저장소 쓰기 메서드

**Files:**
- Modify: `internal/store/monitor.go` (도메인 메모 수정·삭제, 대상 수정·삭제)
- Create: `internal/store/accounts.go`, `internal/store/audit.go`
- Test: `internal/store/monitor_test.go`, `internal/store/accounts_test.go`, `internal/store/audit_test.go`

**Interfaces:**
- Consumes: 기존 `Domain`, `Target`, `NewTarget`, `scanDomain`, `scanTarget`, `targetCols`, `pgCode`, `codeUniqueViolation`, `addrStrings`, `parsePrefixes`, `prefixStrings`, `ErrNotFound`, `ErrTargetExists`, `Account`, `AuditEntry`; 테스트 도우미 `newStore`, `create`, `mustDomain`, `mustTarget`.
- Produces:
  - `func (s *Store) UpdateDomainMemo(ctx, id int64, memo string) (Domain, error)` (없으면 `ErrNotFound`)
  - `func (s *Store) DeleteDomain(ctx, id int64) error` (없으면 `ErrNotFound`; 대상·결과·계정·TXT는 cascade)
  - `type store.TargetFields struct { Hostname string; Port int; SNI string; IPs []netip.Addr; Memo string }`
  - `func (s *Store) UpdateTarget(ctx, id int64, f TargetFields) (Target, error)` (`ErrNotFound`, `ErrTargetExists`)
  - `func (s *Store) DeleteTarget(ctx, id int64) error` (`ErrNotFound`)
  - `func (s *Store) RotateKey(ctx, accountID int64, keyHash string) (Account, error)` (활성 계정만, 아니면 `ErrNotFound`)
  - `func (s *Store) RevokeAccount(ctx, accountID int64, beforeCommit func(ctx context.Context, subdomain string) error) error`
  - `func (s *Store) SetAllowFrom(ctx, accountID int64, allow []netip.Prefix) error` (`ErrNotFound`)
  - `type store.AuditRecord struct { ID int64; At time.Time; AuditEntry }`
  - `func (s *Store) ListAudit(ctx, beforeID int64, limit int) ([]AuditRecord, error)` (최신순, `beforeID == 0`이면 처음부터)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/store/monitor_test.go` 끝에 추가:

```go
func TestUpdateAndDeleteDomain(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	d := mustDomain(t, st, "a.com")
	got, err := st.UpdateDomainMemo(ctx, d.ID, "web servers")
	if err != nil || got.Memo != "web servers" || got.FQDN != "a.com" || got.ID != d.ID {
		t.Fatalf("UpdateDomainMemo = %+v, %v", got, err)
	}
	if _, err := st.UpdateDomainMemo(ctx, 99999, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	create(t, st, "a.com")
	tg := mustTarget(t, st, d.ID, "www.a.com", "10.0.0.1")
	save(t, st, tg.ID, okResult("10.0.0.1:443", t0, "01"))
	if err := st.DeleteDomain(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"SELECT count(*) FROM domains", "SELECT count(*) FROM monitor_targets",
		"SELECT count(*) FROM check_results", "SELECT count(*) FROM acme_accounts",
	} {
		var n int
		if err := pool.QueryRow(ctx, q).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s = %d, %v", q, n, err)
		}
	}
	if err := st.DeleteDomain(ctx, d.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}

func TestUpdateAndDeleteTarget(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	d := mustDomain(t, st, "a.com")
	tg := mustTarget(t, st, d.ID, "www.a.com", "10.0.0.1")
	other := mustTarget(t, st, d.ID, "api.a.com")

	f := store.TargetFields{Hostname: "www.a.com", Port: 8443, SNI: "a.com",
		IPs: []netip.Addr{netip.MustParseAddr("10.0.0.2"), netip.MustParseAddr("2001:db8::2")}, Memo: "moved"}
	got, err := st.UpdateTarget(ctx, tg.ID, f)
	if err != nil || got.ID != tg.ID || got.DomainID != d.ID || got.Port != 8443 || got.SNI != "a.com" || got.Memo != "moved" ||
		!slices.Equal(got.IPs, f.IPs) {
		t.Fatalf("UpdateTarget = %+v, %v", got, err)
	}
	reread, _ := st.GetTarget(ctx, tg.ID)
	if !slices.Equal(reread.IPs, f.IPs) || reread.Port != 8443 {
		t.Fatalf("reread = %+v", reread)
	}
	clash := store.TargetFields{Hostname: "api.a.com", Port: 443}
	if _, err := st.UpdateTarget(ctx, tg.ID, clash); !errors.Is(err, store.ErrTargetExists) {
		t.Fatalf("clash = %v", err)
	}
	if _, err := st.UpdateTarget(ctx, 99999, f); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if err := st.DeleteTarget(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteTarget(ctx, other.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete = %v", err)
	}
}
```

`internal/store/accounts_test.go`:

```go
package store_test

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"testing"

	"ssl-checker/internal/store"
)

func TestRotateKey(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	allow := netip.MustParsePrefix("10.0.0.0/24")
	a := create(t, st, "a.com", allow)
	got, err := st.RotateKey(ctx, a.ID, "new-hash")
	if err != nil || got.ID != a.ID || got.Username != a.Username || got.Subdomain != a.Subdomain || got.Domain != "a.com" ||
		got.KeyHash != "new-hash" || !slices.Equal(got.AllowFrom, []netip.Prefix{allow}) {
		t.Fatalf("RotateKey = %+v, %v", got, err)
	}
	loaded, _ := st.ActiveAccountByUsername(ctx, a.Username)
	if loaded.KeyHash != "new-hash" {
		t.Fatalf("stored hash = %q", loaded.KeyHash)
	}
	if _, err := st.RotateKey(ctx, 99999, "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestRevokeAccount(t *testing.T) {
	st, pool := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	if err := st.UpdateTXT(ctx, a.ID, func([]string) ([]string, error) { return []string{"v1"}, nil }); err != nil {
		t.Fatal(err)
	}

	// beforeCommit이 실패하면 아무것도 바뀌지 않는다.
	boom := errors.New("provider down")
	var gotSub string
	err := st.RevokeAccount(ctx, a.ID, func(_ context.Context, sub string) error { gotSub = sub; return boom })
	if !errors.Is(err, boom) || gotSub != a.Subdomain {
		t.Fatalf("failing revoke = %v (sub %q)", err, gotSub)
	}
	if _, err := st.ActiveAccountByUsername(ctx, a.Username); err != nil {
		t.Fatalf("account gone after failed revoke: %v", err)
	}

	if err := st.RevokeAccount(ctx, a.ID, func(context.Context, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ActiveAccountByUsername(ctx, a.Username); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("account still active: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM txt_records`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("txt rows = %d, %v", n, err)
	}
	if err := st.RevokeAccount(ctx, a.ID, func(context.Context, string) error { return nil }); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second revoke = %v", err)
	}
	// 폐기 후 다시 켜면 새 계정을 만들 수 있다.
	create(t, st, "a.com")
}

func TestSetAllowFrom(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	a := create(t, st, "a.com")
	want := []netip.Prefix{netip.MustParsePrefix("10.1.0.0/16"), netip.MustParsePrefix("2001:db8::/32")}
	if err := st.SetAllowFrom(ctx, a.ID, want); err != nil {
		t.Fatal(err)
	}
	s, _ := st.ACMEStateByDomain(ctx, a.DomainID)
	if !slices.Equal(s.AllowFrom, want) {
		t.Fatalf("allowfrom = %v", s.AllowFrom)
	}
	if err := st.SetAllowFrom(ctx, a.ID, nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := st.ACMEStateByDomain(ctx, a.DomainID); len(s.AllowFrom) != 0 {
		t.Fatalf("cleared allowfrom = %v", s.AllowFrom)
	}
	if err := st.SetAllowFrom(ctx, 99999, want); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}
```

`internal/store/audit_test.go`:

```go
package store_test

import (
	"context"
	"testing"

	"ssl-checker/internal/store"
)

func TestListAudit(t *testing.T) {
	st, _ := newStore(t)
	ctx := context.Background()
	for _, action := range []string{"a1", "a2", "a3"} {
		if err := st.Audit(ctx, store.AuditEntry{Actor: "admin", Action: action, Target: "a.com", SourceIP: "10.0.0.1", Detail: "d"}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := st.ListAudit(ctx, 0, 10)
	if err != nil || len(all) != 3 || all[0].Action != "a3" || all[2].Action != "a1" || all[0].At.IsZero() ||
		all[0].Actor != "admin" || all[0].Target != "a.com" || all[0].SourceIP != "10.0.0.1" || all[0].Detail != "d" {
		t.Fatalf("ListAudit = %+v, %v", all, err)
	}
	page, err := st.ListAudit(ctx, all[0].ID, 1)
	if err != nil || len(page) != 1 || page[0].Action != "a2" {
		t.Fatalf("page = %+v, %v", page, err)
	}
	if empty, _ := st.ListAudit(ctx, all[2].ID, 10); len(empty) != 0 {
		t.Fatalf("before first = %+v", empty)
	}
}
```

- [ ] **Step 2: 실패 확인**

Run: `make test 2>&1 | grep -A3 internal/store`
Expected: FAIL (정의 없음)

- [ ] **Step 3: 구현**

`internal/store/monitor.go`의 `GetDomain` 아래에 추가:

```go
func (s *Store) UpdateDomainMemo(ctx context.Context, id int64, memo string) (Domain, error) {
	rows, err := s.pool.Query(ctx, `UPDATE domains SET memo = $2 WHERE id = $1 RETURNING id, fqdn, memo, created_at`, id, memo)
	if err != nil {
		return Domain{}, fmt.Errorf("store: update domain: %w", err)
	}
	d, err := pgx.CollectExactlyOneRow(rows, scanDomain)
	if errors.Is(err, pgx.ErrNoRows) {
		return Domain{}, ErrNotFound
	}
	if err != nil {
		return Domain{}, fmt.Errorf("store: update domain: %w", err)
	}
	return d, nil
}

// DeleteDomain은 도메인과 대상·점검 기록·ACME 계정·TXT 기록을 함께 지운다(외래 키 cascade).
// 공급자의 TXT는 지우지 않으므로, ACME가 켜져 있으면 먼저 끈다.
func (s *Store) DeleteDomain(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM domains WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete domain: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

`GetTarget` 아래에 추가:

```go
// TargetFields는 대상에서 바꿀 수 있는 값이다.
type TargetFields struct {
	Hostname string
	Port     int
	SNI      string
	IPs      []netip.Addr
	Memo     string
}

func (s *Store) UpdateTarget(ctx context.Context, id int64, f TargetFields) (Target, error) {
	rows, err := s.pool.Query(ctx, `UPDATE monitor_targets SET hostname = $2, port = $3, sni = $4, ips = $5, memo = $6
		WHERE id = $1 RETURNING `+targetCols, id, f.Hostname, f.Port, f.SNI, addrStrings(f.IPs), f.Memo)
	if err == nil {
		var t Target
		t, err = pgx.CollectExactlyOneRow(rows, scanTarget)
		if err == nil {
			return t, nil
		}
	}
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Target{}, ErrNotFound
	case pgCode(err) == codeUniqueViolation:
		return Target{}, ErrTargetExists
	}
	return Target{}, fmt.Errorf("store: update target: %w", err)
}

func (s *Store) DeleteTarget(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM monitor_targets WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("store: delete target: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

`internal/store/accounts.go`:

```go
package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"

	"github.com/jackc/pgx/v5"
)

// RotateKey는 활성 계정의 키 해시만 바꾼다. username과 서브도메인이 그대로라 CNAME을 다시 걸 필요가 없다.
// 진행 중인 /update가 계정 행을 잠그고 있으면 끝날 때까지 기다린다.
func (s *Store) RotateKey(ctx context.Context, accountID int64, keyHash string) (Account, error) {
	var a Account
	var allow []string
	err := s.pool.QueryRow(ctx, `UPDATE acme_accounts a SET key_hash = $2
		FROM domains d
		WHERE a.id = $1 AND a.revoked_at IS NULL AND d.id = a.domain_id
		RETURNING a.id, a.domain_id, d.fqdn, a.username::text, a.key_hash, a.subdomain::text, a.allowfrom, a.created_at`,
		accountID, keyHash).Scan(&a.ID, &a.DomainID, &a.Domain, &a.Username, &a.KeyHash, &a.Subdomain, &allow, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrNotFound
	}
	if err != nil {
		return Account{}, fmt.Errorf("store: rotate key: %w", err)
	}
	if a.AllowFrom, err = parsePrefixes(allow); err != nil {
		return Account{}, fmt.Errorf("store: account %d allowfrom: %w", a.ID, err)
	}
	return a, nil
}

// RevokeAccount는 계정 행을 잠근 채 beforeCommit(공급자 TXT 삭제)을 부르고, 성공하면 TXT 기록을 지우고
// 계정을 폐기한다. beforeCommit이 실패하면 아무것도 바꾸지 않는다. 잠금 덕분에 진행 중인 /update와 겹치지 않는다.
func (s *Store) RevokeAccount(ctx context.Context, accountID int64, beforeCommit func(ctx context.Context, subdomain string) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var sub string
		err := tx.QueryRow(ctx, `SELECT subdomain::text FROM acme_accounts WHERE id = $1 AND revoked_at IS NULL FOR UPDATE`, accountID).Scan(&sub)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("store: lock account: %w", err)
		}
		if err := beforeCommit(ctx, sub); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM txt_records WHERE account_id = $1`, accountID); err != nil {
			return fmt.Errorf("store: delete txt: %w", err)
		}
		if _, err := tx.Exec(ctx, `UPDATE acme_accounts SET revoked_at = now() WHERE id = $1`, accountID); err != nil {
			return fmt.Errorf("store: revoke account: %w", err)
		}
		return nil
	})
}

func (s *Store) SetAllowFrom(ctx context.Context, accountID int64, allow []netip.Prefix) error {
	tag, err := s.pool.Exec(ctx, `UPDATE acme_accounts SET allowfrom = $2 WHERE id = $1 AND revoked_at IS NULL`,
		accountID, prefixStrings(allow))
	if err != nil {
		return fmt.Errorf("store: set allowfrom: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

`internal/store/audit.go`:

```go
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuditRecord struct {
	ID int64
	At time.Time
	AuditEntry
}

// ListAudit는 감사 로그를 최신순으로 돌려준다. beforeID가 0보다 크면 그보다 오래된 것만 준다(다음 페이지).
func (s *Store) ListAudit(ctx context.Context, beforeID int64, limit int) ([]AuditRecord, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, at, actor, action, target, source_ip, detail FROM audit_logs
		WHERE $1::bigint = 0 OR id < $1::bigint ORDER BY id DESC LIMIT $2`, beforeID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (AuditRecord, error) {
		var r AuditRecord
		err := row.Scan(&r.ID, &r.At, &r.Actor, &r.Action, &r.Target, &r.SourceIP, &r.Detail)
		r.At = r.At.UTC()
		return r, err
	})
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	return out, nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `make test && go vet ./...`
Expected: 전체 PASS

- [ ] **Step 5: 커밋**

```bash
git add internal/store
git commit -m "feat: store writes for domains, targets, account keys, and audit listing" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: ACME 키 재발급과 끄기 (`acmedns.Service`)

**Files:**
- Modify: `internal/acmedns/service.go`
- Test: `internal/acmedns/service_test.go`

**Interfaces:**
- Consumes: `store.RotateKey`, `store.RevokeAccount` (Task 3); 기존 `newPassword`, `providerWriteTimeout`, `Registration`, `FQDN`; 테스트 도우미 `newService`, `registerAndAuth`, `dbValues`, `txt`.
- Produces:
  - `func (s *Service) Rotate(ctx context.Context, accountID int64) (Registration, error)` (없거나 폐기됨 → `store.ErrNotFound`)
  - `func (s *Service) Disable(ctx context.Context, accountID int64) error` (공급자 실패는 `*dnsprovider.Error`를 감싼 오류, 없음 → `store.ErrNotFound`)

- [ ] **Step 1: 실패하는 테스트 작성**

`internal/acmedns/service_test.go` 끝에 추가(필요하면 import에 `"ssl-checker/internal/dnsprovider"` 추가):

```go
func TestRotate(t *testing.T) {
	svc, _, _ := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	rot, err := svc.Rotate(ctx, acct.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rot.Username != reg.Username || rot.Subdomain != reg.Subdomain || rot.FullDomain != reg.FullDomain ||
		rot.Password == reg.Password || len(rot.Password) != 40 {
		t.Fatalf("Rotate = %+v (was %+v)", rot, reg)
	}
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("old key = %v", err)
	}
	if _, err := svc.Authenticate(ctx, rot.Username, rot.Password); err != nil {
		t.Fatalf("new key = %v", err)
	}
	if _, err := svc.Rotate(ctx, 99999); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestDisable(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	if len(mem.Values(reg.FullDomain)) != 1 {
		t.Fatal("setup: txt not written")
	}
	if err := svc.Disable(ctx, acct.ID); err != nil {
		t.Fatal(err)
	}
	if v := mem.Values(reg.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
	if v := dbValues(t, pool); len(v) != 0 {
		t.Fatalf("db still has %v", v)
	}
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("revoked account authenticated: %v", err)
	}
	if err := svc.Disable(ctx, acct.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second disable = %v", err)
	}
}

func TestDisableProviderFailureKeepsAccount(t *testing.T) {
	svc, mem, pool := newService(t)
	ctx := context.Background()
	reg, acct := registerAndAuth(t, svc, "a.com")
	if err := svc.UpdateTXT(ctx, acct, txt('a')); err != nil {
		t.Fatal(err)
	}
	mem.SetFailure(errors.New("cloudflare down"))
	err := svc.Disable(ctx, acct.ID)
	var perr *dnsprovider.Error
	if !errors.As(err, &perr) {
		t.Fatalf("Disable = %v, want provider error", err)
	}
	mem.SetFailure(nil)
	if _, err := svc.Authenticate(ctx, reg.Username, reg.Password); err != nil {
		t.Fatalf("account lost after failed disable: %v", err)
	}
	if v := dbValues(t, pool); len(v) != 1 {
		t.Fatalf("db txt = %v", v)
	}
}
```

`registerAndAuth`, `newService`, `dbValues`의 실제 시그니처를 확인하고 맞춘다(`newService(t) (*acmedns.Service, *memory.Provider, *pgxpool.Pool)`, `registerAndAuth(t, svc, domain) (acmedns.Registration, store.Account)`).

- [ ] **Step 2: 실패 확인**

Run: `make test 2>&1 | grep -A3 internal/acmedns`
Expected: FAIL (`Rotate`, `Disable` 정의 없음)

- [ ] **Step 3: 구현**

`internal/acmedns/service.go`: `Register`의 비밀번호·해시 생성을 도우미로 빼고 두 메서드를 더한다.

`Register` 안의

```go
	password, err := newPassword()
	if err != nil {
		return Registration{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Registration{}, fmt.Errorf("acmedns: hash key: %w", err)
	}
```

를

```go
	password, hash, err := newKey()
	if err != nil {
		return Registration{}, err
	}
```

로 바꾸고(`KeyHash: hash`), 파일 끝에 추가:

```go
// Rotate는 활성 계정의 키를 새로 만든다. 옛 키는 바로 무효가 되고, username과 서브도메인은 그대로다.
func (s *Service) Rotate(ctx context.Context, accountID int64) (Registration, error) {
	password, hash, err := newKey()
	if err != nil {
		return Registration{}, err
	}
	acct, err := s.Store.RotateKey(ctx, accountID, hash)
	if err != nil {
		return Registration{}, err
	}
	return Registration{
		Username:   acct.Username,
		Password:   password,
		Subdomain:  acct.Subdomain,
		FullDomain: s.FQDN(acct.Subdomain),
		AllowFrom:  acct.AllowFrom,
	}, nil
}

// Disable은 계정을 폐기하고 챌린지 TXT를 지운다. 공급자 삭제가 실패하면 계정과 TXT 기록은 그대로 둔다.
func (s *Service) Disable(ctx context.Context, accountID int64) error {
	return s.Store.RevokeAccount(ctx, accountID, func(ctx context.Context, subdomain string) error {
		pctx, cancel := context.WithTimeout(ctx, providerWriteTimeout)
		defer cancel()
		return s.Provider.DeleteTXT(pctx, s.FQDN(subdomain))
	})
}

// newKey는 평문 키와 그 bcrypt 해시다. 평문은 호출한 곳에서 한 번만 내보낸다.
func newKey() (password, hash string, err error) {
	password, err = newPassword()
	if err != nil {
		return "", "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", "", fmt.Errorf("acmedns: hash key: %w", err)
	}
	return password, string(h), nil
}
```

- [ ] **Step 4: 통과 확인**

Run: `make test && go vet ./...`
Expected: 전체 PASS (기존 `/register`·compat 관련 테스트 포함)

- [ ] **Step 5: 커밋**

```bash
git add internal/acmedns
git commit -m "feat: rotate and disable ACME accounts" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: 관리자 확인, 세션 API, 감사 로그 조회

**Files:**
- Create: `internal/adminapi/auth.go`, `internal/adminapi/session.go`, `internal/adminapi/audit.go`
- Modify: `internal/adminapi/api.go` (`API.Auth` 필드, 경로 표 기반 `Register`, `parseLimit`, `PublicURLFor` 호스트 검증)
- Test: `internal/adminapi/api_test.go`(fixture·기존 테스트 조정), `internal/adminapi/auth_test.go`

**Interfaces:**
- Consumes: `session.New`, `session.CookieName`, `Signer.Issue/Valid` (Task 2), `store.ListAudit`, `AuditRecord`, `Audit` (Task 3), `httpx.NewKeyed`, `httpx.WriteError/WriteJSON`.
- Produces (같은 패키지의 Task 6·7이 쓴다):
  - `type adminapi.Auth struct { Sessions *session.Signer; /* unexported keyHash, failures */ }`, `func adminapi.NewAuth(adminKey string) *Auth`
  - `API.Auth *Auth` 필드
  - `func (a *API) write(next http.HandlerFunc) http.HandlerFunc` — 관리자 확인 + JSON 요구 + 본문 64KiB 제한
  - `func (a *API) admin(next http.HandlerFunc) http.HandlerFunc` — 관리자 확인만(GET용)
  - `func decode(r *http.Request, v any) error` — 모르는 필드 거부, 빈 본문 허용
  - `func (a *API) audit(r *http.Request, e store.AuditEntry)` — `Actor` 비면 `admin`, `SourceIP` 채움
  - `func parseLimit(r *http.Request, def, max int) (int, bool)`
  - `type route struct { method, pattern string; h http.HandlerFunc }`, `func (a *API) routes() []route` — Task 6·7이 여기에 줄을 더한다
  - 경로: `POST /api/session`, `GET /api/session`, `DELETE /api/session`, `GET /api/audit`

- [ ] **Step 1: fixture와 기존 테스트 조정**

`internal/adminapi/api_test.go`:

1. import 블록 정리 후 상수 추가:

```go
const adminKey = "test-admin-key-0123456789abcdef0123"
```

2. `newFixture`의 `api := &adminapi.API{...}` 리터럴에 `Auth: adminapi.NewAuth(adminKey),`를 더한다.

3. 요청 도우미를 추가한다(파일의 `get` 아래):

```go
type call struct {
	method, path, body string
	bearer             bool         // Authorization: Bearer <adminKey>
	cookie             *http.Cookie // 세션 쿠키
	contentType        string       // 비우면 GET이 아닐 때 application/json, "-"면 보내지 않음
	remote             string       // RemoteAddr
	headers            map[string]string
}

func (f *fixture) do(c call) *httptest.ResponseRecorder {
	r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
	ct := c.contentType
	if ct == "" && c.method != http.MethodGet {
		ct = "application/json"
	}
	if ct != "" && ct != "-" {
		r.Header.Set("Content-Type", ct)
	}
	if c.bearer {
		r.Header.Set("Authorization", "Bearer "+adminKey)
	}
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if c.remote != "" {
		r.RemoteAddr = c.remote
	}
	for k, v := range c.headers {
		if k == "Host" { // httptest.NewRequest는 Host 헤더를 r.Host로 옮기지 않는다
			r.Host = v
			continue
		}
		r.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	return rec
}

// admin은 Bearer 키로 부른다.
func (f *fixture) admin(method, path, body string) *httptest.ResponseRecorder {
	return f.do(call{method: method, path: path, body: body, bearer: true})
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %d %s: %v", rec.Code, rec.Body, err)
	}
	return v
}
```

4. `TestUnknownPathAndMethod`의 405 검사 루프를 아래로 바꾼다(이제 `POST /api/domains` 같은 쓰기 경로가 생기므로 쓰지 않는 메서드 `PUT`으로 본다):

```go
	for _, path := range []string{"/api/domains", "/api/domains/1", "/api/summary", "/api/targets/1/results", "/api/config"} {
		rec := f.do(call{method: http.MethodPut, path: path})
		expectError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
			t.Fatalf("%s Allow = %q", path, allow)
		}
	}
```

나머지(`/api/nope`, `POST /api/nope` 404, `/api/config` 200)는 그대로 둔다.

- [ ] **Step 2: 실패하는 테스트 작성**

`internal/adminapi/auth_test.go`:

```go
package adminapi_test

import (
	"net/http"
	"strings"
	"testing"

	"ssl-checker/internal/session"
)

func sessionCookie(t *testing.T, rec interface{ Result() *http.Response }) *http.Cookie {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func TestLoginLogout(t *testing.T) {
	f := newFixture(t)
	if got := decodeBody[map[string]bool](t, f.get("/api/session")); got["admin"] {
		t.Fatal("anonymous reported as admin")
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`}), http.StatusUnauthorized, "unauthorized")

	rec := f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`})
	if rec.Code != http.StatusOK || !decodeBody[map[string]bool](t, rec)["admin"] {
		t.Fatalf("login = %d %s", rec.Code, rec.Body)
	}
	c := sessionCookie(t, rec)
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge <= 0 || strings.Contains(c.Value, adminKey) {
		t.Fatalf("cookie = %+v", c)
	}
	if got := decodeBody[map[string]bool](t, f.do(call{method: "GET", path: "/api/session", cookie: c})); !got["admin"] {
		t.Fatal("cookie not recognized")
	}
	if rec := f.do(call{method: "GET", path: "/api/audit", cookie: c}); rec.Code != http.StatusOK {
		t.Fatalf("audit with cookie = %d", rec.Code)
	}

	out := f.do(call{method: "DELETE", path: "/api/session", cookie: c})
	if out.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", out.Code)
	}
	if cleared := sessionCookie(t, out); cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatalf("logout cookie = %+v", cleared)
	}
}

func TestSecureCookieFollowsPublicURL(t *testing.T) {
	f := newFixture(t)
	login := func(c call) *http.Cookie {
		c.method, c.path, c.body = "POST", "/api/session", `{"key":"`+adminKey+`"}`
		return sessionCookie(t, f.do(c))
	}
	if c := login(call{}); c.Secure {
		t.Fatal("plain http request got Secure cookie")
	}
	proxied := call{remote: "10.0.0.5:4000", headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "ssl.example.com"}}
	if c := login(proxied); !c.Secure {
		t.Fatal("https via trusted proxy got non-Secure cookie")
	}
}

func TestAdminRequired(t *testing.T) {
	f := newFixture(t)
	expectError(t, f.get("/api/audit"), http.StatusUnauthorized, "unauthorized")
	bad := f.do(call{method: "GET", path: "/api/audit", headers: map[string]string{"Authorization": "Bearer nope"}})
	expectError(t, bad, http.StatusUnauthorized, "unauthorized")
	forged := &http.Cookie{Name: session.CookieName, Value: "9999999999.forged"}
	expectError(t, f.do(call{method: "GET", path: "/api/audit", cookie: forged}), http.StatusUnauthorized, "unauthorized")
	if rec := f.do(call{method: "GET", path: "/api/audit", bearer: true}); rec.Code != http.StatusOK {
		t.Fatalf("bearer = %d %s", rec.Code, rec.Body)
	}
}

// 틀린 키는 IP별로 5번까지 받고 그다음은 429다. 로그인과 Bearer가 같은 한도를 쓴다.
func TestWrongKeyLockout(t *testing.T) {
	f := newFixture(t)
	for range 3 {
		f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`})
	}
	for range 2 {
		f.do(call{method: "GET", path: "/api/audit", headers: map[string]string{"Authorization": "Bearer wrong"}})
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`}), http.StatusTooManyRequests, "too_many_attempts")
	expectError(t, f.do(call{method: "GET", path: "/api/audit", bearer: true}), http.StatusTooManyRequests, "too_many_attempts")
	// 다른 IP는 영향을 받지 않는다.
	if rec := f.do(call{method: "GET", path: "/api/audit", bearer: true, remote: "198.51.100.7:1"}); rec.Code != http.StatusOK {
		t.Fatalf("other ip = %d", rec.Code)
	}
}

// 다른 사이트의 폼 전송(쿠키는 붙어 옴)으로 쓰기 요청을 만들 수 없어야 한다.
func TestWriteRequiresJSON(t *testing.T) {
	f := newFixture(t)
	c := sessionCookie(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`}))
	for _, ct := range []string{"application/x-www-form-urlencoded", "text/plain", "multipart/form-data; boundary=x", "-"} {
		rec := f.do(call{method: "DELETE", path: "/api/session", cookie: c, contentType: ct})
		expectError(t, rec, http.StatusUnsupportedMediaType, "unsupported_media_type")
	}
	if rec := f.do(call{method: "DELETE", path: "/api/session", cookie: c, contentType: "application/json; charset=utf-8"}); rec.Code != http.StatusNoContent {
		t.Fatalf("json with charset = %d", rec.Code)
	}
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: `{"key":"x","extra":1}`}), http.StatusBadRequest, "malformed_json")
	big := `{"key":"` + strings.Repeat("a", 70<<10) + `"}`
	expectError(t, f.do(call{method: "POST", path: "/api/session", body: big}), http.StatusBadRequest, "malformed_json")
}

func TestAuditListing(t *testing.T) {
	f := newFixture(t)
	f.do(call{method: "POST", path: "/api/session", body: `{"key":"wrong"}`})
	f.do(call{method: "POST", path: "/api/session", body: `{"key":"` + adminKey + `"}`})
	type entry struct {
		ID       int64  `json:"id"`
		Actor    string `json:"actor"`
		Action   string `json:"action"`
		SourceIP string `json:"source_ip"`
	}
	resp := decodeBody[struct{ Entries []entry }](t, f.admin("GET", "/api/audit", ""))
	if len(resp.Entries) != 2 || resp.Entries[0].Action != "login" || resp.Entries[0].Actor != "admin" ||
		resp.Entries[1].Action != "login_denied" || resp.Entries[1].Actor != "anonymous" || resp.Entries[0].SourceIP == "" {
		t.Fatalf("entries = %+v", resp.Entries)
	}
	page := decodeBody[struct{ Entries []entry }](t, f.admin("GET", "/api/audit?limit=1&before_id="+itoa(resp.Entries[0].ID), ""))
	if len(page.Entries) != 1 || page.Entries[0].Action != "login_denied" {
		t.Fatalf("page = %+v", page.Entries)
	}
	expectError(t, f.admin("GET", "/api/audit?limit=0", ""), http.StatusBadRequest, "invalid_limit")
	expectError(t, f.admin("GET", "/api/audit?before_id=x", ""), http.StatusBadRequest, "invalid_before_id")
}

// 형식이 이상한 Host/X-Forwarded-Host는 공개 주소에 쓰지 않는다.
func TestPublicURLRejectsOddHosts(t *testing.T) {
	f := newFixture(t)
	cfg := func(c call) string {
		c.method, c.path = "GET", "/api/config"
		return decodeBody[map[string]string](t, f.do(c))["public_url"]
	}
	fwd := func(host string) call {
		return call{remote: "10.0.0.5:4000", headers: map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": host}}
	}
	if got := cfg(fwd("evil.example/x")); got != "https://example.com" {
		t.Fatalf("path in forwarded host: %q", got)
	}
	if got := cfg(fwd("a@b.example")); got != "https://example.com" {
		t.Fatalf("userinfo in forwarded host: %q", got)
	}
	if got := cfg(fwd("[2001:db8::1]:8443")); got != "https://[2001:db8::1]:8443" {
		t.Fatalf("ipv6 host: %q", got)
	}
	if got := cfg(call{headers: map[string]string{"Host": "bad host"}}); got != "" {
		t.Fatalf("invalid Host header: %q", got)
	}
}
```

`api_test.go`에 `func itoa(n int64) string { return strconv.FormatInt(n, 10) }`를 추가한다(이미 `strconv`를 쓴다).

- [ ] **Step 3: 실패 확인**

Run: `make test 2>&1 | grep -A5 internal/adminapi`
Expected: FAIL (`NewAuth` 등 정의 없음)

- [ ] **Step 4: 구현**

`internal/adminapi/auth.go`:

```go
package adminapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/session"
	"ssl-checker/internal/store"
)

const maxBodyBytes = 64 << 10

// Auth는 관리자 확인이다. 세션 쿠키나 Authorization: Bearer <ADMIN_KEY> 중 하나가 맞으면 관리자다.
// 틀린 키(로그인·Bearer)는 IP별로 5번까지 받고, 그 뒤로는 12초에 1번만 받는다.
type Auth struct {
	Sessions *session.Signer
	keyHash  [32]byte
	failures *httpx.Keyed
}

func NewAuth(adminKey string) *Auth {
	return &Auth{
		Sessions: session.New(adminKey),
		keyHash:  sha256.Sum256([]byte(adminKey)),
		failures: httpx.NewKeyed(12*time.Second, 5),
	}
}

// keyMatches는 해시끼리 상수 시간으로 비교해 길이도 드러내지 않는다.
func (a *Auth) keyMatches(key string) bool {
	sum := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare(sum[:], a.keyHash[:]) == 1
}

// check는 요청이 관리자인지 본다. wrongKey는 Bearer 키를 보냈는데 틀린 경우다(실패로 센다).
func (a *Auth) check(r *http.Request) (admin, wrongKey bool) {
	if c, err := r.Cookie(session.CookieName); err == nil && a.Sessions.Valid(c.Value) {
		return true, false
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return false, false
	}
	if a.keyMatches(token) {
		return true, false
	}
	return false, true
}

// admin은 관리자만 통과시킨다.
func (a *API) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := a.IP.From(r).String()
		if a.Auth.failures.Exhausted(ip) {
			httpx.WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
			return
		}
		ok, wrong := a.Auth.check(r)
		if wrong {
			a.Auth.failures.Allow(ip)
			a.Log.Warn("admin auth failed", "ip", ip) // 보낸 키는 남기지 않는다
		}
		if !ok {
			httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

// jsonOnly는 Content-Type: application/json인 요청만 받는다. 브라우저 폼은 이 타입을 보낼 수 없으므로
// 다른 사이트가 관리자의 쿠키로 쓰기 요청을 만들 수 없다. 본문은 maxBodyBytes까지만 읽는다.
func jsonOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mt != "application/json" {
			httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next(w, r)
	}
}

// write는 쓰기 경로용이다: 관리자 확인 후 JSON 요구.
func (a *API) write(next http.HandlerFunc) http.HandlerFunc { return a.admin(jsonOnly(next)) }

// decode는 본문을 엄격하게 읽는다(모르는 필드 거부). 본문이 비어 있으면 v를 그대로 둔다.
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// audit는 관리자 행동을 남긴다. Actor가 비면 admin이다. 클라이언트가 끊어도 기록되게 취소를 떼어 낸 ctx로 쓴다.
func (a *API) audit(r *http.Request, e store.AuditEntry) {
	if e.Actor == "" {
		e.Actor = "admin"
	}
	e.SourceIP = a.IP.From(r).String()
	if err := a.Store.Audit(context.WithoutCancel(r.Context()), e); err != nil {
		a.Log.Error("audit", "action", e.Action, "err", err)
	}
}
```

`internal/adminapi/session.go`:

```go
package adminapi

import (
	"net/http"
	"strings"
	"time"

	"ssl-checker/internal/httpx"
	"ssl-checker/internal/session"
	"ssl-checker/internal/store"
)

type loginRequest struct {
	Key string `json:"key"`
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	ip := a.IP.From(r).String()
	if a.Auth.failures.Exhausted(ip) {
		httpx.WriteError(w, http.StatusTooManyRequests, "too_many_attempts")
		return
	}
	var req loginRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	if !a.Auth.keyMatches(req.Key) {
		a.Auth.failures.Allow(ip)
		a.audit(r, store.AuditEntry{Actor: "anonymous", Action: "login_denied"})
		httpx.WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	value, exp := a.Auth.Sessions.Issue()
	http.SetCookie(w, a.cookie(r, value, exp.Sub(a.now())))
	a.audit(r, store.AuditEntry{Action: "login"})
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"admin": true})
}

func (a *API) sessionStatus(w http.ResponseWriter, r *http.Request) {
	ok, _ := a.Auth.check(r)
	httpx.WriteJSON(w, http.StatusOK, map[string]bool{"admin": ok})
}

// logout은 브라우저의 쿠키만 지운다. 서버에 세션이 없으므로 이미 복사된 쿠키는 만료까지 유효하다.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	c := a.cookie(r, "", 0)
	c.MaxAge = -1
	http.SetCookie(w, c)
	w.WriteHeader(http.StatusNoContent)
}

// cookie의 Secure는 공개 주소가 http면 뺀다. 붙이면 브라우저가 http로는 쿠키를 보내지 않아 로그인이 안 된다.
func (a *API) cookie(r *http.Request, value string, ttl time.Duration) *http.Cookie {
	return &http.Cookie{
		Name: session.CookieName, Value: value, Path: "/",
		MaxAge: int(ttl.Seconds()), HttpOnly: true, SameSite: http.SameSiteStrictMode,
		Secure: !strings.HasPrefix(a.PublicURLFor(r), "http://"),
	}
}
```

(쿠키 만료는 `MaxAge`로 준다. 테스트도 `MaxAge`를 본다.)

`internal/adminapi/audit.go`:

```go
package adminapi

import (
	"net/http"
	"strconv"
	"time"

	"ssl-checker/internal/httpx"
)

const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
)

type auditView struct {
	ID       int64     `json:"id"`
	At       time.Time `json:"at"`
	Actor    string    `json:"actor"`
	Action   string    `json:"action"`
	Target   string    `json:"target"`
	SourceIP string    `json:"source_ip"`
	Detail   string    `json:"detail"`
}

func (a *API) listAudit(w http.ResponseWriter, r *http.Request) {
	limit, ok := parseLimit(r, defaultAuditLimit, maxAuditLimit)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
	var before int64
	if v := r.URL.Query().Get("before_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_before_id")
			return
		}
		before = n
	}
	recs, err := a.Store.ListAudit(r.Context(), before, limit)
	if err != nil {
		a.dbError(w, "list audit", err)
		return
	}
	out := make([]auditView, len(recs))
	for i, e := range recs {
		out[i] = auditView{ID: e.ID, At: e.At, Actor: e.Actor, Action: e.Action, Target: e.Target, SourceIP: e.SourceIP, Detail: e.Detail}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"entries": out})
}
```

`internal/adminapi/api.go`:

1. `API`에 필드 추가: `Auth *Auth // 관리자 확인`.
2. `Register`를 경로 표 기반으로 바꾼다:

```go
type route struct {
	method, pattern string
	h               http.HandlerFunc
}

// routes는 /api 경로 전체다. 조회는 누구나, 쓰기와 감사 로그는 관리자만.
func (a *API) routes() []route {
	return []route{
		{"GET", "/api/summary", a.summary},
		{"GET", "/api/domains", a.listDomains},
		{"GET", "/api/domains/{id}", a.getDomain},
		{"GET", "/api/targets/{id}/results", a.targetResults},
		{"GET", "/api/config", a.config},
		{"GET", "/api/session", a.sessionStatus},
		{"POST", "/api/session", jsonOnly(a.login)},
		{"DELETE", "/api/session", jsonOnly(a.logout)},
		{"GET", "/api/audit", a.admin(a.listAudit)},
	}
}

// Register는 /api 경로를 mux에 붙인다. 모든 /api 응답은 JSON이다: 없는 경로는 404,
// 있는 경로에 받지 않는 메서드는 405(Allow 헤더 포함).
func (a *API) Register(mux *http.ServeMux) {
	allow := map[string][]string{}
	probe := http.NewServeMux() // 메서드 없이 경로만 등록해, catch-all에서 "경로는 있는데 메서드가 다름"을 가린다
	for _, rt := range a.routes() {
		mux.HandleFunc(rt.method+" "+rt.pattern, rt.h)
		if _, seen := allow[rt.pattern]; !seen {
			probe.HandleFunc(rt.pattern, func(http.ResponseWriter, *http.Request) {})
		}
		allow[rt.pattern] = append(allow[rt.pattern], rt.method)
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := probe.Handler(r); pattern != "" {
			methods := allow[pattern]
			if slices.Contains(methods, http.MethodGet) {
				methods = append(slices.Clone(methods), http.MethodHead)
			}
			w.Header().Set("Allow", strings.Join(methods, ", "))
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed")
			return
		}
		httpx.WriteError(w, http.StatusNotFound, "not_found")
	})
}
```

(import에 `"slices"` 추가. 기존 probe 코드는 지운다.)

3. `targetResults`의 limit 파싱을 공통 도우미로 바꾼다:

```go
// parseLimit은 ?limit= 값이다. 없으면 def, max보다 크면 max로 줄인다. 1보다 작거나 숫자가 아니면 ok=false.
func parseLimit(r *http.Request, def, max int) (int, bool) {
	v := r.URL.Query().Get("limit")
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, false
	}
	return min(n, max), true
}
```

`targetResults`:

```go
	limit, ok := parseLimit(r, defaultResultLimit, maxResultLimit)
	if !ok {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_limit")
		return
	}
```

4. `PublicURLFor`에 호스트 검증을 넣는다:

```go
// hostRE는 이름[:포트] 또는 [IPv6][:포트]다. 공개 주소는 자격 증명 안내에 들어가므로 경로·사용자 정보·공백이 섞인 값은 받지 않는다.
var hostRE = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)

// PublicURLFor는 클라이언트가 api에 닿는 주소다. PUBLIC_URL이 있으면 그 값, 없으면 요청에서 만든다.
// X-Forwarded-Proto/Host는 신뢰하는 프록시에서 직접 온 요청일 때만 믿는다. 쓸 수 있는 호스트가 없으면 빈 문자열이다.
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
		if h := firstValue(r.Header.Get("X-Forwarded-Host")); hostRE.MatchString(h) {
			host = h
		}
	}
	if !hostRE.MatchString(host) {
		return ""
	}
	return scheme + "://" + host
}
```

(import에 `"regexp"` 추가.)

- [ ] **Step 5: 통과 확인**

Run: `gofmt -l internal/ && make test && go vet ./...`
Expected: 전체 PASS (계획 1의 adminapi 테스트 포함)

- [ ] **Step 6: 커밋**

```bash
git add internal/adminapi
git commit -m "feat: admin session login, admin checks, and audit log API" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: 도메인·대상 쓰기 API

**Files:**
- Create: `internal/adminapi/validate.go`, `internal/adminapi/write.go`, `internal/adminapi/write_test.go`
- Modify: `internal/adminapi/api.go` (`API.Svc`, `API.Runner` 필드, `routes()`에 줄 추가, `getDomain`을 `loadDetail`로 공통화, `dbError` 옆에 `serviceError`), `internal/adminapi/api_test.go`(fixture에 `Svc`, `Runner`, `mem`)

**Interfaces:**
- Consumes: `API.write`, `decode`, `audit`, `routes` (Task 5); `store.CreateDomain`, `UpdateDomainMemo`, `DeleteDomain`, `CreateTarget`, `UpdateTarget`, `DeleteTarget`, `GetTarget`, `GetDomain`, `ACMEStateByDomain`, `TargetFields`, `ErrDomainExists`, `ErrTargetExists`, `ErrNotFound` (Task 3·계획 1); `acmedns.Service.Disable` (Task 4); `acmedns.ValidDomain`; `netutil.NormalizeName`; `monitor.Runner.Check`, `monitor.AssessTarget`; `dnsprovider.Error`.
- Produces:
  - `API.Svc *acmedns.Service`, `API.Runner *monitor.Runner`
  - `func (a *API) loadDetail(ctx context.Context, id int64) (domainDetail, error)` (없으면 `store.ErrNotFound`)
  - `func (a *API) serviceError(w http.ResponseWriter, op string, err error)` — `*dnsprovider.Error`면 500 `dns_provider_error`, 아니면 500 `db_error`
  - `func singleTargetView(t store.Target, rs []monitor.Result) targetView`
  - 경로: `POST /api/domains`, `PATCH /api/domains/{id}`, `DELETE /api/domains/{id}`, `POST /api/domains/{id}/targets`, `PATCH /api/targets/{id}`, `DELETE /api/targets/{id}`, `POST /api/targets/{id}/check`
  - 상수 `maxMemoLen = 500`, `maxTargetIPs = 16`

- [ ] **Step 1: fixture 확장**

`internal/adminapi/api_test.go`:
- `fixture`에 `mem *memory.Provider`, `svc *acmedns.Service` 필드를 더한다.
- `newFixture`에서:

```go
	pool := dbtest.New(t)
	st := store.New(pool)
	mem := memory.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := &acmedns.Service{Store: st, Provider: mem, Zone: "acme.test", TTL: 60}
	api := &adminapi.API{
		Store: st, Svc: svc, Zone: "acme.test", CheckInterval: 6 * time.Hour,
		Runner: &monitor.Runner{Prober: &monitor.Prober{Timeout: 300 * time.Millisecond}, Sink: st, Log: log},
		Auth:   adminapi.NewAuth(adminKey),
		IP:     httpx.ClientIP{Trusted: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}},
		Now:    func() time.Time { return now }, Log: log,
	}
	mux := http.NewServeMux()
	api.Register(mux)
	return &fixture{t: t, st: st, api: api, h: mux, mem: mem, svc: svc}
```

(import에 `"ssl-checker/internal/acmedns"`, `"ssl-checker/internal/dnsprovider/memory"` 추가.)

- [ ] **Step 2: 실패하는 테스트 작성**

`internal/adminapi/write_test.go`:

```go
package adminapi_test

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/monitor"
)

type detailResp struct {
	ID      int64  `json:"id"`
	FQDN    string `json:"fqdn"`
	Memo    string `json:"memo"`
	Status  string `json:"status"`
	Targets []struct {
		ID      int64    `json:"id"`
		IPs     []string `json:"ips"`
		Results []struct {
			Address string `json:"address"`
		} `json:"results"`
	} `json:"targets"`
	ACME map[string]any `json:"acme"`
}

type targetResp struct {
	ID       int64    `json:"id"`
	Hostname string   `json:"hostname"`
	Port     int      `json:"port"`
	SNI      string   `json:"sni"`
	IPs      []string `json:"ips"`
	Memo     string   `json:"memo"`
	Status   string   `json:"status"`
	Reasons  []string `json:"reasons"`
	Results  []struct {
		Address string `json:"address"`
		Status  string `json:"status"`
		ChainOK bool   `json:"chain_ok"`
	} `json:"results"`
}

// closedPort는 아무도 듣지 않는 로컬 포트다. 접속하면 바로 거부된다.
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

func (f *fixture) createDomain(fqdn string) detailResp {
	f.t.Helper()
	rec := f.admin("POST", "/api/domains", `{"fqdn":"`+fqdn+`"}`)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("create domain = %d %s", rec.Code, rec.Body)
	}
	return decodeBody[detailResp](f.t, rec)
}

func TestWritesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	for _, c := range []call{
		{method: "POST", path: "/api/domains", body: `{"fqdn":"a.com"}`},
		{method: "PATCH", path: "/api/domains/1", body: `{}`},
		{method: "DELETE", path: "/api/domains/1"},
		{method: "POST", path: "/api/domains/1/targets", body: `{}`},
		{method: "PATCH", path: "/api/targets/1", body: `{}`},
		{method: "DELETE", path: "/api/targets/1"},
		{method: "POST", path: "/api/targets/1/check"},
	} {
		expectError(t, f.do(c), http.StatusUnauthorized, "unauthorized")
	}
}

func TestDomainCRUD(t *testing.T) {
	f := newFixture(t)
	rec := f.admin("POST", "/api/domains", `{"fqdn":" A.com. ","memo":"main"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	d := decodeBody[detailResp](t, rec)
	if d.FQDN != "a.com" || d.Memo != "main" || d.Status != "unknown" || len(d.Targets) != 0 || d.ACME["enabled"] != false {
		t.Fatalf("created = %+v", d)
	}
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"a.com"}`), http.StatusConflict, "domain_exists")
	for _, bad := range []string{`{"fqdn":"*.a.com"}`, `{"fqdn":"com"}`, `{"fqdn":""}`, `{"fqdn":"a b.com"}`} {
		expectError(t, f.admin("POST", "/api/domains", bad), http.StatusBadRequest, "invalid_domain")
	}
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"b.com","memo":"`+strings.Repeat("가", 501)+`"}`), http.StatusBadRequest, "invalid_memo")
	expectError(t, f.admin("POST", "/api/domains", `{"fqdn":"b.com","bogus":1}`), http.StatusBadRequest, "malformed_json")

	path := fmt.Sprintf("/api/domains/%d", d.ID)
	if got := decodeBody[detailResp](t, f.admin("PATCH", path, `{"memo":"edited"}`)); got.Memo != "edited" {
		t.Fatalf("patched = %+v", got)
	}
	if got := decodeBody[detailResp](t, f.admin("PATCH", path, `{}`)); got.Memo != "edited" {
		t.Fatalf("empty patch changed memo: %+v", got)
	}
	expectError(t, f.admin("PATCH", "/api/domains/99999", `{"memo":"x"}`), http.StatusNotFound, "not_found")

	if rec := f.admin("DELETE", path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	expectError(t, f.get(path), http.StatusNotFound, "not_found")
	expectError(t, f.admin("DELETE", path, ""), http.StatusNotFound, "not_found")

	actions := auditActions(t, f)
	for _, want := range []string{"domain_delete", "domain_update", "domain_create"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}

// ACME가 켜진 도메인을 지우면 공급자의 챌린지 TXT도 지운다.
func TestDeleteDomainDisablesACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	ctx := context.Background()
	reg, err := f.svc.Register(ctx, "a.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	acct, err := f.svc.Authenticate(ctx, reg.Username, reg.Password)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.UpdateTXT(ctx, acct, strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	if rec := f.admin("DELETE", fmt.Sprintf("/api/domains/%d", d.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if v := f.mem.Values(reg.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
}

func TestDeleteDomainProviderFailure(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	if _, err := f.svc.Register(context.Background(), "a.com", nil); err != nil {
		t.Fatal(err)
	}
	f.mem.SetFailure(errors.New("cloudflare down"))
	expectError(t, f.admin("DELETE", fmt.Sprintf("/api/domains/%d", d.ID), ""), http.StatusInternalServerError, "dns_provider_error")
	f.mem.SetFailure(nil)
	if got := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID))); got.ACME["enabled"] != true {
		t.Fatalf("domain or acme lost: %+v", got)
	}
}

// 대상을 만들면 그 자리에서 점검하고 결과를 돌려준다. 접속이 안 되는 IP가 여러 개여도 빨리 끝난다.
func TestCreateTargetChecksImmediately(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	port := closedPort(t)
	body := fmt.Sprintf(`{"hostname":"WWW.a.com.","port":%d,"ips":["127.0.0.1"," 127.0.0.1 ","::ffff:127.0.0.1"],"memo":"web"}`, port)
	start := time.Now()
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), body)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("create took %s", time.Since(start))
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("create target = %d %s", rec.Code, rec.Body)
	}
	tg := decodeBody[targetResp](t, rec)
	if tg.Hostname != "www.a.com" || tg.Port != port || tg.Memo != "web" || len(tg.IPs) != 1 || tg.IPs[0] != "127.0.0.1" {
		t.Fatalf("target = %+v (IPs should be trimmed, unmapped and deduplicated)", tg)
	}
	if len(tg.Results) != 1 || tg.Results[0].Status != "critical" || tg.Status != "critical" || tg.Reasons[0] != monitor.CodeConnectFailed {
		t.Fatalf("results = %+v", tg)
	}
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), body), http.StatusConflict, "target_exists")
	expectError(t, f.admin("POST", "/api/domains/99999/targets", body), http.StatusNotFound, "not_found")
}

func TestCreateTargetValidation(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	path := fmt.Sprintf("/api/domains/%d/targets", d.ID)
	var seventeen []string
	for i := range 17 {
		seventeen = append(seventeen, fmt.Sprintf(`"10.0.0.%d"`, i+1))
	}
	many := strings.Join(seventeen, ",")
	for _, bad := range []string{
		`{}`,
		`{"hostname":""}`,
		`{"hostname":"*.a.com"}`,
		`{"hostname":"a..com"}`,
		`{"hostname":"a.com","port":0}`,
		`{"hostname":"a.com","port":65536}`,
		`{"hostname":"a.com","sni":"10.0.0.1"}`,
		`{"hostname":"a.com","sni":"*.a.com"}`,
		`{"hostname":"a.com","ips":["not-an-ip"]}`,
		`{"hostname":"a.com","ips":["fe80::1%eth0"]}`,
		`{"hostname":"a.com","ips":[` + many + `]}`,
		`{"hostname":"a.com","memo":"` + strings.Repeat("x", 501) + `"}`,
	} {
		expectError(t, f.admin("POST", path, bad), http.StatusBadRequest, "invalid_target")
	}
	// 내부 이름(한 단어)과 IP 리터럴 호스트명은 받는다.
	for _, ok := range []string{`{"hostname":"intranet","ips":["127.0.0.1"],"port":1}`, `{"hostname":"10.0.0.9","ips":["127.0.0.1"],"port":2}`} {
		if rec := f.admin("POST", path, ok); rec.Code != http.StatusCreated {
			t.Errorf("%s = %d %s", ok, rec.Code, rec.Body)
		}
	}
}

// 실제 TLS 서버를 대상으로 만들면 결과가 정상으로 나온다.
func TestCreateTargetAgainstTLSServer(t *testing.T) {
	f := newFixture(t)
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	f.api.Runner.Prober.Roots = roots
	d := f.createDomain("example.com")
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID), fmt.Sprintf(`{"hostname":"example.com","port":%d,"ips":["127.0.0.1"]}`, port))
	tg := decodeBody[targetResp](t, rec)
	if rec.Code != http.StatusCreated || len(tg.Results) != 1 || !tg.Results[0].ChainOK || tg.Results[0].Status == "critical" {
		t.Fatalf("target = %d %+v", rec.Code, tg)
	}
}

// 접속 설정을 바꾸면 그 자리에서 다시 점검해, 옛 IP 결과가 섞이지 않는다.
func TestPatchTargetRechecks(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	port := closedPort(t)
	created := decodeBody[targetResp](t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID),
		fmt.Sprintf(`{"hostname":"www.a.com","port":%d,"ips":["127.0.0.1"]}`, port)))
	path := fmt.Sprintf("/api/targets/%d", created.ID)

	rec := f.admin("PATCH", path, `{"ips":["127.0.0.2"]}`)
	got := decodeBody[targetResp](t, rec)
	want := fmt.Sprintf("127.0.0.2:%d", port)
	if rec.Code != http.StatusOK || len(got.Results) != 1 || got.Results[0].Address != want {
		t.Fatalf("patched = %d %+v", rec.Code, got)
	}
	detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID)))
	if len(detail.Targets) != 1 || len(detail.Targets[0].Results) != 1 || detail.Targets[0].Results[0].Address != want {
		t.Fatalf("detail after patch = %+v", detail.Targets)
	}

	// 메모만 바꾸면 다시 점검하지 않는다(결과 개수가 그대로).
	before := len(history(t, f, created.ID))
	if rec := f.admin("PATCH", path, `{"memo":"note"}`); rec.Code != http.StatusOK || decodeBody[targetResp](t, rec).Memo != "note" {
		t.Fatalf("memo patch = %d %s", rec.Code, rec.Body)
	}
	if after := len(history(t, f, created.ID)); after != before {
		t.Fatalf("memo-only patch ran a check: %d -> %d", before, after)
	}
	expectError(t, f.admin("PATCH", path, `{"port":0}`), http.StatusBadRequest, "invalid_target")
	expectError(t, f.admin("PATCH", "/api/targets/99999", `{"memo":"x"}`), http.StatusNotFound, "not_found")
}

func TestCheckAndDeleteTarget(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	created := decodeBody[targetResp](t, f.admin("POST", fmt.Sprintf("/api/domains/%d/targets", d.ID),
		fmt.Sprintf(`{"hostname":"www.a.com","port":%d,"ips":["127.0.0.1"]}`, closedPort(t))))
	rec := f.admin("POST", fmt.Sprintf("/api/targets/%d/check", created.ID), "")
	resp := decodeBody[struct {
		Results []struct{ Status string } `json:"results"`
	}](t, rec)
	if rec.Code != http.StatusOK || len(resp.Results) != 1 {
		t.Fatalf("check = %d %s", rec.Code, rec.Body)
	}
	if n := len(history(t, f, created.ID)); n != 2 {
		t.Fatalf("history = %d, want 2 (create + check)", n)
	}
	expectError(t, f.admin("POST", "/api/targets/99999/check", ""), http.StatusNotFound, "not_found")
	if rec := f.admin("DELETE", fmt.Sprintf("/api/targets/%d", created.ID), ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	expectError(t, f.admin("DELETE", fmt.Sprintf("/api/targets/%d", created.ID), ""), http.StatusNotFound, "not_found")
	actions := auditActions(t, f)
	for _, want := range []string{"target_create", "target_check", "target_delete"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}

func history(t *testing.T, f *fixture, targetID int64) []any {
	t.Helper()
	return decodeBody[struct {
		Results []any `json:"results"`
	}](t, f.get(fmt.Sprintf("/api/targets/%d/results", targetID))).Results
}

func auditActions(t *testing.T, f *fixture) []string {
	t.Helper()
	resp := decodeBody[struct {
		Entries []struct{ Action string } `json:"entries"`
	}](t, f.admin("GET", "/api/audit", ""))
	out := make([]string, len(resp.Entries))
	for i, e := range resp.Entries {
		out[i] = e.Action
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
```

`TestPatchTargetRechecks`의 `127.0.0.2`는 macOS에서 접속이 늘어질 수 있으나 fixture의 Prober Timeout이 300ms라 괜찮다.

- [ ] **Step 3: 실패 확인**

Run: `make test 2>&1 | grep -A5 internal/adminapi`
Expected: FAIL (정의 없음 또는 405/404)

- [ ] **Step 4: 구현**

`internal/adminapi/validate.go`:

```go
package adminapi

import (
	"net/netip"
	"regexp"
	"strings"
	"unicode/utf8"

	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const (
	maxMemoLen   = 500
	maxTargetIPs = 16
)

var labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func validMemo(s string) bool { return utf8.RuneCountInString(s) <= maxMemoLen }

// validHostname은 점검할 호스트 이름이다. 내부 이름(한 단어)과 IP 리터럴도 받고, 와일드카드는 받지 않는다.
func validHostname(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return true
	}
	if s == "" || len(s) > 253 {
		return false
	}
	for _, l := range strings.Split(s, ".") {
		if !labelRE.MatchString(l) {
			return false
		}
	}
	return true
}

// validSNI는 TLS SNI로 보낼 이름이다. SNI에는 IP를 쓸 수 없다.
func validSNI(s string) bool {
	if _, err := netip.ParseAddr(s); err == nil {
		return false
	}
	return validHostname(s)
}

// parseIPs는 공백을 지우고, IPv4-mapped IPv6를 IPv4로 바꾸고, 중복을 뺀다. 존(%eth0)이 붙은 주소는 받지 않는다.
func parseIPs(ss []string) ([]netip.Addr, bool) {
	out := make([]netip.Addr, 0, len(ss))
	for _, s := range ss {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || a.Zone() != "" {
			return nil, false
		}
		a = a.Unmap()
		if !slicesContains(out, a) {
			out = append(out, a)
		}
	}
	return out, len(out) <= maxTargetIPs
}

func slicesContains(as []netip.Addr, a netip.Addr) bool {
	for _, x := range as {
		if x == a {
			return true
		}
	}
	return false
}

// targetRequest는 대상 추가·수정 본문이다. 수정에서는 보낸 필드만 바꾼다.
type targetRequest struct {
	Hostname *string   `json:"hostname"`
	Port     *int      `json:"port"`
	SNI      *string   `json:"sni"`
	IPs      *[]string `json:"ips"`
	Memo     *string   `json:"memo"`
}

// apply는 요청 값을 base에 덮어쓰고 검사한다.
func (req targetRequest) apply(base store.TargetFields) (store.TargetFields, bool) {
	f := base
	if req.Hostname != nil {
		f.Hostname = netutil.NormalizeName(*req.Hostname)
	}
	if req.Port != nil {
		f.Port = *req.Port
	}
	if req.SNI != nil {
		f.SNI = netutil.NormalizeName(*req.SNI)
	}
	if req.IPs != nil {
		ips, ok := parseIPs(*req.IPs)
		if !ok {
			return f, false
		}
		f.IPs = ips
	}
	if req.Memo != nil {
		f.Memo = *req.Memo
	}
	ok := validHostname(f.Hostname) && f.Port >= 1 && f.Port <= 65535 &&
		(f.SNI == "" || validSNI(f.SNI)) && validMemo(f.Memo)
	return f, ok
}

// connectionChanged는 접속 결과에 영향을 주는 값이 바뀌었는지 본다(메모만 바뀌면 다시 점검하지 않는다).
func connectionChanged(a, b store.TargetFields) bool {
	if a.Hostname != b.Hostname || a.Port != b.Port || a.SNI != b.SNI || len(a.IPs) != len(b.IPs) {
		return true
	}
	for i := range a.IPs {
		if a.IPs[i] != b.IPs[i] {
			return true
		}
	}
	return false
}
```

(`slicesContains` 대신 표준 `slices.Contains`를 써도 된다 — `netip.Addr`는 comparable이다. 그 경우 도우미를 지운다.)

`internal/adminapi/api.go`:

1. `API`에 필드 추가:

```go
	Svc    *acmedns.Service // ACME 켜기·재발급·끄기
	Runner *monitor.Runner  // "지금 점검"과 대상 추가·수정 직후 점검
```

2. `getDomain`의 본문을 `loadDetail`로 옮긴다:

```go
// loadDetail은 도메인 상세 응답이다. 도메인이 없으면 store.ErrNotFound다.
func (a *API) loadDetail(ctx context.Context, id int64) (domainDetail, error) {
	d, err := a.Store.GetDomain(ctx, id)
	if err != nil {
		return domainDetail{}, err
	}
	targets, err := a.Store.TargetsByDomain(ctx, id)
	if err != nil {
		return domainDetail{}, err
	}
	latest, err := a.Store.LatestResults(ctx, targets)
	if err != nil {
		return domainDetail{}, err
	}
	var acme *store.ACMEState
	switch st, err := a.Store.ACMEStateByDomain(ctx, id); {
	case err == nil:
		acme = &st
	case !errors.Is(err, store.ErrNotFound):
		return domainDetail{}, err
	}
	overall, per := assess(targets, latest, acme)
	return domainDetail{
		ID: d.ID, FQDN: d.FQDN, Memo: d.Memo, CreatedAt: d.CreatedAt, Assessment: overall,
		Targets: targetViews(targets, latest, per), ACME: a.acmeView(d.FQDN, acme),
	}, nil
}

func (a *API) getDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

// writeDetail은 도메인 상세를 status로 응답한다.
func (a *API) writeDetail(w http.ResponseWriter, r *http.Request, status int, id int64) {
	detail, err := a.loadDetail(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return
	}
	httpx.WriteJSON(w, status, detail)
}

// serviceError는 ACME 작업 실패를 응답한다. DNS 공급자 실패와 DB 실패를 구분한다.
func (a *API) serviceError(w http.ResponseWriter, op string, err error) {
	a.Log.Error(op, "err", err)
	var perr *dnsprovider.Error
	if errors.As(err, &perr) {
		httpx.WriteError(w, http.StatusInternalServerError, "dns_provider_error")
		return
	}
	httpx.WriteError(w, http.StatusInternalServerError, "db_error")
}
```

(import에 `"ssl-checker/internal/acmedns"`, `"ssl-checker/internal/dnsprovider"`.)

3. `routes()`에 줄 추가:

```go
		{"POST", "/api/domains", a.write(a.createDomain)},
		{"PATCH", "/api/domains/{id}", a.write(a.patchDomain)},
		{"DELETE", "/api/domains/{id}", a.write(a.deleteDomain)},
		{"POST", "/api/domains/{id}/targets", a.write(a.createTarget)},
		{"PATCH", "/api/targets/{id}", a.write(a.patchTarget)},
		{"DELETE", "/api/targets/{id}", a.write(a.deleteTarget)},
		{"POST", "/api/targets/{id}/check", a.write(a.checkTarget)},
```

`internal/adminapi/view.go`에 추가:

```go
func singleTargetView(t store.Target, rs []monitor.Result) targetView {
	return targetViews([]store.Target{t}, map[int64][]monitor.Result{t.ID: rs}, []monitor.Assessment{monitor.AssessTarget(rs)})[0]
}
```

`internal/adminapi/write.go`:

```go
package adminapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/monitor"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

type domainRequest struct {
	FQDN string `json:"fqdn"`
	Memo string `json:"memo"`
}

func (a *API) createDomain(w http.ResponseWriter, r *http.Request) {
	var req domainRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	fqdn := netutil.NormalizeName(req.FQDN)
	if !acmedns.ValidDomain(fqdn) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	}
	if !validMemo(req.Memo) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_memo")
		return
	}
	d, err := a.Store.CreateDomain(r.Context(), fqdn, req.Memo)
	if errors.Is(err, store.ErrDomainExists) {
		httpx.WriteError(w, http.StatusConflict, "domain_exists")
		return
	}
	if err != nil {
		a.dbError(w, "create domain", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "domain_create", Target: fqdn})
	a.writeDetail(w, r, http.StatusCreated, d.ID)
}

func (a *API) patchDomain(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req struct {
		Memo *string `json:"memo"`
	}
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	if req.Memo != nil {
		if !validMemo(*req.Memo) {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_memo")
			return
		}
		d, err := a.Store.UpdateDomainMemo(r.Context(), id, *req.Memo)
		if errors.Is(err, store.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "not_found")
			return
		}
		if err != nil {
			a.dbError(w, "update domain", err)
			return
		}
		a.audit(r, store.AuditEntry{Action: "domain_update", Target: d.FQDN})
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

// deleteDomain은 ACME가 켜져 있으면 먼저 꺼서 공급자의 챌린지 TXT를 지운 뒤 도메인을 지운다.
// 공급자 삭제가 실패하면 아무것도 지우지 않는다.
func (a *API) deleteDomain(w http.ResponseWriter, r *http.Request) {
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
	st, err := a.Store.ACMEStateByDomain(ctx, id)
	switch {
	case err == nil:
		if err := a.Svc.Disable(ctx, st.AccountID); err != nil && !errors.Is(err, store.ErrNotFound) {
			a.serviceError(w, "delete domain: disable acme", err)
			return
		}
	case !errors.Is(err, store.ErrNotFound):
		a.dbError(w, "delete domain: acme state", err)
		return
	}
	if err := a.Store.DeleteDomain(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.dbError(w, "delete domain", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "domain_delete", Target: d.FQDN})
	w.WriteHeader(http.StatusNoContent)
}

func targetName(f store.TargetFields) string { return net.JoinHostPort(f.Hostname, strconv.Itoa(f.Port)) }

func fieldsOf(t store.Target) store.TargetFields {
	return store.TargetFields{Hostname: t.Hostname, Port: t.Port, SNI: t.SNI, IPs: t.IPs, Memo: t.Memo}
}

// checkNow는 대상을 바로 점검하고 저장한다. 저장에 실패해도 이번 결과는 돌려준다(대상 추가·수정 응답용).
func (a *API) checkNow(r *http.Request, t store.Target) []monitor.Result {
	rs, err := a.Runner.Check(r.Context(), t.Target)
	if err != nil {
		a.Log.Error("check target", "target", t.ID, "err", err)
	}
	return rs
}

func (a *API) createTarget(w http.ResponseWriter, r *http.Request) {
	domainID, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req targetRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	f, valid := req.apply(store.TargetFields{Port: 443})
	if req.Hostname == nil || !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	t, err := a.Store.CreateTarget(r.Context(), store.NewTarget{
		DomainID: domainID, Hostname: f.Hostname, Port: f.Port, SNI: f.SNI, IPs: f.IPs, Memo: f.Memo,
	})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrTargetExists):
		httpx.WriteError(w, http.StatusConflict, "target_exists")
		return
	case err != nil:
		a.dbError(w, "create target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_create", Target: targetName(f), Detail: fmt.Sprintf("domain_id=%d", domainID)})
	httpx.WriteJSON(w, http.StatusCreated, singleTargetView(t, a.checkNow(r, t)))
}

// patchTarget은 보낸 필드만 바꾼다. 접속 설정(hostname·port·sni·ips)이 바뀌면 그 자리에서 다시 점검한다.
func (a *API) patchTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	var req targetRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	ctx := r.Context()
	old, err := a.Store.GetTarget(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	f, valid := req.apply(fieldsOf(old))
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_target")
		return
	}
	t, err := a.Store.UpdateTarget(ctx, id, f)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, store.ErrTargetExists):
		httpx.WriteError(w, http.StatusConflict, "target_exists")
		return
	case err != nil:
		a.dbError(w, "update target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_update", Target: targetName(f)})
	var rs []monitor.Result
	if connectionChanged(fieldsOf(old), f) {
		rs = a.checkNow(r, t)
	} else {
		latest, err := a.Store.LatestResults(ctx, []store.Target{t})
		if err != nil {
			a.dbError(w, "target results", err)
			return
		}
		rs = latest[t.ID]
	}
	httpx.WriteJSON(w, http.StatusOK, singleTargetView(t, rs))
}

func (a *API) deleteTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	ctx := r.Context()
	t, err := a.Store.GetTarget(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	if err := a.Store.DeleteTarget(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
		a.dbError(w, "delete target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_delete", Target: targetName(fieldsOf(t))})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) checkTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	t, err := a.Store.GetTarget(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		a.dbError(w, "get target", err)
		return
	}
	rs, err := a.Runner.Check(r.Context(), t.Target)
	if err != nil {
		a.dbError(w, "check target", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "target_check", Target: targetName(fieldsOf(t))})
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"results": rs})
}
```

- [ ] **Step 5: 통과 확인**

Run: `gofmt -l internal/ && make test && go vet ./...`
Expected: 전체 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/adminapi
git commit -m "feat: admin API for domains and monitoring targets" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: ACME 관리 API와 일회성 자격 증명

**Files:**
- Create: `internal/adminapi/acme.go`, `internal/adminapi/acme_test.go`
- Modify: `internal/adminapi/api.go` (`API.CNAME` 필드, `routes()`에 줄 추가), `internal/adminapi/view.go` (`cnameRecord` 도우미), `internal/adminapi/api_test.go` (fixture에 `CNAME`)

**Interfaces:**
- Consumes: `API.write`, `decode`, `audit`, `serviceError`, `PublicURLFor` (Task 5·6); `acmedns.Service.Register`, `Rotate`, `Disable`, `Registration`, `ErrInvalidDomain`; `store.ErrActiveAccountExists`, `SetAllowFrom`, `SetCNAMEStatus`, `ACMEStateByDomain`, `GetDomain`; `cnamecheck.Checker.Check`, `cnamecheck.ChallengeName`; `netutil.ParsePrefix`; 테스트용 `cnametest.Serve`.
- Produces:
  - `API.CNAME *cnamecheck.Checker`
  - 경로: `POST /api/domains/{id}/acme`(201), `PATCH /api/domains/{id}/acme`(200), `POST /api/domains/{id}/acme/rotate`(200), `POST /api/domains/{id}/acme/check`(200), `DELETE /api/domains/{id}/acme`(204)
  - 자격 증명 응답 `{"username","password","subdomain","fulldomain","cname_record","allowfrom","acme_sh"}`, `Cache-Control: no-store`
  - `const maxAllowFrom = 64`

- [ ] **Step 1: fixture 확장**

`newFixture`의 `api` 리터럴에 `CNAME: &cnamecheck.Checker{Resolver: "127.0.0.1:1", Timeout: 200 * time.Millisecond},`를 더한다(테스트마다 필요하면 `f.api.CNAME`을 바꾼다).

- [ ] **Step 2: 실패하는 테스트 작성**

`internal/adminapi/acme_test.go`:

```go
package adminapi_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/cnamecheck"
	"ssl-checker/internal/cnamecheck/cnametest"
)

type credsResp struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Subdomain   string   `json:"subdomain"`
	FullDomain  string   `json:"fulldomain"`
	CNAMERecord string   `json:"cname_record"`
	AllowFrom   []string `json:"allowfrom"`
	AcmeSh      string   `json:"acme_sh"`
}

func (f *fixture) enableACME(domainID int64, body string) credsResp {
	f.t.Helper()
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", domainID), body)
	if rec.Code != http.StatusCreated {
		f.t.Fatalf("enable = %d %s", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		f.t.Fatalf("Cache-Control = %q", cc)
	}
	return decodeBody[credsResp](f.t, rec)
}

func TestACMEWritesRequireAdmin(t *testing.T) {
	f := newFixture(t)
	for _, c := range []call{
		{method: "POST", path: "/api/domains/1/acme", body: `{}`},
		{method: "PATCH", path: "/api/domains/1/acme", body: `{}`},
		{method: "POST", path: "/api/domains/1/acme/rotate"},
		{method: "POST", path: "/api/domains/1/acme/check"},
		{method: "DELETE", path: "/api/domains/1/acme"},
	} {
		expectError(t, f.do(c), http.StatusUnauthorized, "unauthorized")
	}
}

func TestEnableACME(t *testing.T) {
	f := newFixture(t)
	f.api.PublicURL = "https://ssl.example.com"
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{"allowfrom":[" 10.0.1.0/24 ","10.0.2.5"]}`)
	full := c.Subdomain + ".acme.test"
	if c.FullDomain != full || c.CNAMERecord != "_acme-challenge.a.com. CNAME "+full+"." || len(c.Password) != 40 ||
		len(c.AllowFrom) != 2 || c.AllowFrom[0] != "10.0.1.0/24" || c.AllowFrom[1] != "10.0.2.5/32" {
		t.Fatalf("creds = %+v", c)
	}
	for _, want := range []string{
		`export ACMEDNS_BASE_URL="https://ssl.example.com"`,
		`export ACMEDNS_USERNAME="` + c.Username + `"`,
		`export ACMEDNS_PASSWORD="` + c.Password + `"`,
		`export ACMEDNS_SUBDOMAIN="` + c.Subdomain + `"`,
		`--issue --dns dns_acmedns -d a.com -d '*.a.com'`,
		`--install-cert -d a.com --ecc`,
	} {
		if !strings.Contains(c.AcmeSh, want) {
			t.Errorf("acme_sh missing %q:\n%s", want, c.AcmeSh)
		}
	}
	if _, err := f.svc.Authenticate(context.Background(), c.Username, c.Password); err != nil {
		t.Fatalf("issued credentials do not authenticate: %v", err)
	}
	detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID)))
	if detail.ACME["enabled"] != true || detail.ACME["username"] != c.Username || strings.Contains(fmt.Sprint(detail.ACME), c.Password) {
		t.Fatalf("detail acme = %v", detail.ACME)
	}
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", d.ID), `{}`), http.StatusConflict, "acme_enabled")
	expectError(t, f.admin("POST", "/api/domains/99999/acme", `{}`), http.StatusNotFound, "not_found")
	other := f.createDomain("b.com")
	expectError(t, f.admin("POST", fmt.Sprintf("/api/domains/%d/acme", other.ID), `{"allowfrom":["nope"]}`), http.StatusBadRequest, "invalid_allowfrom")
	if !contains(auditActions(t, f), "acme_enable") {
		t.Error("audit missing acme_enable")
	}
}

// PUBLIC_URL이 없고 요청에서도 주소를 만들 수 없으면 안내에 자리표시자를 넣는다.
func TestEnableACMEWithoutPublicURL(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	rec := f.do(call{method: "POST", path: fmt.Sprintf("/api/domains/%d/acme", d.ID), body: `{}`, bearer: true, headers: map[string]string{"Host": "bad host"}})
	c := decodeBody[credsResp](t, rec)
	if !strings.Contains(c.AcmeSh, `export ACMEDNS_BASE_URL="<ssl-checker 주소>"`) {
		t.Fatalf("acme_sh = %s", c.AcmeSh)
	}
}

func TestRotateKeepsSubdomain(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	old := f.enableACME(d.ID, `{}`)
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme/rotate", d.ID), "")
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("rotate = %d %s", rec.Code, rec.Body)
	}
	c := decodeBody[credsResp](t, rec)
	if c.Username != old.Username || c.Subdomain != old.Subdomain || c.CNAMERecord != old.CNAMERecord || c.Password == old.Password {
		t.Fatalf("rotated = %+v, old = %+v", c, old)
	}
	ctx := context.Background()
	if _, err := f.svc.Authenticate(ctx, old.Username, old.Password); !errors.Is(err, acmedns.ErrForbidden) {
		t.Fatalf("old key still works: %v", err)
	}
	if _, err := f.svc.Authenticate(ctx, c.Username, c.Password); err != nil {
		t.Fatalf("new key = %v", err)
	}
}

func TestPatchACMEAllowFrom(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	f.enableACME(d.ID, `{"allowfrom":["10.0.1.0/24"]}`)
	path := fmt.Sprintf("/api/domains/%d/acme", d.ID)
	rec := f.admin("PATCH", path, `{"allowfrom":["10.9.0.0/16"]}`)
	got := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || fmt.Sprint(got["allowfrom"]) != "[10.9.0.0/16]" || got["enabled"] != true {
		t.Fatalf("patch = %d %v", rec.Code, got)
	}
	if got := decodeBody[map[string]any](t, f.admin("PATCH", path, `{"allowfrom":[]}`)); fmt.Sprint(got["allowfrom"]) != "[]" {
		t.Fatalf("cleared = %v", got)
	}
	expectError(t, f.admin("PATCH", path, `{}`), http.StatusBadRequest, "invalid_allowfrom")
	expectError(t, f.admin("PATCH", path, `{"allowfrom":["x"]}`), http.StatusBadRequest, "invalid_allowfrom")
}

func TestCheckACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{}`)
	f.api.CNAME = &cnamecheck.Checker{Resolver: cnametest.Serve(t, map[string]cnametest.Answer{
		"_acme-challenge.a.com.": {CNAME: c.FullDomain + "."},
	})}
	rec := f.admin("POST", fmt.Sprintf("/api/domains/%d/acme/check", d.ID), "")
	got := decodeBody[map[string]any](t, rec)
	if rec.Code != http.StatusOK || got["cname_status"] != "ok" || got["cname_value"] != c.FullDomain || got["cname_checked_at"] == nil {
		t.Fatalf("check = %d %v", rec.Code, got)
	}
}

func TestDisableACME(t *testing.T) {
	f := newFixture(t)
	d := f.createDomain("a.com")
	c := f.enableACME(d.ID, `{}`)
	ctx := context.Background()
	acct, _ := f.svc.Authenticate(ctx, c.Username, c.Password)
	if err := f.svc.UpdateTXT(ctx, acct, strings.Repeat("a", 43)); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/domains/%d/acme", d.ID)

	f.mem.SetFailure(errors.New("cloudflare down"))
	expectError(t, f.admin("DELETE", path, ""), http.StatusInternalServerError, "dns_provider_error")
	f.mem.SetFailure(nil)
	if _, err := f.svc.Authenticate(ctx, c.Username, c.Password); err != nil {
		t.Fatalf("account lost after failed disable: %v", err)
	}

	if rec := f.admin("DELETE", path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("disable = %d %s", rec.Code, rec.Body)
	}
	if v := f.mem.Values(c.FullDomain); len(v) != 0 {
		t.Fatalf("provider still has %v", v)
	}
	if detail := decodeBody[detailResp](t, f.get(fmt.Sprintf("/api/domains/%d", d.ID))); detail.ACME["enabled"] != false {
		t.Fatalf("acme still on: %v", detail.ACME)
	}
	for _, cl := range []call{
		{method: "DELETE", path: path},
		{method: "PATCH", path: path, body: `{"allowfrom":[]}`},
		{method: "POST", path: path + "/rotate"},
		{method: "POST", path: path + "/check"},
	} {
		cl.bearer = true
		expectError(t, f.do(cl), http.StatusConflict, "acme_disabled")
	}
	// 다시 켜면 새 서브도메인이 생긴다.
	if again := f.enableACME(d.ID, `{}`); again.Subdomain == c.Subdomain {
		t.Fatal("re-enable reused the old subdomain")
	}
	actions := auditActions(t, f)
	for _, want := range []string{"acme_disable", "acme_enable"} {
		if !contains(actions, want) {
			t.Errorf("audit missing %s: %v", want, actions)
		}
	}
}
```

- [ ] **Step 3: 실패 확인**

Run: `make test 2>&1 | grep -A5 internal/adminapi`
Expected: FAIL

- [ ] **Step 4: 구현**

`internal/adminapi/view.go`의 `acmeView`에서 `CNAMERecord: cnamecheck.ChallengeName(fqdn) + ". CNAME " + full + ".",`를 `CNAMERecord: cnameRecord(fqdn, full),`로 바꾸고 추가:

```go
// cnameRecord는 도메인 DNS에 걸어야 하는 레코드 한 줄이다.
func cnameRecord(fqdn, fullDomain string) string {
	return cnamecheck.ChallengeName(fqdn) + ". CNAME " + fullDomain + "."
}
```

`internal/adminapi/api.go`:
- `API`에 `CNAME *cnamecheck.Checker // ACME CNAME "지금 확인"` 필드(import `ssl-checker/internal/cnamecheck`).
- `routes()`에 줄 추가:

```go
		{"POST", "/api/domains/{id}/acme", a.write(a.enableACME)},
		{"PATCH", "/api/domains/{id}/acme", a.write(a.patchACME)},
		{"DELETE", "/api/domains/{id}/acme", a.write(a.disableACME)},
		{"POST", "/api/domains/{id}/acme/rotate", a.write(a.rotateACME)},
		{"POST", "/api/domains/{id}/acme/check", a.write(a.checkACME)},
```

`internal/adminapi/acme.go`:

```go
package adminapi

import (
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"ssl-checker/internal/acmedns"
	"ssl-checker/internal/httpx"
	"ssl-checker/internal/netutil"
	"ssl-checker/internal/store"
)

const maxAllowFrom = 64

// parseAllowFrom은 /update를 부를 수 있는 IP·CIDR 목록이다. 비어 있으면 어디서든 부를 수 있다.
func parseAllowFrom(ss []string) ([]netip.Prefix, bool) {
	if len(ss) > maxAllowFrom {
		return nil, false
	}
	out := make([]netip.Prefix, 0, len(ss))
	for _, s := range ss {
		p, err := netutil.ParsePrefix(strings.TrimSpace(s))
		if err != nil {
			return nil, false
		}
		out = append(out, p)
	}
	return out, true
}

// domainFor는 경로의 도메인이다. 없으면 404로 답하고 ok=false.
func (a *API) domainFor(w http.ResponseWriter, r *http.Request) (store.Domain, bool) {
	id, ok := pathID(r)
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return store.Domain{}, false
	}
	d, err := a.Store.GetDomain(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found")
		return store.Domain{}, false
	}
	if err != nil {
		a.dbError(w, "get domain", err)
		return store.Domain{}, false
	}
	return d, true
}

// activeACME는 도메인과 그 활성 ACME 계정이다. ACME가 꺼져 있으면 409 acme_disabled로 답한다.
func (a *API) activeACME(w http.ResponseWriter, r *http.Request) (store.Domain, store.ACMEState, bool) {
	d, ok := a.domainFor(w, r)
	if !ok {
		return d, store.ACMEState{}, false
	}
	st, err := a.Store.ACMEStateByDomain(r.Context(), d.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return d, st, false
	}
	if err != nil {
		a.dbError(w, "acme state", err)
		return d, st, false
	}
	return d, st, true
}

// writeACME는 갱신된 ACME 상태(도메인 상세의 acme와 같은 모양)를 응답한다.
func (a *API) writeACME(w http.ResponseWriter, r *http.Request, d store.Domain) {
	st, err := a.Store.ACMEStateByDomain(r.Context(), d.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.dbError(w, "acme state", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.acmeView(d.FQDN, &st))
}

type credentials struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Subdomain   string   `json:"subdomain"`
	FullDomain  string   `json:"fulldomain"`
	CNAMERecord string   `json:"cname_record"`
	AllowFrom   []string `json:"allowfrom"`
	AcmeSh      string   `json:"acme_sh"`
}

// writeCredentials는 일회성 자격 증명이다. 비밀번호는 이 응답에만 나오므로 캐시하지 않게 한다.
func (a *API) writeCredentials(w http.ResponseWriter, r *http.Request, status int, fqdn string, reg acmedns.Registration) {
	allow := make([]string, 0, len(reg.AllowFrom))
	for _, p := range reg.AllowFrom {
		allow = append(allow, p.String())
	}
	base := a.PublicURLFor(r)
	if base == "" {
		base = "<ssl-checker 주소>"
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, status, credentials{
		Username: reg.Username, Password: reg.Password, Subdomain: reg.Subdomain, FullDomain: reg.FullDomain,
		CNAMERecord: cnameRecord(fqdn, reg.FullDomain), AllowFrom: allow, AcmeSh: acmeSh(base, fqdn, reg),
	})
}

// acmeSh는 대상 서버에서 그대로 붙여 넣을 acme.sh 명령이다(acme.sh 3.1.4 이상).
func acmeSh(baseURL, fqdn string, reg acmedns.Registration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "export ACMEDNS_BASE_URL=%q\n", baseURL)
	fmt.Fprintf(&b, "export ACMEDNS_USERNAME=%q\n", reg.Username)
	fmt.Fprintf(&b, "export ACMEDNS_PASSWORD=%q\n", reg.Password)
	fmt.Fprintf(&b, "export ACMEDNS_SUBDOMAIN=%q\n", reg.Subdomain)
	b.WriteString("# 처음 연결하는 도메인은 --issue에 --staging을 붙여 먼저 확인한다\n")
	fmt.Fprintf(&b, "~/.acme.sh/acme.sh --issue --dns dns_acmedns -d %s -d '*.%s'\n", fqdn, fqdn)
	fmt.Fprintf(&b, "~/.acme.sh/acme.sh --install-cert -d %s --ecc \\\n", fqdn)
	fmt.Fprintf(&b, "  --key-file /etc/ssl/%s/key.pem \\\n", fqdn)
	fmt.Fprintf(&b, "  --fullchain-file /etc/ssl/%s/fullchain.pem \\\n", fqdn)
	b.WriteString("  --reloadcmd \"systemctl reload nginx\"\n")
	return b.String()
}

type allowFromRequest struct {
	AllowFrom *[]string `json:"allowfrom"`
}

func (a *API) enableACME(w http.ResponseWriter, r *http.Request) {
	d, ok := a.domainFor(w, r)
	if !ok {
		return
	}
	var req allowFromRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	var raw []string
	if req.AllowFrom != nil {
		raw = *req.AllowFrom
	}
	allow, valid := parseAllowFrom(raw)
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	reg, err := a.Svc.Register(r.Context(), d.FQDN, allow)
	switch {
	case errors.Is(err, store.ErrActiveAccountExists):
		httpx.WriteError(w, http.StatusConflict, "acme_enabled")
		return
	case errors.Is(err, acmedns.ErrInvalidDomain):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_domain")
		return
	case err != nil:
		a.serviceError(w, "enable acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_enable", Target: d.FQDN, Detail: "subdomain=" + reg.Subdomain})
	a.writeCredentials(w, r, http.StatusCreated, d.FQDN, reg)
}

func (a *API) patchACME(w http.ResponseWriter, r *http.Request) {
	var req allowFromRequest
	if err := decode(r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "malformed_json")
		return
	}
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	if req.AllowFrom == nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	allow, valid := parseAllowFrom(*req.AllowFrom)
	if !valid {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_allowfrom")
		return
	}
	if err := a.Store.SetAllowFrom(r.Context(), st.AccountID, allow); errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	} else if err != nil {
		a.dbError(w, "set allowfrom", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_update", Target: d.FQDN, Detail: "allowfrom=" + strings.Join(*req.AllowFrom, ",")})
	a.writeACME(w, r, d)
}

func (a *API) rotateACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	reg, err := a.Svc.Rotate(r.Context(), st.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.serviceError(w, "rotate acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_rotate", Target: d.FQDN})
	a.writeCredentials(w, r, http.StatusOK, d.FQDN, reg)
}

func (a *API) checkACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	res := a.CNAME.Check(r.Context(), d.FQDN, st.Subdomain+"."+a.Zone)
	if res.Err != nil {
		a.Log.Warn("cname check", "domain", d.FQDN, "err", res.Err)
	}
	if err := a.Store.SetCNAMEStatus(r.Context(), st.AccountID, res.Status, res.Value, a.now()); err != nil {
		a.dbError(w, "save cname status", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_check", Target: d.FQDN, Detail: "cname=" + string(res.Status)})
	a.writeACME(w, r, d)
}

func (a *API) disableACME(w http.ResponseWriter, r *http.Request) {
	d, st, ok := a.activeACME(w, r)
	if !ok {
		return
	}
	err := a.Svc.Disable(r.Context(), st.AccountID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.WriteError(w, http.StatusConflict, "acme_disabled")
		return
	}
	if err != nil {
		a.serviceError(w, "disable acme", err)
		return
	}
	a.audit(r, store.AuditEntry{Action: "acme_disable", Target: d.FQDN, Detail: "subdomain=" + st.Subdomain})
	w.WriteHeader(http.StatusNoContent)
}
```

`acmeView`의 `allowfrom`은 빈 목록이면 `[]`로 나와야 한다(이미 `make([]string, 0, ...)`). `TestPatchACMEAllowFrom`의 `fmt.Sprint(got["allowfrom"])`는 JSON `[]`가 `[]interface{}{}`로 풀려 `"[]"`가 된다.

- [ ] **Step 5: 통과 확인**

Run: `gofmt -l internal/ && make test && go vet ./...`
Expected: 전체 PASS

- [ ] **Step 6: 커밋**

```bash
git add internal/adminapi
git commit -m "feat: admin API for enabling, rotating, checking, and disabling ACME" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: 연결, smoke, 문서

**Files:**
- Modify: `internal/app/serve.go`
- Modify: `scripts/smoke.sh`
- Modify: `docs/guide/api.md`, `docs/guide/onboarding.md`, `docs/guide/operations.md`, `docs/guide/configuration.md`

**Interfaces:**
- Consumes: `adminapi.NewAuth`, `API.Svc/Runner/CNAME/Auth` (Task 5–7), `config.Config.DNSResolver`, `AdminKey`.

- [ ] **Step 1: smoke 확장 (먼저 써서 실패 확인)**

`scripts/smoke.sh`의 `/api/config` 검사 뒤, worker 검사 앞에 추가:

```sh
auth="Authorization: Bearer $ADMIN_KEY"
json="Content-Type: application/json"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/api/domains" -H "$json" -d '{"fqdn":"smoke2.example"}')
[ "$code" = 401 ] || { echo "unauthenticated write returned $code" >&2; exit 1; }

dom=$(curl -fsS -X POST "$BASE/api/domains" -H "$auth" -H "$json" -d '{"fqdn":"smoke2.example"}')
did=$(printf '%s' "$dom" | sed -n 's/^{"id":\([0-9]*\).*/\1/p')
[ -n "$did" ] || { echo "unexpected domain response: $dom" >&2; exit 1; }

creds=$(curl -fsS -X POST "$BASE/api/domains/$did/acme" -H "$auth" -H "$json" -d '{}')
user2=$(field "$creds" username)
pass2=$(field "$creds" password)
sub2=$(field "$creds" subdomain)
resp=$(curl -fsS -X POST "$BASE/update" -H "X-Api-User: $user2" -H "X-Api-Key: $pass2" \
  -d "{\"subdomain\":\"$sub2\",\"txt\":\"$txt\"}")
echo "$resp" | grep -q "\"txt\":\"$txt\"" || { echo "update with API-issued creds failed: $resp" >&2; exit 1; }

rot=$(curl -fsS -X POST "$BASE/api/domains/$did/acme/rotate" -H "$auth" -H "$json")
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/update" -H "X-Api-User: $user2" -H "X-Api-Key: $pass2" \
  -d "{\"subdomain\":\"$sub2\",\"txt\":\"$txt\"}")
[ "$code" = 401 ] || { echo "old key after rotate returned $code" >&2; exit 1; }
[ "$(field "$rot" subdomain)" = "$sub2" ] || { echo "rotate changed subdomain: $rot" >&2; exit 1; }

jar=$(mktemp)
curl -fsS -c "$jar" -X POST "$BASE/api/session" -H "$json" -d "{\"key\":\"$ADMIN_KEY\"}" >/dev/null
curl -fsS -b "$jar" "$BASE/api/session" | grep -q '"admin":true' || { echo "session cookie not accepted" >&2; exit 1; }
rm -f "$jar"
```

`field` 함수는 이미 위에 정의돼 있다. `$BASE`가 `http://`라 쿠키에 `Secure`가 붙지 않아 curl이 쿠키를 보낸다(계획의 「스펙과 다른 점」).

Run: `./scripts/smoke.sh 2>&1 | tail -3`
Expected: FAIL (`serve.go`가 아직 `Auth`를 넣지 않아 panic 또는 401/404)

- [ ] **Step 2: serve 연결**

`internal/app/serve.go`: import에 `"ssl-checker/internal/cnamecheck"`, `"ssl-checker/internal/monitor"` 추가. `h := &acmedns.Handler{...}` 앞에서 서비스를 변수로 빼고 `api`를 바꾼다:

```go
	svc := &acmedns.Service{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL}
	h := &acmedns.Handler{
		Svc:           svc,
		IP:            ip,
		Admin:         httpx.NewAdminAuth(cfg.AdminKey, ip),
		UpdateLimit:   httpx.NewKeyed(updateEvery, updateBurst),
		AuthFailLimit: httpx.NewKeyed(authFailEvery, authFailBurst),
		Health:        &acmedns.HealthChecker{DB: pool, Provider: provider, CacheFor: time.Minute},
		Log:           slog.Default(),
	}
	api := &adminapi.API{
		Store: st, Svc: svc, Zone: cfg.ChallengeZone, CheckInterval: cfg.CheckInterval, PublicURL: cfg.PublicURL, IP: ip,
		Runner: &monitor.Runner{Prober: &monitor.Prober{}, Sink: st, Log: slog.Default()},
		CNAME:  &cnamecheck.Checker{Resolver: cfg.DNSResolver},
		Auth:   adminapi.NewAuth(cfg.AdminKey),
		Log:    slog.Default(),
	}
```

`Serve`의 함수 주석을 "acme-dns 호환 API와 /api(조회·관리)를 연다"로 바꾼다.

Run: `./scripts/smoke.sh 2>&1 | tail -1`
Expected: `smoke OK`

- [ ] **Step 3: 문서**

`docs/guide/api.md`: `## 조회 API (/api)` 섹션의 마지막 줄 "도메인과 모니터링 대상을 추가·수정하는 API는 다음 버전에서 추가됩니다."를 지우고, 그 뒤에 추가:

````markdown
## 관리 API (`/api`, 관리자)

도메인·모니터링 대상을 관리하고 ACME를 켜고 끕니다. 관리자만 부를 수 있습니다.

### 인증

| 방법 | 쓰는 곳 |
| --- | --- |
| `Authorization: Bearer <ADMIN_KEY>` | 스크립트, 에이전트 |
| 세션 쿠키 `sslchk_session` (`POST /api/session`로 발급) | 대시보드 화면 |

- 관리자가 아니면 401 `unauthorized`입니다. 틀린 키(로그인·Bearer)는 IP별로 5번까지 받고, 그 뒤로는 12초에 1번만 받습니다(429 `too_many_attempts`).
- GET이 아닌 요청은 `Content-Type: application/json`이 있어야 합니다(본문이 없어도). 없으면 415 `unsupported_media_type`입니다. 모르는 필드가 있으면 400 `malformed_json`입니다.
- 모든 쓰기는 감사 로그에 남습니다(`GET /api/audit`).

### 세션

| 요청 | 본문 | 응답 |
| --- | --- | --- |
| `POST /api/session` | `{"key":"<ADMIN_KEY>"}` | 200 `{"admin":true}` + 쿠키. 틀리면 401 |
| `GET /api/session` | | `{"admin":true|false}` |
| `DELETE /api/session` | | 204, 쿠키 삭제 |

쿠키는 12시간 유효합니다. 서버에 세션을 저장하지 않으므로, **로그아웃해도 이미 복사된 쿠키는 만료까지 유효합니다.** 모든 세션을 끊으려면 `ADMIN_KEY`를 바꿉니다. 쿠키에는 `HttpOnly`, `SameSite=Strict`가 붙고, 공개 주소가 `https://`면 `Secure`도 붙습니다. 화면 로그인은 HTTPS(Caddy 뒤)에서 쓰는 것을 권장합니다.

### 도메인과 대상

| 요청 | 본문 | 응답 |
| --- | --- | --- |
| `POST /api/domains` | `{"fqdn":"a.com","memo":""}` | 201 도메인 상세. 409 `domain_exists`, 400 `invalid_domain`·`invalid_memo` |
| `PATCH /api/domains/{id}` | `{"memo":"..."}` | 200 도메인 상세 |
| `DELETE /api/domains/{id}` | | 204. ACME가 켜져 있으면 먼저 끄고(챌린지 TXT 삭제) 대상·기록과 함께 지웁니다 |
| `POST /api/domains/{id}/targets` | `{"hostname":"www.a.com","port":443,"sni":"","ips":["10.0.1.10"],"memo":""}` | 201 대상(바로 점검한 결과 포함). 409 `target_exists`, 400 `invalid_target` |
| `PATCH /api/targets/{id}` | 위 필드 중 바꿀 것만 | 200 대상. hostname·port·sni·ips를 바꾸면 바로 다시 점검합니다 |
| `DELETE /api/targets/{id}` | | 204 |
| `POST /api/targets/{id}/check` | | 200 `{"results":[...]}` 이번 점검 결과 |

- `hostname`: 점검할 이름. 내부 이름(한 단어)이나 IP도 됩니다. 와일드카드는 안 됩니다.
- `ips`: 서버별로 직접 접속할 IP 목록(최대 16개). 비우면 hostname으로 한 번 접속합니다.
- `memo`: 최대 500자.

### ACME

| 요청 | 본문 | 응답 |
| --- | --- | --- |
| `POST /api/domains/{id}/acme` | `{"allowfrom":["10.0.1.0/24"]}` | 201 **일회성 자격 증명**. 409 `acme_enabled`, 400 `invalid_allowfrom` |
| `PATCH /api/domains/{id}/acme` | `{"allowfrom":[...]}` | 200 ACME 상태. 빈 목록이면 어디서든 `/update`를 부를 수 있습니다 |
| `POST /api/domains/{id}/acme/rotate` | | 200 일회성 자격 증명. username·서브도메인은 그대로, 키만 바뀝니다. 옛 키는 바로 무효 |
| `POST /api/domains/{id}/acme/check` | | 200 ACME 상태(CNAME을 지금 확인한 결과) |
| `DELETE /api/domains/{id}/acme` | | 204. 계정 폐기, 챌린지 TXT 삭제. 다시 켜면 서브도메인이 바뀌어 CNAME을 다시 걸어야 합니다 |

ACME가 꺼진 도메인에 수정·재발급·확인·끄기를 부르면 409 `acme_disabled`입니다. 끄기나 도메인 삭제 중 DNS 공급자가 실패하면 아무것도 지우지 않고 500 `dns_provider_error`를 돌려줍니다.

일회성 자격 증명 응답(`Cache-Control: no-store`). **비밀번호는 이 응답에만 나옵니다.**

```json
{
  "username": "<uuid>", "password": "<40자>", "subdomain": "<uuid>",
  "fulldomain": "<uuid>.acme.example.com",
  "cname_record": "_acme-challenge.a.com. CNAME <uuid>.acme.example.com.",
  "allowfrom": ["10.0.1.0/24"],
  "acme_sh": "export ACMEDNS_BASE_URL=\"https://...\"\n..."
}
```

`acme_sh`는 대상 서버에서 그대로 붙여 넣을 환경변수와 acme.sh 발급·설치 명령입니다. `ACMEDNS_BASE_URL`은 `PUBLIC_URL`(없으면 요청 주소)입니다.

### 감사 로그

`GET /api/audit?limit=100&before_id=<id>` (관리자, 최대 500) → `{"entries":[{"id","at","actor","action","target","source_ip","detail"}]}` 최신순. 다음 페이지는 마지막 항목의 `id`를 `before_id`로 넘깁니다.

```bash
read -rs ADMIN_KEY
curl -s -X POST '<앱>/api/domains' -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"fqdn":"a.com"}'
```
````

`docs/guide/onboarding.md`의 `## 1. 계정 등록` 섹션을 아래로 바꾼다(제목은 `## 1. 도메인 등록과 ACME 켜기`, 전체 흐름 표의 1단계 이름도 맞춘다):

````markdown
## 1. 도메인 등록과 ACME 켜기

도메인 하나에 ACME 계정 하나를 만듭니다. 이 계정으로 `a.com`과 `*.a.com`을 함께 처리합니다. `*.a.com` 인증서가 `www.a.com`, `api.a.com` 같은 한 단계 아래 이름을 모두 덮습니다. 두 단계 아래(`*.dev.a.com`)가 필요하면 `dev.a.com`을 따로 등록합니다.

`ADMIN_KEY`가 셸 히스토리에 남지 않게 입력으로 받습니다.

```bash
read -rs ADMIN_KEY      # 붙여넣고 Enter. 화면에 표시되지 않는다
A='Authorization: Bearer '"$ADMIN_KEY"
J='Content-Type: application/json'

# 도메인 등록 (이미 있으면 409 domain_exists — GET <앱>/api/domains 에서 id를 찾는다)
curl -s -X POST <앱>/api/domains -H "$A" -H "$J" -d '{"fqdn":"a.com"}'
# 응답의 "id"로 ACME 켜기
curl -s -X POST <앱>/api/domains/<id>/acme -H "$A" -H "$J" \
  -d '{"allowfrom":["10.0.1.10/32","10.0.1.11/32"]}'
unset ADMIN_KEY A
```

- `allowfrom`: 이 계정으로 `/update`를 부를 수 있는 서버 IP나 CIDR 목록. 비우면 어디서든 부를 수 있습니다. 서버 IP를 알고 있다면 넣는 것을 권장합니다. 프록시 뒤나 rootless Docker라면 먼저 [출발지 IP를 확인](configuration.md#네트워크와-프록시)합니다.

응답에는 `username`, `password`, `subdomain`, `fulldomain`, 등록할 CNAME 한 줄(`cname_record`), 그리고 3단계에서 그대로 붙여 넣을 acme.sh 명령(`acme_sh`)이 들어 있습니다.

`password`는 이때 한 번만 나오고 다시 볼 수 없습니다. 이 도메인의 인증서를 발급받을 수 있는 비밀이므로 시크릿 저장소에 바로 넣습니다. 티켓이나 채팅에 붙이지 않습니다. 잃어버리면 [키를 재발급](operations.md#계정-폐기와-키-재발급)합니다. 서브도메인이 그대로라 CNAME은 다시 걸지 않아도 됩니다.

예전 방식인 `POST /register`(`{"domain":"a.com","allowfrom":[...]}`)도 그대로 동작합니다. 도메인이 없으면 만들고 ACME를 켭니다.
````

같은 파일 `## 같은 도메인에 서버 추가`의 "지금은 `allowfrom`을 수정하는 API가 없으므로 …" 문단과 그 아래 두 항목을 아래로 바꾼다:

```markdown
새 서버 IP가 `allowfrom`에 없으면 `/update`가 거부됩니다. `PATCH <앱>/api/domains/<id>/acme`에 `{"allowfrom":[...]}`로 목록 전체를 다시 보내 고칩니다. 계정과 CNAME은 그대로입니다.
```

`2. CNAME 등록`의 확인 명령 아래에 한 줄 추가:

```markdown
ssl-checker도 공개 DNS로 확인합니다: `POST <앱>/api/domains/<id>/acme/check`의 `cname_status`가 `ok`면 됩니다(worker도 `CHECK_INTERVAL`마다 확인합니다).
```

`docs/guide/operations.md`의 `## 계정 폐기와 키 재발급` 섹션 본문을 아래로 바꾼다:

````markdown
키가 새었거나 잃어버렸으면 **재발급**합니다. username과 서브도메인이 그대로라 CNAME은 다시 걸지 않아도 됩니다. 옛 키는 바로 무효가 됩니다.

```bash
curl -s -X POST <앱>/api/domains/<id>/acme/rotate -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json'
```

응답의 새 `password`(또는 `acme_sh`)로 각 서버의 acme.sh 설정을 바꿉니다: `ACMEDNS_PASSWORD`를 새 값으로 export한 뒤 `--issue … --force`로 한 번 다시 발급하면 도메인 설정 파일에 새 값이 저장됩니다.

도메인에서 ACME를 더 쓰지 않으면 **끕니다**. 계정을 폐기하고 챌린지 TXT를 지웁니다. 다시 켜면 서브도메인이 바뀌어 CNAME을 다시 걸어야 합니다.

```bash
curl -s -X DELETE <앱>/api/domains/<id>/acme -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json'
```

DNS 공급자가 실패하면 아무것도 바꾸지 않고 500 `dns_provider_error`를 돌려줍니다. 공급자가 회복된 뒤 다시 부릅니다.
````

(기존 섹션의 SQL 기반 폐기 안내는 지운다. 제목과 앵커 `#계정-폐기와-키-재발급`은 유지한다.)

`docs/guide/operations.md`의 `## 감사 로그` 섹션에 한 줄 추가: "관리 API로 한 일은 `admin`, 로그인 실패는 `anonymous`로 남습니다. `GET <앱>/api/audit`(관리자)로도 볼 수 있습니다." 그리고 action 목록(있다면)에 `login`, `login_denied`, `domain_create`, `domain_update`, `domain_delete`, `target_create`, `target_update`, `target_delete`, `target_check`, `acme_enable`, `acme_update`, `acme_rotate`, `acme_check`, `acme_disable`을 더한다.

`docs/guide/configuration.md`의 `ADMIN_KEY` 행 설명 끝에 덧붙인다: "관리 API의 Bearer 키이자 화면 로그인 키입니다. 바꾸면 모든 로그인 세션이 끊깁니다."

- [ ] **Step 4: 전체 확인**

Run: `make test && go vet ./... && ./scripts/smoke.sh 2>&1 | tail -1`
Expected: 전체 PASS, `smoke OK`

- [ ] **Step 5: 커밋**

```bash
git add internal/app scripts docs/guide
git commit -m "feat: wire admin API into serve and document it" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
