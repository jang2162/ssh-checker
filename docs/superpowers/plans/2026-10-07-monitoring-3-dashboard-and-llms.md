# 모니터링 3: 대시보드와 llms.txt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 누구나 도메인 인증서 상태를 보고, 관리자는 등록·수정·ACME 설정을 하는 웹 대시보드(React SPA, 바이너리에 내장)와, 에이전트가 ACME를 구성할 때 따라 할 `/llms.txt`를 제공한다.

**Architecture:** `web/`(React + Vite + TypeScript)의 빌드 결과를 `internal/webui`가 `go:embed`로 내장해 SPA 폴백과 함께 제공한다. `webui.Handler`는 mux를 감싸는 방식이라 API 경로(`/api/`, `/update`, `/register`, `/health`, `/llms.txt`)를 가로채지 않는다. `/llms.txt`는 `internal/llmstxt`가 템플릿에 서버 주소·챌린지 존·판정 코드 표를 채워 낸다. 판정 코드 설명은 `monitor.Codes()` 한 곳에서 `/api/codes`, 화면, llms.txt가 나눠 쓴다. 화면은 계획 1·2의 API만 쓴다.

**Tech Stack:** Go 1.27(표준 라이브러리만), React 19, react-router-dom 7, Vite 8, TypeScript 5.9, Node 24(`node --test`로 로직 테스트). 새 Go 의존성 없음.

**Spec:** `docs/superpowers/specs/2026-10-07-monitoring-dashboard-design.md` (「화면」「/llms.txt」「구현 계획 분할」 3번). 계획 1·2는 main에 병합됐다.

## Global Constraints

- 모듈 경로 `ssl-checker`. 주석·로그·문서·화면 문구는 한국어, 식별자는 영어. 주석은 **왜**를 적는다.
- Go: 새 외부 의존성 금지. 테스트 DB가 떠 있으면 `make test`(DB 테스트는 `dbtest.New(t)`).
- 화면(`web/`): 런타임 의존성은 `react`, `react-dom`, `react-router-dom`**만**. UI 라이브러리 없음, CSS 직접 작성. 개발 의존성은 `package.json`에 적힌 것만. Node 24.
- TypeScript: `strict`, `noUncheckedIndexedAccess`, `erasableSyntaxOnly`(enum·생성자 매개변수 프로퍼티 금지). import에는 `.ts`/`.tsx` 확장자를 붙인다(Node 테스트가 변환 없이 TS를 읽는다).
- API 응답 모양은 `web/src/api/types.ts` 한 곳에만 둔다. 서버는 GET이 아닌 요청에 `Content-Type: application/json`을 요구하므로(본문이 없어도) 클라이언트는 항상 붙인다.
- `/api/`, `/update`, `/register`, `/health`, `/llms.txt`는 화면이 가로채지 않는다. `/assets/*`는 `Cache-Control: public, max-age=31536000, immutable`, `index.html`은 `no-cache`.
- 화면은 비밀번호를 일회성 창에서만 보여 주고 저장(localStorage 등)이나 로그에 남기지 않는다. 비로그인 사용자에게는 쓰기 버튼을 보이지 않는다(서버도 거부한다).
- 상태 색은 CSS 토큰이고 라이트·다크를 모두 갖는다. 배지에는 색과 함께 글자를 쓴다. 시각은 브라우저 시간대의 `YYYY-MM-DD HH:mm`. 데이터는 화면을 열 때와 쓰기 후, 그리고 60초마다 다시 불러온다.
- 커밋 메시지 끝: `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

### 스펙과 다른 점·보완 (이 계획에서 정함)

- **`GET /api/codes` 신설:** 스펙에는 없다. 화면의 판정 코드 툴팁과 llms.txt의 코드 표가 같은 설명을 쓰게 하려고 `monitor.Codes()` 한 곳에서 낸다(코드를 추가하면 화면·문서가 따로 어긋나지 않는다).
- **`webui.Handler`는 mux를 감싼다:** `GET /`를 mux에 등록하면 `GET /update`가 405 대신 화면을 돌려줘 기존 응답이 바뀐다. 그래서 예약 경로와 GET·HEAD가 아닌 요청은 안쪽 mux로 넘긴다.
- **화면 보안 헤더:** 화면 응답에 `Content-Security-Policy`(같은 출처만, `frame-ancestors 'none'`), `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`를 붙인다(관리자 쿠키 세션이 있는 화면이라 클릭재킹을 막는다).
- **"오래됨" 표시(계획 1 이월):** 상태는 점검할 때 저장되므로 worker가 멈추면 옛 "정상"이 남는다. 마지막 점검이 `check_interval`의 2배보다 오래되면 화면이 "오래됨"과 경고를 띄운다.
- **JSON 모양(계획 1 이월):** 인증서를 못 받은 결과의 `not_before`·`not_after`는 서버가 필드를 빼 버린다. Go는 바꾸지 않고 TS 타입에서 optional로 둔다.
- **일회성 자격 증명 창(계획 2 이월):** `read -rs` 안내를 넣고, 재발급이면 `--force`가 필요하다는 경고를 보여 준다. Esc로 닫히지 않는다.
- **대시보드 정렬·필터:** 나쁜 상태, 만료가 가까운 순서로 정렬한다. 상태 필터는 주소창 `?status=`에 둔다.
- **화면을 빌드하지 않은 바이너리:** `index.html`이 없으면 503과 안내 페이지를 돌려준다(API·llms.txt는 그대로 동작).
- **Docker 빌드:** Node 단계에서 `npm test`와 `npm run build`가 모두 통과해야 이미지가 만들어진다.

## Review Focus

- **API 경로가 화면에 가로채임:** `GET /update`가 405가 아니라 HTML 200이 되는 일. 예전과 같은 405(`Allow: POST`)와 JSON 404를 유지해야 한다. → Task 3 테스트.
- **없는 에셋이 HTML로 응답:** `/assets/missing.js`가 `index.html`(200)로 돌아와 브라우저가 구문 오류만 내는 일. 404여야 한다. → Task 3 테스트.
- **worker가 멈춘 뒤의 옛 "정상":** 점검이 오래 멈췄는데 화면이 계속 초록으로 보이는 일. "오래됨" 표시가 떠야 한다. → Task 4 `isStale` 테스트, Task 5 대시보드.
- **일회성 자격 증명 창이 Esc로 닫혀 password를 잃는 일:** 버튼으로만 닫혀야 한다. → Task 10 브라우저 확인.
- **메모·오류 문구의 HTML 주입:** 메모에 `<img src=x onerror=...>`를 넣어도 글자 그대로 보이고 실행되지 않아야 한다. → Task 10 브라우저 확인.

---

### Task 1: 판정 코드 표와 `/api/codes`

**Files:**
- Create: `internal/monitor/codes.go`
- Create: `internal/monitor/codes_test.go`
- Modify: `internal/adminapi/api.go`
- Modify: `internal/adminapi/api_test.go`

**Interfaces:**
- Consumes: `monitor.codeStatus`(코드→상태 맵), `Status.rank()`.
- Produces: `monitor.CodeInfo{Code string; Status Status; Meaning string}`(JSON `code`·`status`·`meaning`), `monitor.Codes() []CodeInfo`(나쁜 상태부터, 같은 상태 안에서는 코드 이름순, 모든 코드를 한 번씩), `GET /api/codes` → `{"codes":[...]}`(인증 불필요).

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/monitor/codes_test.go`:

```go
package monitor

import "testing"

func TestCodesCoverEveryCode(t *testing.T) {
	got := Codes()
	if len(got) != len(codeStatus) {
		t.Fatalf("len(Codes()) = %d, want %d", len(got), len(codeStatus))
	}
	seen := map[string]bool{}
	for _, c := range got {
		if seen[c.Code] {
			t.Errorf("%s listed twice", c.Code)
		}
		seen[c.Code] = true
		if c.Meaning == "" {
			t.Errorf("%s has no meaning", c.Code)
		}
		if c.Status != CodeStatus(c.Code) {
			t.Errorf("%s status = %s, want %s", c.Code, c.Status, CodeStatus(c.Code))
		}
	}
	for code := range codeStatus {
		if !seen[code] {
			t.Errorf("%s missing from Codes()", code)
		}
	}
}

func TestCodesWorstFirst(t *testing.T) {
	got := Codes()
	for i := 1; i < len(got); i++ {
		if got[i-1].Status.rank() < got[i].Status.rank() {
			t.Fatalf("%s (%s) listed before %s (%s)", got[i-1].Code, got[i-1].Status, got[i].Code, got[i].Status)
		}
	}
	if got[0].Status != StatusCritical || got[len(got)-1].Code != CodeNotChecked {
		t.Fatalf("first %s last %s", got[0].Code, got[len(got)-1].Code)
	}
}
```

`internal/adminapi/api_test.go`에 `TestCodes`를 더하고, `TestUnknownPathAndMethod`가 도는 경로 목록에 `/api/codes`를 넣는다:

```diff
diff --git a/internal/adminapi/api_test.go b/internal/adminapi/api_test.go
index a504efc..9aa8d82 100644
--- a/internal/adminapi/api_test.go
+++ b/internal/adminapi/api_test.go
@@ -388,6 +388,21 @@ func TestConfig(t *testing.T) {
 	}
 }
 
+func TestCodes(t *testing.T) {
+	f := newFixture(t)
+	var out struct {
+		Codes []struct{ Code, Status, Meaning string }
+	}
+	f.getJSON("/api/codes", &out)
+	want := monitor.Codes()
+	if len(out.Codes) != len(want) {
+		t.Fatalf("codes = %d, want %d", len(out.Codes), len(want))
+	}
+	if out.Codes[0].Code != want[0].Code || out.Codes[0].Status != "critical" || out.Codes[0].Meaning == "" {
+		t.Fatalf("first code = %+v", out.Codes[0])
+	}
+}
+
 func TestUnknownPathAndMethod(t *testing.T) {
 	f := newFixture(t)
 	rec := f.get("/api/nope")
@@ -395,7 +410,7 @@ func TestUnknownPathAndMethod(t *testing.T) {
 	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
 		t.Fatalf("content-type = %q", ct)
 	}
-	for _, path := range []string{"/api/domains", "/api/domains/1", "/api/summary", "/api/targets/1/results", "/api/config"} {
+	for _, path := range []string{"/api/domains", "/api/domains/1", "/api/summary", "/api/targets/1/results", "/api/config", "/api/codes"} {
 		rec := f.do(call{method: http.MethodPut, path: path})
 		expectError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
 		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
```

- [ ] **Step 2: 실패를 확인한다**

Run: `go test ./internal/monitor -run TestCodes -v`
Expected: FAIL (`undefined: Codes`)

- [ ] **Step 3: 구현한다**

`internal/monitor/codes.go`:

```go
package monitor

import (
	"slices"
	"strings"
)

// CodeInfo는 판정 코드 하나의 설명이다. 화면(/api/codes)과 /llms.txt가 같은 표를 쓴다.
type CodeInfo struct {
	Code    string `json:"code"`
	Status  Status `json:"status"`
	Meaning string `json:"meaning"`
}

// codeMeanings는 코드별 한국어 설명이다. 새 코드를 만들면 여기에도 적는다(테스트가 빠진 코드를 잡는다).
var codeMeanings = map[string]string{
	CodeConnectFailed:    "접속이나 TLS 핸드셰이크에 실패했습니다(10초 제한). 아주 오래된 서버도 여기로 나오니 error 필드를 확인합니다.",
	CodeCertExpired:      "인증서가 만료됐습니다.",
	CodeCertExpiresSoon:  "만료까지 min(7일, 수명의 1/3) 미만 남았습니다.",
	CodeChainInvalid:     "인증서 체인 검증에 실패했습니다(자체 서명 포함).",
	CodeHostnameMismatch: "인증서가 접속한 이름을 덮지 않습니다.",
	CodeRenewalOverdue:   "남은 기간이 수명의 1/3 미만입니다. 정상이면 이미 갱신됐을 시점입니다.",
	CodeTLSOutdated:      "TLS 1.2 미만으로 협상됐습니다.",
	CodeSerialMismatch:   "같은 대상의 서버들이 서로 다른 인증서를 냅니다(일부 서버에 배포가 빠짐).",
	CodeCNAMEMissing:     "ACME를 켰는데 _acme-challenge CNAME이 없습니다.",
	CodeCNAMEMismatch:    "_acme-challenge CNAME이 다른 곳을 가리킵니다.",
	CodeTXTUpdateFailing: "/update가 3번 이상 연달아 실패했습니다(DNS 공급자나 DB 오류).",
	CodeWeakKey:          "RSA 2048비트 미만 또는 ECDSA 256비트 미만 키입니다.",
	CodeNotChecked:       "아직 점검 결과가 없습니다.",
}

// Codes는 모든 판정 코드를 나쁜 상태부터 돌려준다. 같은 상태 안에서는 코드 이름순이다.
func Codes() []CodeInfo {
	out := make([]CodeInfo, 0, len(codeStatus))
	for code, st := range codeStatus {
		out = append(out, CodeInfo{Code: code, Status: st, Meaning: codeMeanings[code]})
	}
	slices.SortFunc(out, func(a, b CodeInfo) int {
		if d := b.Status.rank() - a.Status.rank(); d != 0 {
			return d
		}
		return strings.Compare(a.Code, b.Code)
	})
	return out
}
```

`internal/adminapi/api.go`에 라우트와 핸들러를 더한다:

```diff
diff --git a/internal/adminapi/api.go b/internal/adminapi/api.go
index 3dc8e81..d1c9a5c 100644
--- a/internal/adminapi/api.go
+++ b/internal/adminapi/api.go
@@ -52,6 +52,7 @@ func (a *API) routes() []route {
 		{"GET", "/api/domains/{id}", a.getDomain},
 		{"GET", "/api/targets/{id}/results", a.targetResults},
 		{"GET", "/api/config", a.config},
+		{"GET", "/api/codes", a.codes},
 		{"GET", "/api/session", a.sessionStatus},
 		{"POST", "/api/session", jsonOnly(a.login)},
 		{"DELETE", "/api/session", jsonOnly(a.logout)},
@@ -286,6 +287,11 @@ func (a *API) config(w http.ResponseWriter, r *http.Request) {
 	})
 }
 
+// codes는 판정 코드 표다. 화면과 llms.txt가 같은 설명을 쓰도록 한 곳에서 낸다.
+func (a *API) codes(w http.ResponseWriter, r *http.Request) {
+	httpx.WriteJSON(w, http.StatusOK, map[string]any{"codes": monitor.Codes()})
+}
+
 // hostRE는 이름[:포트] 또는 [IPv6][:포트]다. 공개 주소는 자격 증명 안내에 들어가므로 경로·사용자 정보·공백이 섞인 값은 받지 않는다.
 var hostRE = regexp.MustCompile(`^([A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?|\[[0-9A-Fa-f:.]+\])(:[0-9]{1,5})?$`)
 
```

- [ ] **Step 4: 통과를 확인한다**

Run: `gofmt -l internal/ && go vet ./... && TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go test ./internal/monitor ./internal/adminapi`
Expected: `gofmt`는 아무것도 출력하지 않고, 두 패키지 모두 `ok`.

- [ ] **Step 5: 커밋**

```bash
git add internal/monitor/codes.go internal/monitor/codes_test.go internal/adminapi/api.go internal/adminapi/api_test.go
git commit -m "feat: serve the status code table at /api/codes" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 2: `/llms.txt` 템플릿과 렌더링

**Files:**
- Create: `internal/llmstxt/llms.txt.tmpl`
- Create: `internal/llmstxt/llmstxt.go`
- Create: `internal/llmstxt/llmstxt_test.go`

**Interfaces:**
- Consumes: `monitor.Codes()`(Task 1).
- Produces: `llmstxt.Data{BaseURL string; Zone string; Codes []monitor.CodeInfo}`(`BaseURL`이 비면 `<ssl-checker 주소>`, `Codes`가 nil이면 `monitor.Codes()`), `llmstxt.Render(Data) (string, error)`, `llmstxt.Handler(zone string, baseURL func(*http.Request) string) http.Handler`(`text/plain; charset=utf-8`, `Cache-Control: no-cache`).

템플릿의 사실(응답 필드, 한도, 오류 코드, 명령)은 `docs/guide/api.md`와 `docs/guide/onboarding.md`, 그리고 `internal/adminapi`의 실제 동작과 맞춰 검증한 문구다. 바꿀 때는 그 문서들과 같은지 확인한다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/llmstxt/llmstxt_test.go`:

```go
package llmstxt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ssl-checker/internal/monitor"
)

func render(t *testing.T, d Data) string {
	t.Helper()
	out, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRenderFillsServerValues(t *testing.T) {
	out := render(t, Data{BaseURL: "https://ssl.example.com", Zone: "acme.example.com"})
	for _, want := range []string{
		"서버 주소: https://ssl.example.com",
		"챌린지 존: acme.example.com",
		"curl -sS -X POST https://ssl.example.com/api/domains ",
		"_acme-challenge.a.com.  CNAME  <uuid>.acme.example.com.",
		`export ACMEDNS_BASE_URL="https://ssl.example.com"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(out, "<ssl-checker 주소>") {
		t.Errorf("unfilled template: %q", out)
	}
}

func TestRenderHasRequiredSections(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	if !strings.HasPrefix(out, "# ssl-checker\n\n> ") {
		t.Fatalf("llms.txt 형식(H1, 요약 인용문)이 아니다: %q", out[:40])
	}
	for _, h := range []string{
		"## 동작 방식", "## 관리자 인증", "## ACME 구성 절차", "### 1. 도메인 등록", "### 2. ACME 켜기",
		"### 3. CNAME 등록과 확인", "### 4. 인증서를 받을 서버에서 발급", "### 5. 확인과 모니터링 등록",
		"## 키 재발급, 허용 IP 변경, 끄기", "## 상태 판정", "## 흔한 오류와 해결", "## 모니터링 API", "## 하지 말 것",
	} {
		if !strings.Contains(out, h+"\n") {
			t.Errorf("missing section %q", h)
		}
	}
}

func TestRenderListsEveryStatusCode(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	for _, c := range monitor.Codes() {
		if !strings.Contains(out, "| `"+c.Code+"` | "+string(c.Status)+" | "+c.Meaning+" |") {
			t.Errorf("code %s not listed with its status and meaning", c.Code)
		}
	}
}

func TestRenderWithoutKnownBaseURLUsesPlaceholder(t *testing.T) {
	out := render(t, Data{Zone: "z.test"})
	if !strings.Contains(out, "서버 주소: <ssl-checker 주소>") {
		t.Fatal("자리표시자가 없다")
	}
}

func TestRenderNeverContainsSecrets(t *testing.T) {
	out := render(t, Data{BaseURL: "https://x", Zone: "z.test"})
	for _, bad := range []string{"password\":\"", "Bearer eyJ", "sslchk_session="} {
		if strings.Contains(out, bad) {
			t.Errorf("secret-like text %q in output", bad)
		}
	}
}

func TestHandler(t *testing.T) {
	h := Handler("z.test", func(r *http.Request) string { return "https://" + r.Host })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/llms.txt", nil)
	req.Host = "ssl.example.com"
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain; charset=utf-8" {
		t.Fatalf("content-type = %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "https://ssl.example.com/api/domains") || !strings.Contains(body, "z.test") {
		t.Fatalf("body = %q", body[:200])
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

Run: `go test ./internal/llmstxt`
Expected: FAIL (빌드 실패: 패키지에 Go 파일이 없거나 `undefined: Render`)

- [ ] **Step 3: 구현한다**

`internal/llmstxt/llms.txt.tmpl`:

````text
# ssl-checker

> 인증서 모니터링 서버입니다. 부가 기능으로 acme-dns 호환 DNS-01 검증 대행(CNAME 위임)을 제공합니다. DNS API가 없는 도메인도 acme.sh로 와일드카드를 포함한 Let's Encrypt 인증서를 발급하고 갱신할 수 있습니다.

이 문서는 에이전트가 이 서버로 ACME를 구성할 때 그대로 따라 할 절차입니다.

- 서버 주소: {{.BaseURL}}
- 챌린지 존: {{.Zone}} (챌린지 TXT는 `<uuid>.{{.Zone}}`에 기록됩니다)

## 동작 방식

1. 도메인마다 `_acme-challenge.<도메인>`에 CNAME을 **한 번** 등록합니다. 대상은 이 서버가 정해 주는 `<uuid>.{{.Zone}}`입니다. 도메인의 DNS는 이 한 줄 말고는 건드리지 않습니다.
2. 인증서를 받을 서버에서 acme.sh가 Let's Encrypt에서 받은 TXT 값을 이 서버의 `/update`로 보냅니다. acme.sh의 `dns_acmedns` 플러그인이 알아서 합니다.
3. 이 서버가 `<uuid>.{{.Zone}}`에 TXT를 기록합니다(최근 값 2개 유지: `a.com`과 `*.a.com`을 한 인증서로 받을 때 둘 다 필요합니다). Let's Encrypt가 CNAME을 따라가 TXT를 확인합니다.
4. 인증서와 개인키는 각 서버의 acme.sh가 만들고 보관합니다. 이 서버는 개인키를 보지 않습니다.

## 관리자 인증

관리 API는 관리자 키(`ADMIN_KEY`)가 필요합니다. 헤더에 `Authorization: Bearer <ADMIN_KEY>`를 넣습니다.

- 키는 사람이 제공해야 합니다. 만들어 내거나 추측하지 마세요.
- 틀린 키는 IP당 5번까지 받고, 그 뒤로는 12초에 1번만 받습니다(429 `too_many_attempts`).
- 쓰기 요청(POST, PATCH, DELETE)에는 본문이 없어도 `Content-Type: application/json` 헤더가 필요합니다(없으면 415).
- 상태 조회(GET)는 키 없이 됩니다. 오류 응답은 `{"error":"<코드>"}` 형태입니다.

아래 예시는 `ADMIN_KEY` 환경변수가 있다고 가정합니다.

```sh
A="Authorization: Bearer $ADMIN_KEY"
J="Content-Type: application/json"
```

## ACME 구성 절차

### 1. 도메인 등록

```sh
curl -sS -X POST {{.BaseURL}}/api/domains -H "$A" -H "$J" -d '{"fqdn":"a.com"}'
```

응답의 `id`를 이후 단계에서 `<id>`로 씁니다. 이미 등록돼 있으면 409 `domain_exists`입니다. 그때는 `curl -sS {{.BaseURL}}/api/domains`에서 `fqdn`이 같은 항목의 `id`를 찾습니다. 도메인은 `a.com`처럼 와일드카드 없이 씁니다. `*.a.com`도 같은 CNAME으로 처리됩니다.

### 2. ACME 켜기

```sh
curl -sS -X POST {{.BaseURL}}/api/domains/<id>/acme -H "$A" -H "$J" -d '{"allowfrom":["203.0.113.10/32"]}'
```

`allowfrom`은 `/update`를 부를 수 있는 서버의 IP나 CIDR입니다(최대 64개). 키가 새어도 다른 곳에서는 못 쓰게 가능한 한 지정합니다. 빈 목록 `[]`이면 어디서든 부를 수 있습니다.

응답(201)에는 `username`, `password`, `subdomain`, `fulldomain`, `cname_record`, `allowfrom`, `acme_sh`가 있습니다.

- **`password`는 이 응답에서만 볼 수 있습니다.** 다시 얻을 방법이 없고, 잃어버리면 키를 재발급합니다.
- 응답을 로그, 티켓, 채팅에 남기지 마세요.
- `acme_sh`는 3~4단계에서 쓸 명령입니다.
- 이미 켜져 있으면 409 `acme_enabled`입니다.

### 3. CNAME 등록과 확인

응답의 `cname_record`를 도메인(`a.com`)의 DNS에 등록합니다. 레코드 한 줄입니다.

```text
_acme-challenge.a.com.  CNAME  <uuid>.{{.Zone}}.
```

DNS 콘솔에서 하는 일이라 사람이 해야 할 수 있습니다. 등록한 뒤 확인합니다.

```sh
curl -sS -X POST {{.BaseURL}}/api/domains/<id>/acme/check -H "$A" -H "$J"
```

응답의 `cname_status`가 `ok`면 됩니다.

| `cname_status` | 뜻 |
| --- | --- |
| `ok` | CNAME이 이 서버가 정해 준 이름을 가리킵니다 |
| `missing` | CNAME이 없습니다. DNS 전파에 몇 분 걸릴 수 있으니 기다린 뒤 다시 확인합니다 |
| `mismatch` | 다른 곳을 가리킵니다. `cname_record`와 값이 같은지 봅니다 |
| `error` | 조회에 실패했습니다. 잠시 뒤 다시 확인합니다 |

### 4. 인증서를 받을 서버에서 발급

acme.sh 3.1.4 이상이 필요합니다. 없으면 `curl https://get.acme.sh | sh -s email=<연락 이메일>`로 설치합니다. 2단계 응답의 `acme_sh`에 아래 명령이 값이 채워진 채로 들어 있습니다.

```sh
export ACMEDNS_BASE_URL="{{.BaseURL}}"
export ACMEDNS_USERNAME="<username>"
read -rs ACMEDNS_PASSWORD && export ACMEDNS_PASSWORD   # 비밀번호가 셸 기록에 남지 않게 입력으로 받는다
export ACMEDNS_SUBDOMAIN="<subdomain>"
~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
```

처음 연결하는 도메인은 **스테이징으로 먼저** 확인합니다. 운영 CA의 발급 횟수 제한에 걸리지 않습니다.

```sh
~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d a.com -d '*.a.com'
# "Cert success."가 나오면 스테이징 인증서를 지운다
~/.acme.sh/acme.sh --remove -d a.com --ecc
rm -rf ~/.acme.sh/a.com_ecc
```

운영 발급과 설치입니다.

```sh
~/.acme.sh/acme.sh --issue --dns dns_acmedns -d a.com -d '*.a.com'
sudo mkdir -p /etc/ssl/a.com
~/.acme.sh/acme.sh --install-cert -d a.com --ecc \
  --key-file       /etc/ssl/a.com/key.pem \
  --fullchain-file /etc/ssl/a.com/fullchain.pem \
  --reloadcmd      "sudo systemctl reload nginx"
```

갱신은 acme.sh가 설치한 cron이 알아서 합니다. 처음 발급이 끝나면 자격 증명이 acme.sh 설정에 저장되므로, 환경변수는 다시 설정하지 않아도 됩니다.

### 5. 확인과 모니터링 등록

```sh
curl -sS {{.BaseURL}}/api/domains/<id>
```

`acme.last_update_at`이 채워지고 `acme.consecutive_failures`가 0이면 TXT 갱신이 되고 있는 것입니다.

이 도메인의 인증서를 계속 점검하려면 모니터링 대상을 추가합니다. 응답에 즉시 점검한 결과가 들어 있습니다.

```sh
curl -sS -X POST {{.BaseURL}}/api/domains/<id>/targets -H "$A" -H "$J" \
  -d '{"hostname":"www.a.com","port":443,"ips":["203.0.113.10"]}'
```

- `ips`를 생략하면 호스트명으로 한 번만 접속합니다(DNS가 여러 주소를 줘도 하나만 점검). 서버마다 같은 인증서가 깔렸는지 보려면 `ips`에 서버 IP를 모두 적습니다(최대 16개).
- 다시 점검하려면 `POST {{.BaseURL}}/api/targets/<target_id>/check`를 부릅니다.
- 서버는 이후 주기적으로 모든 대상을 점검합니다.

## 키 재발급, 허용 IP 변경, 끄기

| 하려는 일 | 호출 |
| --- | --- |
| 키 재발급 | `POST /api/domains/<id>/acme/rotate` (200, 일회성 자격 증명). 서브도메인과 CNAME은 그대로이고 옛 키는 즉시 무효입니다 |
| 허용 IP 변경 | `PATCH /api/domains/<id>/acme` `{"allowfrom":["203.0.113.0/24"]}` |
| 끄기 | `DELETE /api/domains/<id>/acme` (204). 챌린지 TXT를 지웁니다. 다시 켜면 서브도메인이 새로 생겨 CNAME을 다시 걸어야 합니다 |

**키를 재발급하면 서버에서 `--issue ... --force`를 다시 실행해야 합니다.** 재발급 응답의 `acme_sh`에 들어 있습니다. 그렇게 하지 않으면 acme.sh 설정에 옛 키가 남아, 다음 자동 갱신이 401로 실패합니다.

## 상태 판정

도메인 상태는 대상과 ACME 상태 중 가장 나쁜 것입니다. 나쁜 순서는 `critical` > `warning` > `info` > `ok` > `unknown`입니다.

| 코드 | 상태 | 뜻 |
| --- | --- | --- |
{{- range .Codes}}
| `{{.Code}}` | {{.Status}} | {{.Meaning}} |
{{- end}}

## 흔한 오류와 해결

- **`/update`가 401 `forbidden`:** username이나 password가 틀렸거나, 키를 재발급한 뒤 옛 키를 쓰고 있거나, 호출한 서버의 IP가 `allowfrom`에 없을 때입니다. ACME를 끈 도메인의 계정도 401입니다. 이 서버가 프록시 뒤에 있으면 관리자에게 `TRUSTED_PROXIES` 설정을 확인해 달라고 합니다.
- **`/update`가 400 `bad_txt`:** TXT 값이 43자 `[A-Za-z0-9_-]` 형식이 아닙니다. acme.sh가 아닌 도구로 직접 호출했을 때 생깁니다.
- **`/update`가 429 `rate_limited`:** 계정별 호출 빈도 제한입니다(처음 10번, 이후 6초에 1번). 잠시 뒤 다시 시도합니다.
- **`/update`가 500 `dns_provider_error`:** 이 서버가 DNS 공급자를 호출하지 못했습니다. 서버 관리자에게 `{{.BaseURL}}/health`를 확인해 달라고 합니다.
- **Let's Encrypt 검증 실패:** `acme/check`로 `cname_status`가 `ok`인지 먼저 봅니다. 도메인에 CAA 레코드가 있으면 `letsencrypt.org`를 허용해야 합니다.
- **`CNAME_MISMATCH`:** ACME를 껐다 다시 켜면 서브도메인이 바뀝니다. 새 `cname_record`의 값으로 CNAME을 고칩니다.
- **acme.sh가 "Skipping. Next renewal time is"를 출력:** 이미 발급된 인증서가 있어서 건너뛴 것입니다. 새 키를 저장해야 하면 `--force`를 붙입니다.

## 모니터링 API

조회는 키가 필요 없습니다.

- `GET {{.BaseURL}}/api/summary`: 상태별 도메인 수
- `GET {{.BaseURL}}/api/domains?status=critical`: 도메인 목록(상태 필터 선택)
- `GET {{.BaseURL}}/api/domains/<id>`: 대상·접속 주소별 점검 결과와 ACME 상태
- `GET {{.BaseURL}}/api/targets/<target_id>/results?limit=50`: 점검 이력
- `GET {{.BaseURL}}/api/codes`: 위 상태 코드 표(JSON)

관리자 전용: `PATCH /api/domains/<id>`(메모), `DELETE /api/domains/<id>`(도메인 삭제, ACME가 켜져 있으면 먼저 끄고 TXT를 지웁니다), `PATCH`·`DELETE /api/targets/<target_id>`, `GET /api/audit`(감사 로그).

## 하지 말 것

- `ADMIN_KEY`와 ACME `password`를 로그, 티켓, 채팅, 저장소에 남기지 않습니다.
- 운영 CA로 발급 시험을 반복하지 않습니다. 먼저 `--staging`으로 확인합니다.
- 도메인의 DNS는 `_acme-challenge` CNAME 한 줄 말고는 바꾸지 않습니다.
- `allowfrom`을 비워 두지 않습니다.
````

`internal/llmstxt/llmstxt.go`:

```go
// Package llmstxt는 에이전트가 읽는 /llms.txt를 만든다. 이 서버의 실제 주소와 챌린지 존이 채워진다.
package llmstxt

import (
	"bytes"
	_ "embed"
	"net/http"
	"text/template"

	"ssl-checker/internal/monitor"
)

//go:embed llms.txt.tmpl
var source string

var tmpl = template.Must(template.New("llms.txt").Parse(source))

// 서버 주소를 알 수 없을 때(PUBLIC_URL이 없고 Host도 쓸 수 없을 때) 문서에 넣는 자리표시자.
const unknownBaseURL = "<ssl-checker 주소>"

// Data는 템플릿에 채우는 값이다.
type Data struct {
	BaseURL string // 비어 있으면 자리표시자를 쓴다
	Zone    string // CHALLENGE_ZONE
	Codes   []monitor.CodeInfo
}

// Render는 /llms.txt 본문이다.
func Render(d Data) (string, error) {
	if d.BaseURL == "" {
		d.BaseURL = unknownBaseURL
	}
	if d.Codes == nil {
		d.Codes = monitor.Codes()
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, d); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Handler는 GET /llms.txt다. baseURL은 요청에서 서버 주소를 만드는 함수다(PUBLIC_URL 우선).
func Handler(zone string, baseURL func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := Render(Data{BaseURL: baseURL(r), Zone: zone})
		if err != nil {
			http.Error(w, "llms.txt render failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache") // PUBLIC_URL이 없으면 요청마다 주소가 달라질 수 있다
		_, _ = w.Write([]byte(body))
	})
}
```

- [ ] **Step 4: 통과를 확인한다**

Run: `gofmt -l internal/ && go vet ./internal/llmstxt && go test ./internal/llmstxt -v`
Expected: 테스트 6개 모두 PASS.

- [ ] **Step 5: 커밋**

```bash
git add internal/llmstxt
git commit -m "feat: add /llms.txt content for agents configuring ACME" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 3: 화면 내장·제공(`webui`)과 serve 연결

**Files:**
- Create: `internal/webui/webui.go`
- Create: `internal/webui/webui_test.go`
- Create: `internal/webui/dist/.keep` (빈 파일)
- Create: `internal/app/serve_test.go`
- Modify: `.gitignore`
- Modify: `internal/app/serve.go`

**Interfaces:**
- Consumes: `llmstxt.Handler`(Task 2), `adminapi.API.PublicURLFor`, 기존 `h.Routes()`·`api.Register`.
- Produces: `webui.Handler(next http.Handler) http.Handler`(내장 `dist` 제공), `webui.New(next http.Handler, files fs.FS) http.Handler`(테스트용 파일 시스템 주입), `app.newServeHandler(cfg config.Config, pool *pgxpool.Pool, provider dnsprovider.Provider) http.Handler`(acme-dns 경로 + `/api` + `/llms.txt` + 화면을 묶은 serve 핸들러 전체).

- [ ] **Step 1: webui 테스트를 쓴다**

`internal/webui/webui_test.go`:

```go
package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

var built = fstest.MapFS{
	"index.html":     {Data: []byte("<!doctype html><title>app</title>")},
	"assets/app.js":  {Data: []byte("console.log(1)")},
	"assets/app.css": {Data: []byte("body{}")},
	".keep":          {Data: nil},
}

// next는 화면이 가로채지 않은 요청이 도착했다는 표시로 418을 돌려준다.
var next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

func do(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServesIndexWithSecurityHeaders(t *testing.T) {
	rec := do(New(next, built), "GET", "/")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>app</title>") {
		t.Fatalf("/ = %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("csp = %q", csp)
	}
	if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("headers = %v", rec.Header())
	}
}

func TestIndexHTMLIsNotRedirected(t *testing.T) {
	// http.ServeFile은 /index.html을 ./로 되돌린다. 우리는 그렇게 하지 않는다.
	if rec := do(New(next, built), "GET", "/index.html"); rec.Code != 200 {
		t.Fatalf("/index.html = %d", rec.Code)
	}
}

func TestSPAFallback(t *testing.T) {
	for _, p := range []string{"/domains/3", "/audit", "/anything/else"} {
		rec := do(New(next, built), "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<title>app</title>") {
			t.Errorf("%s = %d %q", p, rec.Code, rec.Body)
		}
	}
}

func TestAssetsAreImmutableAndTyped(t *testing.T) {
	rec := do(New(next, built), "GET", "/assets/app.js")
	if rec.Code != 200 || rec.Body.String() != "console.log(1)" {
		t.Fatalf("app.js = %d %q", rec.Code, rec.Body)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("cache-control = %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("content-type = %q", ct)
	}
	if ct := do(New(next, built), "GET", "/assets/app.css").Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("css content-type = %q", ct)
	}
}

func TestMissingAssetIs404NotHTML(t *testing.T) {
	rec := do(New(next, built), "GET", "/assets/missing.js")
	if rec.Code != 404 || strings.Contains(rec.Body.String(), "<title>") {
		t.Fatalf("missing asset = %d %q", rec.Code, rec.Body)
	}
}

func TestDotFilesAreNotServed(t *testing.T) {
	rec := do(New(next, built), "GET", "/.keep")
	if !strings.Contains(rec.Body.String(), "<title>app</title>") {
		t.Fatalf("/.keep = %d %q (index로 폴백해야 한다)", rec.Code, rec.Body)
	}
}

func TestReservedPathsGoToNext(t *testing.T) {
	h := New(next, built)
	for _, p := range []string{"/api", "/api/summary", "/api/nope", "/update", "/register", "/health", "/llms.txt"} {
		if rec := do(h, "GET", p); rec.Code != http.StatusTeapot {
			t.Errorf("GET %s = %d, want next", p, rec.Code)
		}
	}
}

func TestOtherMethodsGoToNext(t *testing.T) {
	h := New(next, built)
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if rec := do(h, m, "/"); rec.Code != http.StatusTeapot {
			t.Errorf("%s / = %d, want next", m, rec.Code)
		}
	}
}

func TestHeadHasNoBody(t *testing.T) {
	rec := do(New(next, built), "HEAD", "/")
	if rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("HEAD / = %d %q", rec.Code, rec.Body)
	}
}

func TestPlaceholderWhenNotBuilt(t *testing.T) {
	empty := fstest.MapFS{".keep": {}}
	rec := do(New(next, empty), "GET", "/")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Fatalf("placeholder = %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q", ct)
	}
	// 화면 경로도 같은 안내를 보여 주고, API 경로는 그대로 next로 간다.
	if rec := do(New(next, empty), "GET", "/domains/1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/domains/1 = %d", rec.Code)
	}
	if rec := do(New(next, empty), "GET", "/api/summary"); rec.Code != http.StatusTeapot {
		t.Fatalf("/api/summary = %d", rec.Code)
	}
}

func TestEmbeddedDistBuilds(t *testing.T) {
	// 저장소에는 dist/.keep만 있어도(또는 화면을 빌드했어도) Handler가 만들어지고 응답한다.
	rec := do(Handler(next), "GET", "/")
	if rec.Code != 200 && rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("/ = %d", rec.Code)
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

Run: `go test ./internal/webui`
Expected: FAIL (빌드 실패: 패키지에 Go 파일이 없거나 `undefined: New`)

- [ ] **Step 3: webui를 구현하고 `dist/.keep`을 만든다**

`dist/.keep`은 `go:embed all:dist`가 디렉터리를 찾게 하는 자리다. 화면 빌드 결과(`dist/` 안의 나머지)는 커밋하지 않는다.

```bash
mkdir -p internal/webui/dist && touch internal/webui/dist/.keep
```

`.gitignore`에 아래를 더한다:

```diff
diff --git a/.gitignore b/.gitignore
index 964a7d9..b1ce415 100644
--- a/.gitignore
+++ b/.gitignore
@@ -3,3 +3,8 @@
 /bin/
 .env
 .claude/worktrees/
+
+# 대시보드(SPA) 빌드 결과와 의존성. dist/.keep만 커밋해 Node 없이도 go build가 되게 한다.
+/internal/webui/dist/*
+!/internal/webui/dist/.keep
+/web/node_modules/
```

`internal/webui/webui.go`:

```go
// Package webui는 내장한 대시보드(SPA) 정적 파일을 제공한다.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist는 web/에서 `npm run build`가 만든 결과다. 저장소에는 .keep만 있어서 Node 없이도 go build가 된다.
//
//go:embed all:dist
var embedded embed.FS

// csp는 화면이 같은 출처의 스크립트·스타일만 쓰게 하고, 다른 사이트가 화면을 iframe으로 감싸지 못하게 한다.
const csp = "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

const placeholder = `<!doctype html>
<html lang="ko"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>ssl-checker</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 40rem; margin: 3rem auto; padding: 0 1rem">
<h1>화면이 빌드되지 않았습니다</h1>
<p>이 바이너리에는 대시보드가 들어 있지 않습니다. <code>web/</code>에서 <code>npm ci &amp;&amp; npm run build</code>를 실행한 뒤 다시 빌드하세요. Docker 이미지는 빌드할 때 화면을 함께 만듭니다.</p>
<p>API(<code>/api</code>)와 <code>/llms.txt</code>는 화면과 상관없이 동작합니다.</p>
</body></html>
`

// Handler는 내장한 화면을 제공하고, 그 밖의 요청은 next로 넘긴다.
func Handler(next http.Handler) http.Handler {
	files, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist는 컴파일할 때 내장돼 항상 있다
	}
	return New(next, files)
}

// New는 files(Vite 빌드 출력)를 제공하는 핸들러다. 테스트가 가짜 파일 시스템을 넣을 수 있게 Handler와 나눴다.
func New(next http.Handler, files fs.FS) http.Handler {
	return &handler{next: next, files: files}
}

type handler struct {
	next  http.Handler
	files fs.FS
}

// reserved는 서버 API가 맡는 경로다. 화면이 가로채면 안 된다(GET /update의 405 같은 기존 응답이 달라진다).
func reserved(p string) bool {
	switch p {
	case "/update", "/register", "/health", "/llms.txt", "/api":
		return true
	}
	return strings.HasPrefix(p, "/api/")
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || reserved(r.URL.Path) {
		h.next.ServeHTTP(w, r)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	data, err := h.read(name)
	if err != nil {
		if strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r) // 없는 스크립트에 HTML을 돌려주면 브라우저가 구문 오류만 낸다
			return
		}
		// 화면 경로(/domains/3 등)는 SPA가 처리한다.
		name = "index.html"
		if data, err = h.read(name); err != nil {
			servePlaceholder(w)
			return
		}
	}
	hd := w.Header()
	hd.Set("Content-Security-Policy", csp)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("X-Frame-Options", "DENY")
	hd.Set("Referrer-Policy", "same-origin")
	if strings.HasPrefix(name, "assets/") {
		hd.Set("Cache-Control", "public, max-age=31536000, immutable") // 파일 이름에 해시가 들어 있다
	} else {
		hd.Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// read는 내장 파일 하나를 읽는다. .keep 같은 점 파일과 디렉터리는 없는 것으로 본다.
func (h *handler) read(name string) ([]byte, error) {
	if strings.HasPrefix(path.Base(name), ".") {
		return nil, fs.ErrNotExist
	}
	return fs.ReadFile(h.files, name)
}

func servePlaceholder(w http.ResponseWriter) {
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(placeholder))
}
```

- [ ] **Step 4: webui 통과를 확인한다**

Run: `gofmt -l internal/ && go vet ./internal/webui && go test ./internal/webui -v`
Expected: 테스트 11개 모두 PASS. `git status --short internal/webui`에 `dist/.keep`이 새 파일로 보여야 한다(`.gitignore`가 막지 않는다).

- [ ] **Step 5: serve 연결 테스트를 쓴다**

`internal/app/serve_test.go`:

```go
package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ssl-checker/internal/config"
	"ssl-checker/internal/db/dbtest"
	"ssl-checker/internal/dnsprovider/memory"
)

func serveHandler(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		DNSProvider: "memory", ChallengeZone: "acme.test", TXTTTL: 60, AdminKey: strings.Repeat("k", 32),
		CheckInterval: time.Hour, DNSResolver: "127.0.0.1:1",
	}
	return newServeHandler(cfg, dbtest.New(t), memory.New())
}

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestServeHandlerKeepsAPIPathsAheadOfTheUI(t *testing.T) {
	h := serveHandler(t)

	// 화면이 가로채지 않는다: 예전과 같은 405, JSON 404.
	rec := get(h, "GET", "/update")
	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Header().Get("Allow"), "POST") {
		t.Fatalf("GET /update = %d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
	rec = get(h, "GET", "/api/nope")
	if rec.Code != http.StatusNotFound || !strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("GET /api/nope = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(h, "POST", "/register"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("POST /register without key = %d", rec.Code)
	}

	rec = get(h, "GET", "/api/summary")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"domains":0`) {
		t.Fatalf("GET /api/summary = %d %q", rec.Code, rec.Body)
	}
}

func TestServeHandlerServesLLMSTxt(t *testing.T) {
	rec := get(serveHandler(t), "GET", "/llms.txt")
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/plain") {
		t.Fatalf("GET /llms.txt = %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if body := rec.Body.String(); !strings.Contains(body, "챌린지 존: acme.test") || !strings.Contains(body, "http://example.com/api/domains") {
		t.Fatalf("body = %q", body[:300])
	}
}

func TestServeHandlerServesTheUI(t *testing.T) {
	h := serveHandler(t)
	for _, path := range []string{"/", "/domains/1", "/audit"} {
		rec := get(h, "GET", path)
		// 화면을 빌드했으면 200, 안 했으면 안내 페이지(503)다. 어느 쪽이든 HTML이다.
		if (rec.Code != 200 && rec.Code != http.StatusServiceUnavailable) || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s = %d %q", path, rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}
```

- [ ] **Step 6: 실패를 확인한다**

Run: `go vet ./internal/app`
Expected: FAIL (`undefined: newServeHandler`)

- [ ] **Step 7: serve.go를 바꾼다**

`Serve`가 직접 조립하던 핸들러를 `newServeHandler`로 빼고, `/llms.txt`와 화면을 붙인다:

```diff
diff --git a/internal/app/serve.go b/internal/app/serve.go
index c25d9b6..2cdf426 100644
--- a/internal/app/serve.go
+++ b/internal/app/serve.go
@@ -8,14 +8,19 @@ import (
 	"net/http"
 	"time"
 
+	"github.com/jackc/pgx/v5/pgxpool"
+
 	"ssl-checker/internal/acmedns"
 	"ssl-checker/internal/adminapi"
 	"ssl-checker/internal/cnamecheck"
 	"ssl-checker/internal/config"
 	"ssl-checker/internal/db"
+	"ssl-checker/internal/dnsprovider"
 	"ssl-checker/internal/httpx"
+	"ssl-checker/internal/llmstxt"
 	"ssl-checker/internal/monitor"
 	"ssl-checker/internal/store"
+	"ssl-checker/internal/webui"
 )
 
 const (
@@ -50,6 +55,34 @@ func Serve(ctx context.Context, args []string, getenv func(string) string) error
 		return err
 	}
 
+	srv := &http.Server{
+		Addr:              *listen,
+		Handler:           newServeHandler(cfg, pool, provider),
+		ReadHeaderTimeout: 5 * time.Second,
+		ReadTimeout:       10 * time.Second,
+		WriteTimeout:      30 * time.Second,
+		IdleTimeout:       60 * time.Second,
+	}
+	errc := make(chan error, 1)
+	go func() { errc <- srv.ListenAndServe() }()
+	slog.Info("serving", "addr", *listen, "provider", cfg.DNSProvider, "zone", cfg.ChallengeZone)
+
+	select {
+	case err := <-errc:
+		return err
+	case <-ctx.Done():
+	}
+	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
+	defer cancel()
+	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
+		return err
+	}
+	return nil
+}
+
+// newServeHandler는 serve가 여는 핸들러 전체다: acme-dns 경로, /api, /llms.txt, 내장한 화면.
+// 화면은 API가 맡은 경로를 가로채지 않는다.
+func newServeHandler(cfg config.Config, pool *pgxpool.Pool, provider dnsprovider.Provider) http.Handler {
 	ip := httpx.ClientIP{Trusted: cfg.TrustedProxies}
 	st := store.New(pool)
 	svc := &acmedns.Service{Store: st, Provider: provider, Zone: cfg.ChallengeZone, TTL: cfg.TXTTTL}
@@ -72,35 +105,13 @@ func Serve(ctx context.Context, args []string, getenv func(string) string) error
 		Log:    slog.Default(),
 	}
 
-	// acme-dns 경로는 기존 핸들러에 그대로 넘기고, /api는 같은 mux에 붙인다.
+	// acme-dns 경로는 기존 핸들러에 그대로 넘기고, /api와 /llms.txt는 같은 mux에 붙인다.
 	mux := http.NewServeMux()
 	legacy := h.Routes()
 	mux.Handle("POST /update", legacy)
 	mux.Handle("POST /register", legacy)
 	mux.Handle("GET /health", legacy)
 	api.Register(mux)
-
-	srv := &http.Server{
-		Addr:              *listen,
-		Handler:           mux,
-		ReadHeaderTimeout: 5 * time.Second,
-		ReadTimeout:       10 * time.Second,
-		WriteTimeout:      30 * time.Second,
-		IdleTimeout:       60 * time.Second,
-	}
-	errc := make(chan error, 1)
-	go func() { errc <- srv.ListenAndServe() }()
-	slog.Info("serving", "addr", *listen, "provider", cfg.DNSProvider, "zone", cfg.ChallengeZone)
-
-	select {
-	case err := <-errc:
-		return err
-	case <-ctx.Done():
-	}
-	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
-	defer cancel()
-	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
-		return err
-	}
-	return nil
+	mux.Handle("GET /llms.txt", llmstxt.Handler(cfg.ChallengeZone, api.PublicURLFor))
+	return webui.Handler(mux)
 }
```

- [ ] **Step 8: 전체 통과를 확인한다**

Run: `gofmt -l internal/ && go vet ./... && TEST_DATABASE_URL='postgres://postgres:test@localhost:55432/postgres?sslmode=disable' go test ./...`
Expected: 모든 패키지 `ok`.

- [ ] **Step 9: 커밋**

```bash
git add .gitignore internal/webui internal/app/serve.go internal/app/serve_test.go
git commit -m "feat: embed and serve the dashboard SPA and /llms.txt" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 4: `web/` 설정과 순수 로직(API 클라이언트·포맷·입력·오류 문구)

화면의 React 코드는 컴포넌트 테스트 도구 없이 타입 검사와 빌드로 확인한다. 그래서 판단이 들어가는 로직은 모두 React 밖의 순수 함수로 두고 Node 내장 테스트 러너(`node --test`, 의존성 없음)로 검증한다.

**Files:**
- Create: `web/package.json`, `web/tsconfig.json`, `web/vite.config.ts`, `web/index.html`, `web/public/.keep`
- Create: `web/src/api/types.ts`, `web/src/api/client.ts`, `web/src/api/endpoints.ts`
- Create: `web/src/lib/status.ts`, `web/src/lib/format.ts`, `web/src/lib/input.ts`, `web/src/lib/errors.ts`
- Create (테스트): `web/src/api/client.test.ts`, `web/src/api/endpoints.test.ts`, `web/src/lib/status.test.ts`, `web/src/lib/format.test.ts`, `web/src/lib/input.test.ts`, `web/src/lib/errors.test.ts`
- Generate: `web/package-lock.json` (`npm install`이 만든다. 커밋한다)

**Interfaces:**
- Consumes: 서버 JSON(`docs/guide/api.md`).
- Produces (이후 태스크가 쓴다):
  - `api/types.ts`: `Status`, `CnameStatus`, `Summary`, `Config`, `CodeInfo`, `DomainSummary`, `CheckResult`, `Target`, `AcmeOn`, `AcmeOff`, `Acme`, `DomainDetail`, `Credentials`, `AuditEntry`, `TargetInput`.
  - `api/client.ts`: `class ApiError{status:number; code:string}`, `type FetchFn`, `interface Client{get<T>(path); send<T>(method:"POST"|"PATCH"|"DELETE", path, body?)}`, `createClient(fetchFn?)`.
  - `api/endpoints.ts`: `createApi(client)`와 기본 인스턴스 `api`(`summary, config, codes, domains, domain, targetResults, audit, session, login, logout, createDomain, patchDomain, deleteDomain, createTarget, patchTarget, deleteTarget, checkTarget, enableAcme, patchAcme, rotateAcme, checkAcme, disableAcme`).
  - `lib/status.ts`: `STATUS_LABEL`, `STATUS_ORDER`, `CNAME_LABEL`, `parseStatus(v)`, `parseGoDuration(s)`(ms 또는 null), `STALE_FACTOR`, `isStale(lastCheckedAt, checkIntervalMs, now)`, `sortByUrgency(items)`.
  - `lib/format.ts`: `shortSerial`, `formatDateTime(iso, tz?)`, `formatDate(iso, tz?)`, `relativeTime(iso, now)`, `daysLeftText(days)`, `daysUntil(iso, now)`.
  - `lib/input.ts`: `splitList(text)`, `parsePort(text)`.
  - `lib/errors.ts`: `errorMessage(e)`, `KNOWN_ERROR_CODES`.

- [ ] **Step 1: 설정 파일을 만든다**

```bash
mkdir -p web/public web/src/api web/src/lib && touch web/public/.keep
```

`web/public/.keep`은 Vite가 빌드마다 `internal/webui/dist/.keep`을 되살리게 하는 자리다(`emptyOutDir`이 지우기 때문).

`web/package.json`:

```json
{
  "name": "ssl-checker-web",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc --noEmit && vite build",
    "test": "node --test 'src/**/*.test.ts'"
  },
  "dependencies": {
    "react": "^19.3.0",
    "react-dom": "^19.3.0",
    "react-router-dom": "^7.18.4"
  },
  "devDependencies": {
    "@types/node": "^24.0.0",
    "@types/react": "^19.3.0",
    "@types/react-dom": "^19.2.0",
    "@vitejs/plugin-react": "^6.1.2",
    "typescript": "^5.9.0",
    "vite": "^8.3.3"
  }
}
```

`web/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["ES2023", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "moduleResolution": "bundler",
    "jsx": "react-jsx",
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "erasableSyntaxOnly": true,
    "verbatimModuleSyntax": true,
    "allowImportingTsExtensions": true,
    "noEmit": true,
    "skipLibCheck": true,
    "types": ["node"]
  },
  "include": ["src", "vite.config.ts"]
}
```

`web/vite.config.ts`:

```ts
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 빌드 결과는 Go 바이너리가 내장하는 internal/webui/dist로 간다.
// public/.keep이 빌드 때마다 dist/.keep을 되살려, 저장소에서 .keep이 지워진 것처럼 보이지 않게 한다.
export default defineConfig({
  plugins: [react()],
  build: { outDir: "../internal/webui/dist", emptyOutDir: true },
  // npm run dev: API는 로컬에서 띄운 ssl-checker serve(:8080)로 보낸다.
  server: { proxy: { "/api": "http://localhost:8080" } },
});
```

`web/index.html`:

```html
<!doctype html>
<html lang="ko">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <meta name="color-scheme" content="light dark" />
    <title>ssl-checker</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 2: 테스트를 쓴다**

`web/src/api/client.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiError, createClient, type FetchFn } from "./client.ts";

interface Call {
  url: string;
  init: RequestInit;
}

function fake(respond: (call: Call) => Response | Error) {
  const calls: Call[] = [];
  const fetchFn: FetchFn = async (url, init = {}) => {
    const call = { url, init };
    calls.push(call);
    const r = respond(call);
    if (r instanceof Error) throw r;
    return r;
  };
  return { calls, client: createClient(fetchFn) };
}

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

function header(call: Call, name: string): string | undefined {
  return (call.init.headers as Record<string, string>)[name];
}

test("GET sends no Content-Type and no body", async () => {
  const { calls, client } = fake(() => json({ ok: 1 }));
  assert.deepEqual(await client.get("/api/x"), { ok: 1 });
  assert.equal(calls[0]?.url, "/api/x");
  assert.equal(calls[0]?.init.method, "GET");
  assert.equal(header(calls[0]!, "Content-Type"), undefined);
  assert.equal(calls[0]?.init.body, undefined);
  assert.equal(calls[0]?.init.credentials, "same-origin");
});

test("writes always send Content-Type: application/json, even without a body", async () => {
  // 서버는 본문이 없는 요청에도 JSON 헤더를 요구한다(415를 막는다).
  const { calls, client } = fake(() => new Response(null, { status: 204 }));
  await client.send("DELETE", "/api/domains/1");
  assert.equal(header(calls[0]!, "Content-Type"), "application/json");
  assert.equal(calls[0]?.init.body, undefined);
  await client.send("POST", "/api/session", { key: "k" });
  assert.equal(header(calls[1]!, "Content-Type"), "application/json");
  assert.equal(calls[1]?.init.body, '{"key":"k"}');
});

test("204 resolves to undefined", async () => {
  const { client } = fake(() => new Response(null, { status: 204 }));
  assert.equal(await client.send("DELETE", "/api/x"), undefined);
});

test("an error body becomes ApiError with status and code", async () => {
  const { client } = fake(() => json({ error: "domain_exists" }, 409));
  await assert.rejects(client.send("POST", "/api/domains", {}), (e: unknown) => {
    assert.ok(e instanceof ApiError);
    assert.equal(e.status, 409);
    assert.equal(e.code, "domain_exists");
    return true;
  });
});

test("an error response that is not JSON still becomes ApiError", async () => {
  const { client } = fake(() => new Response("<html>Bad Gateway</html>", { status: 502 }));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.status === 502 && e.code === "unknown");
});

test("a JSON error without an error field is unknown", async () => {
  const { client } = fake(() => json({ message: "x" }, 500));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.code === "unknown");
});

test("a network failure becomes status 0", async () => {
  const { client } = fake(() => new TypeError("Failed to fetch"));
  await assert.rejects(client.get("/api/x"), (e: unknown) => e instanceof ApiError && e.status === 0 && e.code === "network_error");
});
```

`web/src/api/endpoints.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { createClient, type FetchFn } from "./client.ts";
import { createApi } from "./endpoints.ts";

function setup(body: unknown = {}, status = 200) {
  const calls: { url: string; method: string; body: string | undefined }[] = [];
  const fetchFn: FetchFn = async (url, init = {}) => {
    calls.push({ url, method: String(init.method), body: init.body as string | undefined });
    return status === 204 ? new Response(null, { status }) : new Response(JSON.stringify(body), { status });
  };
  return { calls, api: createApi(createClient(fetchFn)) };
}

test("list endpoints unwrap their envelope", async () => {
  const d = setup({ domains: [{ id: 1 }] });
  assert.deepEqual(await d.api.domains(), [{ id: 1 }]);
  const c = setup({ codes: [{ code: "X" }] });
  assert.deepEqual(await c.api.codes(), [{ code: "X" }]);
  const r = setup({ results: [{ address: "a" }] });
  assert.deepEqual(await r.api.targetResults(7, 10), [{ address: "a" }]);
  assert.equal(r.calls[0]?.url, "/api/targets/7/results?limit=10");
  const s = setup({ admin: true });
  assert.equal(await s.api.session(), true);
});

test("audit paging passes limit and before_id", async () => {
  const a = setup({ entries: [] });
  await a.api.audit();
  await a.api.audit(123, 50);
  assert.equal(a.calls[0]?.url, "/api/audit?limit=100");
  assert.equal(a.calls[1]?.url, "/api/audit?limit=50&before_id=123");
});

test("ACME calls use the documented paths and bodies", async () => {
  const e = setup({});
  await e.api.enableAcme(3, ["10.0.0.0/24"]);
  await e.api.patchAcme(3, []);
  await e.api.rotateAcme(3);
  await e.api.checkAcme(3);
  assert.deepEqual(
    e.calls.map((c) => [c.method, c.url, c.body]),
    [
      ["POST", "/api/domains/3/acme", '{"allowfrom":["10.0.0.0/24"]}'],
      ["PATCH", "/api/domains/3/acme", '{"allowfrom":[]}'],
      ["POST", "/api/domains/3/acme/rotate", undefined],
      ["POST", "/api/domains/3/acme/check", undefined],
    ],
  );
  const d = setup({}, 204);
  await d.api.disableAcme(3);
  assert.deepEqual([d.calls[0]?.method, d.calls[0]?.url], ["DELETE", "/api/domains/3/acme"]);
});

test("domain and target writes", async () => {
  const w = setup({});
  await w.api.createDomain("a.com", "memo");
  await w.api.patchDomain(1, "new");
  await w.api.createTarget(1, { hostname: "www.a.com", port: 443, sni: "", ips: ["10.0.0.1"], memo: "" });
  await w.api.checkTarget(9);
  assert.deepEqual(
    w.calls.map((c) => [c.method, c.url]),
    [
      ["POST", "/api/domains"],
      ["PATCH", "/api/domains/1"],
      ["POST", "/api/domains/1/targets"],
      ["POST", "/api/targets/9/check"],
    ],
  );
  assert.equal(w.calls[0]?.body, '{"fqdn":"a.com","memo":"memo"}');
});

test("login posts the key and logout deletes the session", async () => {
  const l = setup({ admin: true });
  await l.api.login("secret");
  assert.deepEqual([l.calls[0]?.method, l.calls[0]?.url, l.calls[0]?.body], ["POST", "/api/session", '{"key":"secret"}']);
  const o = setup({}, 204);
  await o.api.logout();
  assert.deepEqual([o.calls[0]?.method, o.calls[0]?.url], ["DELETE", "/api/session"]);
});
```

`web/src/lib/status.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import type { Status } from "../api/types.ts";
import { isStale, parseGoDuration, parseStatus, sortByUrgency, STATUS_LABEL, STATUS_ORDER } from "./status.ts";

test("parseGoDuration reads Go duration strings", () => {
  assert.equal(parseGoDuration("6h0m0s"), 6 * 3600_000);
  assert.equal(parseGoDuration("5m0s"), 5 * 60_000);
  assert.equal(parseGoDuration("1h30m0s"), 90 * 60_000);
  assert.equal(parseGoDuration("30s"), 30_000);
  assert.equal(parseGoDuration("1.5s"), 1500);
});

test("parseGoDuration rejects what it does not understand", () => {
  for (const bad of ["", "6", "abc", "6x", "-5m", "1d"]) assert.equal(parseGoDuration(bad), null, bad);
});

test("parseStatus accepts only known statuses", () => {
  assert.equal(parseStatus("critical"), "critical");
  assert.equal(parseStatus("unknown"), "unknown");
  assert.equal(parseStatus("nope"), null);
  assert.equal(parseStatus(null), null);
});

test("every status has a label and order is worst first", () => {
  assert.deepEqual([...STATUS_ORDER], ["critical", "warning", "info", "ok", "unknown"]);
  for (const s of STATUS_ORDER) assert.ok(STATUS_LABEL[s].length > 0);
});

test("isStale flags results older than twice the interval", () => {
  const interval = 6 * 3600_000;
  const checked = "2026-10-07T00:00:00Z";
  const at = Date.parse(checked);
  assert.equal(isStale(checked, interval, at + 2 * interval), false); // 딱 2배는 아직 괜찮다
  assert.equal(isStale(checked, interval, at + 2 * interval + 1), true);
  assert.equal(isStale(checked, interval, at + interval), false);
});

test("isStale is false when there is nothing to compare", () => {
  const now = Date.parse("2030-01-01T00:00:00Z");
  assert.equal(isStale(null, 3600_000, now), false); // 미점검은 stale이 아니라 unknown이다
  assert.equal(isStale("2026-10-07T00:00:00Z", null, now), false); // 주기를 모르면 판단하지 않는다
  assert.equal(isStale("not a date", 3600_000, now), false);
});

test("sortByUrgency puts worse statuses first, then sooner expiry, then name", () => {
  const d = (fqdn: string, status: Status, days_left: number | null) => ({ fqdn, status, days_left });
  const sorted = sortByUrgency([
    d("z.com", "ok", 80),
    d("b.com", "critical", null),
    d("a.com", "ok", 10),
    d("c.com", "critical", 2),
    d("e.com", "unknown", null),
    d("d.com", "critical", null),
    d("f.com", "warning", 5),
  ]).map((x) => x.fqdn);
  assert.deepEqual(sorted, ["c.com", "b.com", "d.com", "f.com", "a.com", "z.com", "e.com"]);
});

test("sortByUrgency does not change its input", () => {
  const input = [
    { fqdn: "b", status: "ok" as Status, days_left: 1 },
    { fqdn: "a", status: "critical" as Status, days_left: 1 },
  ];
  sortByUrgency(input);
  assert.equal(input[0]?.fqdn, "b");
});
```

`web/src/lib/format.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { daysLeftText, daysUntil, formatDate, formatDateTime, relativeTime, shortSerial } from "./format.ts";

test("shortSerial keeps the first 8 characters", () => {
  assert.equal(shortSerial("04ab12cd34ef56"), "04ab12cd");
  assert.equal(shortSerial("ab"), "ab");
  assert.equal(shortSerial(""), "");
});

test("formatDateTime renders a fixed pattern in the given time zone", () => {
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "UTC"), "2026-10-07 12:34");
  assert.equal(formatDateTime("2026-10-07T12:34:56Z", "Asia/Seoul"), "2026-10-07 21:34");
  assert.equal(formatDateTime("2026-10-07T15:00:00Z", "Asia/Seoul"), "2026-10-08 00:00"); // 자정은 24시가 아니라 00시
});

test("formatDateTime and formatDate give - for missing or invalid values", () => {
  for (const v of [null, undefined, "", "garbage"]) {
    assert.equal(formatDateTime(v), "-");
    assert.equal(formatDate(v), "-");
  }
  assert.equal(formatDate("2026-10-07T12:34:56Z", "UTC"), "2026-10-07");
});

test("relativeTime counts in minutes, hours and days", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  const at = (ms: number) => new Date(now - ms).toISOString();
  assert.equal(relativeTime(at(10_000), now), "방금 전");
  assert.equal(relativeTime(at(5 * 60_000), now), "5분 전");
  assert.equal(relativeTime(at(3 * 3600_000), now), "3시간 전");
  assert.equal(relativeTime(at(50 * 3600_000), now), "2일 전");
  assert.equal(relativeTime(at(-2 * 3600_000), now), "2시간 후");
  assert.equal(relativeTime(at(-10_000), now), "곧");
  assert.equal(relativeTime(null, now), "-");
  assert.equal(relativeTime("garbage", now), "-");
});

test("daysLeftText", () => {
  assert.equal(daysLeftText(13), "13일 남음");
  assert.equal(daysLeftText(0), "오늘 만료");
  assert.equal(daysLeftText(-3), "3일 지남");
  assert.equal(daysLeftText(null), "-");
  assert.equal(daysLeftText(undefined), "-");
});

test("daysUntil floors toward the past", () => {
  const now = Date.parse("2026-10-07T12:00:00Z");
  assert.equal(daysUntil("2026-10-20T00:00:00Z", now), 12);
  assert.equal(daysUntil("2026-10-07T13:00:00Z", now), 0);
  assert.equal(daysUntil("2026-10-06T00:00:00Z", now), -2);
  assert.equal(daysUntil(undefined, now), null);
  assert.equal(daysUntil("garbage", now), null);
});
```

`web/src/lib/input.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { parsePort, splitList } from "./input.ts";

test("splitList splits on newlines, commas and spaces", () => {
  assert.deepEqual(splitList("10.0.0.1\n10.0.0.2, 10.0.0.3  10.0.0.4"), ["10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"]);
});

test("splitList drops empty items and duplicates but keeps order", () => {
  assert.deepEqual(splitList(" a,, b\n\n a ,c "), ["a", "b", "c"]);
  assert.deepEqual(splitList(""), []);
  assert.deepEqual(splitList(" \n , "), []);
});

test("parsePort accepts 1..65535 only", () => {
  assert.equal(parsePort("443"), 443);
  assert.equal(parsePort(" 8443 "), 8443);
  assert.equal(parsePort("1"), 1);
  assert.equal(parsePort("65535"), 65535);
  for (const bad of ["", "0", "65536", "-1", "44.3", "abc", "443a", "123456"]) assert.equal(parsePort(bad), null, bad);
});
```

`web/src/lib/errors.test.ts`:

```ts
import assert from "node:assert/strict";
import { test } from "node:test";
import { ApiError } from "../api/client.ts";
import { errorMessage, KNOWN_ERROR_CODES } from "./errors.ts";

// 서버(internal/adminapi)가 내는 오류 코드 전부. 새 코드를 만들면 여기와 errors.ts에 함께 더한다.
const SERVER_CODES = [
  "unauthorized", "too_many_attempts", "unsupported_media_type", "malformed_json", "method_not_allowed",
  "invalid_status", "invalid_limit", "invalid_before_id", "not_found", "db_error", "invalid_domain",
  "invalid_memo", "domain_exists", "invalid_target", "target_exists", "acme_enabled", "acme_disabled",
  "invalid_allowfrom", "dns_provider_error",
];

test("every server error code has a message", () => {
  for (const code of SERVER_CODES) {
    assert.ok(KNOWN_ERROR_CODES.includes(code), `${code} has no message`);
    assert.ok(errorMessage(new ApiError(400, code)).length > 0);
  }
});

test("an unknown code shows the code itself", () => {
  assert.match(errorMessage(new ApiError(418, "teapot")), /teapot/);
});

test("a non-ApiError becomes a generic message", () => {
  assert.equal(errorMessage(new Error("boom")), "알 수 없는 오류입니다.");
  assert.equal(errorMessage("x"), "알 수 없는 오류입니다.");
});

test("network failures are readable", () => {
  assert.match(errorMessage(new ApiError(0, "network_error")), /연결/);
});
```

- [ ] **Step 3: 설치하고 실패를 확인한다**

Run: `cd web && npm install --no-audit --no-fund && npm test`
Expected: FAIL (`Cannot find module './client.ts'` 등). `web/package-lock.json`이 생긴다.

- [ ] **Step 4: 구현한다**

`web/src/api/types.ts`:

```ts
// /api 응답 모양. 서버의 internal/adminapi와 internal/monitor가 만드는 JSON과 같아야 한다.

export type Status = "critical" | "warning" | "info" | "ok" | "unknown";
export type CnameStatus = "unknown" | "ok" | "missing" | "mismatch" | "error";

export interface Summary {
  domains: number;
  critical: number;
  warning: number;
  info: number;
  ok: number;
  unknown: number;
  checked_at: string | null;
}

export interface Config {
  challenge_zone: string;
  check_interval: string; // Go 시간 문자열. 예: "6h0m0s"
  public_url: string;
}

export interface CodeInfo {
  code: string;
  status: Status;
  meaning: string;
}

export interface DomainSummary {
  id: number;
  fqdn: string;
  memo: string;
  status: Status;
  reasons: string[];
  earliest_not_after: string | null;
  days_left: number | null;
  targets: number;
  acme: { enabled: boolean; cname_status?: CnameStatus };
  last_checked_at: string | null;
}

/** 접속 주소 하나의 점검 한 건. 인증서를 받지 못한 결과에는 not_before/not_after가 아예 없다. */
export interface CheckResult {
  address: string;
  checked_at: string;
  status: Status;
  reasons: string[];
  tls_version: string;
  cipher: string;
  subject_cn: string;
  sans: string[];
  issuer: string;
  serial: string;
  not_before?: string;
  not_after?: string;
  key_alg: string;
  key_bits: number;
  chain_ok: boolean;
  hostname_ok: boolean;
  error: string;
}

export interface Target {
  id: number;
  hostname: string;
  port: number;
  sni: string;
  ips: string[];
  memo: string;
  status: Status;
  reasons: string[];
  results: CheckResult[];
}

export interface AcmeOff {
  enabled: false;
}

export interface AcmeOn {
  enabled: true;
  username: string;
  subdomain: string;
  fulldomain: string;
  cname_record: string;
  allowfrom: string[];
  cname_status: CnameStatus;
  cname_value: string;
  cname_checked_at: string | null;
  last_update_at: string | null;
  last_failure_at: string | null;
  consecutive_failures: number;
}

export type Acme = AcmeOff | AcmeOn;

export interface DomainDetail {
  id: number;
  fqdn: string;
  memo: string;
  created_at: string;
  status: Status;
  reasons: string[];
  targets: Target[];
  acme: Acme;
}

/** ACME를 켜거나 키를 재발급할 때 한 번만 받는 자격 증명. password는 이 응답에만 있다. */
export interface Credentials {
  username: string;
  password: string;
  subdomain: string;
  fulldomain: string;
  cname_record: string;
  allowfrom: string[];
  acme_sh: string;
}

export interface AuditEntry {
  id: number;
  at: string;
  actor: string;
  action: string;
  target: string;
  source_ip: string;
  detail: string;
}

export interface TargetInput {
  hostname: string;
  port: number;
  sni: string;
  ips: string[];
  memo: string;
}
```

`web/src/api/client.ts`:

```ts
/** 서버가 {"error":"<코드>"}로 돌려준 실패. status가 0이면 서버에 닿지 못한 것이다. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string) {
    super(`${status} ${code}`);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

export type FetchFn = (input: string, init?: RequestInit) => Promise<Response>;

export interface Client {
  get<T>(path: string): Promise<T>;
  send<T>(method: "POST" | "PATCH" | "DELETE", path: string, body?: unknown): Promise<T>;
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null;
}

/**
 * fetch 위의 얇은 JSON 클라이언트다. 서버는 GET·HEAD가 아닌 요청에 Content-Type: application/json을
 * 요구하므로(다른 사이트의 폼 위조를 막는다) 본문이 없는 DELETE에도 헤더를 붙인다.
 */
export function createClient(fetchFn: FetchFn = (input, init) => fetch(input, init)): Client {
  async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: "application/json" };
    const init: RequestInit = { method, headers, credentials: "same-origin" };
    if (method !== "GET") {
      headers["Content-Type"] = "application/json";
      if (body !== undefined) init.body = JSON.stringify(body);
    }
    let res: Response;
    try {
      res = await fetchFn(path, init);
    } catch {
      throw new ApiError(0, "network_error");
    }
    if (res.status === 204) return undefined as T;
    let data: unknown = null;
    try {
      data = await res.json();
    } catch {
      // 본문이 JSON이 아니다(프록시 오류 페이지 등). 아래에서 상태 코드만으로 처리한다.
    }
    if (!res.ok) {
      const code = isRecord(data) && typeof data.error === "string" ? data.error : "unknown";
      throw new ApiError(res.status, code);
    }
    return data as T;
  }

  return {
    get: (path) => request("GET", path),
    send: (method, path, body) => request(method, path, body),
  };
}
```

`web/src/api/endpoints.ts`:

```ts
import { createClient, type Client } from "./client.ts";
import type {
  AcmeOn,
  AuditEntry,
  CheckResult,
  CodeInfo,
  Config,
  Credentials,
  DomainDetail,
  DomainSummary,
  Summary,
  Target,
  TargetInput,
} from "./types.ts";

/** /api 엔드포인트를 타입이 있는 함수로 감싼다. 경로와 본문 모양은 docs/guide/api.md와 같다. */
export function createApi(c: Client) {
  return {
    summary: () => c.get<Summary>("/api/summary"),
    config: () => c.get<Config>("/api/config"),
    codes: () => c.get<{ codes: CodeInfo[] }>("/api/codes").then((r) => r.codes),
    domains: () => c.get<{ domains: DomainSummary[] }>("/api/domains").then((r) => r.domains),
    domain: (id: number) => c.get<DomainDetail>(`/api/domains/${id}`),
    targetResults: (targetId: number, limit = 50) =>
      c.get<{ results: CheckResult[] }>(`/api/targets/${targetId}/results?limit=${limit}`).then((r) => r.results),
    audit: (beforeId?: number, limit = 100) => {
      const q = new URLSearchParams({ limit: String(limit) });
      if (beforeId !== undefined) q.set("before_id", String(beforeId));
      return c.get<{ entries: AuditEntry[] }>(`/api/audit?${q}`).then((r) => r.entries);
    },

    session: () => c.get<{ admin: boolean }>("/api/session").then((r) => r.admin),
    login: (key: string) => c.send<{ admin: boolean }>("POST", "/api/session", { key }),
    logout: () => c.send<void>("DELETE", "/api/session"),

    createDomain: (fqdn: string, memo: string) => c.send<DomainDetail>("POST", "/api/domains", { fqdn, memo }),
    patchDomain: (id: number, memo: string) => c.send<DomainDetail>("PATCH", `/api/domains/${id}`, { memo }),
    deleteDomain: (id: number) => c.send<void>("DELETE", `/api/domains/${id}`),

    createTarget: (domainId: number, t: TargetInput) => c.send<Target>("POST", `/api/domains/${domainId}/targets`, t),
    patchTarget: (id: number, t: TargetInput) => c.send<Target>("PATCH", `/api/targets/${id}`, t),
    deleteTarget: (id: number) => c.send<void>("DELETE", `/api/targets/${id}`),
    checkTarget: (id: number) => c.send<{ results: CheckResult[] }>("POST", `/api/targets/${id}/check`),

    enableAcme: (domainId: number, allowfrom: string[]) =>
      c.send<Credentials>("POST", `/api/domains/${domainId}/acme`, { allowfrom }),
    patchAcme: (domainId: number, allowfrom: string[]) =>
      c.send<AcmeOn>("PATCH", `/api/domains/${domainId}/acme`, { allowfrom }),
    rotateAcme: (domainId: number) => c.send<Credentials>("POST", `/api/domains/${domainId}/acme/rotate`),
    checkAcme: (domainId: number) => c.send<AcmeOn>("POST", `/api/domains/${domainId}/acme/check`),
    disableAcme: (domainId: number) => c.send<void>("DELETE", `/api/domains/${domainId}/acme`),
  };
}

export const api = createApi(createClient());
```

`web/src/lib/status.ts`:

```ts
import type { CnameStatus, Status } from "../api/types.ts";

export const STATUS_LABEL: Record<Status, string> = {
  critical: "긴급",
  warning: "경고",
  info: "정보",
  ok: "정상",
  unknown: "미점검",
};

/** 나쁜 순서. 요약 카드와 필터가 이 순서로 나온다. */
export const STATUS_ORDER: readonly Status[] = ["critical", "warning", "info", "ok", "unknown"];

export const CNAME_LABEL: Record<CnameStatus, string> = {
  ok: "CNAME 정상",
  missing: "CNAME 없음",
  mismatch: "CNAME 다름",
  error: "CNAME 확인 실패",
  unknown: "CNAME 미확인",
};

/** 주소창의 ?status= 값을 상태로 바꾼다. 모르는 값이면 null(필터 없음). */
export function parseStatus(v: string | null): Status | null {
  return STATUS_ORDER.find((s) => s === v) ?? null;
}

/** Go의 time.Duration 문자열("6h0m0s", "5m0s", "30s")을 밀리초로 바꾼다. 모르는 형식이면 null. */
export function parseGoDuration(s: string): number | null {
  const m = /^(?:(\d+(?:\.\d+)?)h)?(?:(\d+(?:\.\d+)?)m)?(?:(\d+(?:\.\d+)?)s)?$/.exec(s.trim());
  if (!m || (m[1] === undefined && m[2] === undefined && m[3] === undefined)) return null;
  const [h, min, sec] = [m[1], m[2], m[3]].map((x) => (x === undefined ? 0 : Number(x)));
  return (((h ?? 0) * 60 + (min ?? 0)) * 60 + (sec ?? 0)) * 1000;
}

/** 마지막 점검이 점검 주기의 이 배수보다 오래되면 worker가 멈춘 것으로 본다. */
export const STALE_FACTOR = 2;

/**
 * 점검 결과가 오래됐는지 알려 준다. 상태는 점검할 때 저장되므로, worker가 멈추면 만료된 인증서도
 * 마지막 "정상"으로 계속 보인다. 그래서 화면이 시간으로 따로 알려야 한다.
 * 점검한 적이 없거나(미점검) 주기를 모르면 false다.
 */
export function isStale(lastCheckedAt: string | null, checkIntervalMs: number | null, now: number): boolean {
  if (!lastCheckedAt || !checkIntervalMs) return false;
  const t = Date.parse(lastCheckedAt);
  return Number.isFinite(t) && now - t > STALE_FACTOR * checkIntervalMs;
}

/**
 * 대시보드 정렬: 나쁜 상태가 먼저, 같은 상태에서는 만료가 가까운 것이 먼저, 그다음 도메인 이름순.
 * 만료일이 없는(인증서를 못 받은) 도메인은 같은 상태 안에서 뒤로 간다.
 */
export function sortByUrgency<T extends { status: Status; days_left: number | null; fqdn: string }>(items: readonly T[]): T[] {
  const rank = (s: Status) => STATUS_ORDER.indexOf(s);
  return [...items].sort(
    (a, b) =>
      rank(a.status) - rank(b.status) ||
      (a.days_left ?? Infinity) - (b.days_left ?? Infinity) || // 둘 다 없으면 NaN이라 다음 비교로 넘어간다
      a.fqdn.localeCompare(b.fqdn),
  );
}
```

`web/src/lib/format.ts`:

```ts
/** serial 앞 8자. 표에서는 이것만 보이고, 전체는 title로 보여 준다. */
export function shortSerial(serial: string): string {
  return serial.slice(0, 8);
}

function parts(d: Date, timeZone?: string): Record<string, string> {
  const fmt = new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hourCycle: "h23",
    timeZone,
  });
  return Object.fromEntries(fmt.formatToParts(d).map((p) => [p.type, p.value]));
}

/** "2026-10-07 21:00" 형식(브라우저 시간대). 값이 없거나 잘못되면 "-". */
export function formatDateTime(iso: string | null | undefined, timeZone?: string): string {
  if (!iso) return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  const p = parts(d, timeZone);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}`;
}

/** "2026-10-07". 값이 없거나 잘못되면 "-". */
export function formatDate(iso: string | null | undefined, timeZone?: string): string {
  const s = formatDateTime(iso, timeZone);
  return s === "-" ? s : s.slice(0, 10);
}

/** "3시간 전", "2일 후". 1분 미만은 "방금 전"(과거) 또는 "곧"(미래). */
export function relativeTime(iso: string | null | undefined, now: number): string {
  if (!iso) return "-";
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "-";
  const diff = now - t;
  const past = diff >= 0;
  const sec = Math.abs(diff) / 1000;
  if (sec < 60) return past ? "방금 전" : "곧";
  const suffix = past ? "전" : "후";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}분 ${suffix}`;
  const hour = Math.floor(min / 60);
  if (hour < 24) return `${hour}시간 ${suffix}`;
  return `${Math.floor(hour / 24)}일 ${suffix}`;
}

/** 만료까지 남은 일수 문구. */
export function daysLeftText(days: number | null | undefined): string {
  if (days === null || days === undefined) return "-";
  if (days < 0) return `${-days}일 지남`;
  if (days === 0) return "오늘 만료";
  return `${days}일 남음`;
}

/** 만료 시각까지 남은 일수(내림). 시각이 없으면 null. */
export function daysUntil(iso: string | null | undefined, now: number): number | null {
  if (!iso) return null;
  const t = Date.parse(iso);
  return Number.isNaN(t) ? null : Math.floor((t - now) / 86_400_000);
}
```

`web/src/lib/input.ts`:

```ts
/** 줄바꿈·쉼표·공백으로 나눈 목록. 빈 항목과 중복은 버리고 순서는 유지한다. */
export function splitList(text: string): string[] {
  const out: string[] = [];
  for (const item of text.split(/[\s,]+/)) {
    if (item !== "" && !out.includes(item)) out.push(item);
  }
  return out;
}

/** 1~65535인 정수만 포트로 받는다. */
export function parsePort(text: string): number | null {
  const t = text.trim();
  if (!/^\d{1,5}$/.test(t)) return null;
  const n = Number(t);
  return n >= 1 && n <= 65535 ? n : null;
}
```

`web/src/lib/errors.ts`:

```ts
import { ApiError } from "../api/client.ts";

/** 서버 오류 코드의 화면용 문구. 코드 목록은 docs/guide/api.md의 오류 표와 같다. */
const MESSAGES: Record<string, string> = {
  unauthorized: "관리자 로그인이 필요하거나 로그인이 만료됐습니다.",
  too_many_attempts: "시도가 너무 많습니다. 잠시 뒤 다시 시도하세요.",
  unsupported_media_type: "요청 형식이 올바르지 않습니다.",
  malformed_json: "요청 내용이 올바르지 않습니다.",
  method_not_allowed: "지원하지 않는 요청입니다.",
  invalid_status: "상태 값이 올바르지 않습니다.",
  invalid_limit: "목록 범위 값이 올바르지 않습니다.",
  invalid_before_id: "목록 범위 값이 올바르지 않습니다.",
  not_found: "찾을 수 없습니다. 이미 삭제됐을 수 있습니다.",
  db_error: "서버 데이터베이스 오류입니다. 잠시 뒤 다시 시도하세요.",
  invalid_domain: "도메인 형식이 올바르지 않습니다. 와일드카드 없이 a.com 형태로 입력하세요.",
  invalid_memo: "메모는 500자까지 쓸 수 있습니다.",
  domain_exists: "이미 등록된 도메인입니다.",
  invalid_target: "대상 값이 올바르지 않습니다. 호스트명, 포트(1~65535), SNI, IP(최대 16개)를 확인하세요.",
  target_exists: "같은 호스트명·포트·SNI의 대상이 이미 있습니다.",
  acme_enabled: "이미 ACME가 켜져 있습니다.",
  acme_disabled: "ACME가 꺼져 있습니다.",
  invalid_allowfrom: "허용 IP 형식이 올바르지 않습니다. IP나 CIDR을 최대 64개까지 입력하세요.",
  dns_provider_error: "DNS 공급자 호출에 실패했습니다. 서버의 DNS 설정과 /health를 확인하세요.",
  network_error: "서버에 연결할 수 없습니다.",
  unknown: "알 수 없는 오류입니다.",
};

export const KNOWN_ERROR_CODES: readonly string[] = Object.keys(MESSAGES);

/** 화면에 보여 줄 오류 문구. 모르는 코드는 코드를 그대로 붙여 원인을 찾을 수 있게 한다. */
export function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return MESSAGES[e.code] ?? `오류가 발생했습니다 (${e.code})`;
  return MESSAGES.unknown ?? "";
}
```

- [ ] **Step 5: 통과를 확인한다**

Run: `cd web && npm test && npx tsc --noEmit`
Expected: `ℹ tests 33`, `ℹ pass 33`, `ℹ fail 0`. `tsc`는 아무것도 출력하지 않는다.

- [ ] **Step 6: 커밋**

```bash
git add web
git commit -m "feat(web): add API client and display logic with node tests" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 5: 앱 뼈대, 로그인, 대시보드

**Files:**
- Create: `web/src/app/context.tsx`, `web/src/app/useResource.ts`, `web/src/app/useAction.ts`, `web/src/app/usePageTitle.ts`
- Create: `web/src/components/StatusBadge.tsx`, `Notice.tsx`, `Dialog.tsx`, `CopyButton.tsx`, `LoginDialog.tsx`, `Layout.tsx`
- Create: `web/src/dialogs/AddDomainDialog.tsx`
- Create: `web/src/pages/Dashboard.tsx`, `web/src/pages/NotFound.tsx`
- Create: `web/src/main.tsx`, `web/src/styles.css`

**Interfaces:**
- Consumes: Task 4의 `api`, `types`, `lib`.
- Produces:
  - `app/context.tsx`: `AppProvider`, `useApp(): {ready, admin, config, checkIntervalMs, codes: ReadonlyMap<string, CodeInfo>, login(key), logout(), markLoggedOut()}`.
  - `app/useResource.ts`: `REFRESH_MS`(60000), `useResource<T>(load, deps, refreshMs?) → {data, error, loading, reload}`(새로 고침이 실패해도 이전 데이터를 남기고, deps가 바뀌면 데이터를 버린다).
  - `app/useAction.ts`: `useAction() → {run(fn): Promise<T|undefined>, busy, error, clearError}`(실패하면 `undefined`, 401이면 `markLoggedOut()`).
  - `components/StatusBadge.tsx`: `StatusBadge({status})`, `ReasonTags({reasons})`, `ReasonList({reasons})`.
  - `components/Dialog.tsx`: `Dialog({title, onClose, children, wide?, persistent?})`(`persistent`면 Esc로 안 닫힌다).
  - `components/Notice.tsx`: `Notice({kind?, children})`. `components/CopyButton.tsx`: `CopyButton({text, label?})`.
  - 이후 태스크가 같은 `styles.css`를 쓴다(상세·감사 화면용 클래스도 여기서 모두 정의한다).

UI는 컴포넌트 테스트 없이 타입 검사·빌드로 확인한다(판단 로직은 Task 4에서 테스트했다). 사용자가 보는 동작은 Task 10에서 브라우저로 확인한다.

- [ ] **Step 1: 앱 상태와 훅을 만든다**

`web/src/app/context.tsx`:

```tsx
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { api } from "../api/endpoints.ts";
import type { CodeInfo, Config } from "../api/types.ts";
import { parseGoDuration } from "../lib/status.ts";

interface AppState {
  /** 로그인 여부와 설정을 처음 불러오는 일이 끝났는지. */
  ready: boolean;
  admin: boolean;
  config: Config | null;
  checkIntervalMs: number | null;
  /** 판정 코드 → 설명(/api/codes). 배지의 툴팁에 쓴다. */
  codes: ReadonlyMap<string, CodeInfo>;
  login: (key: string) => Promise<void>;
  logout: () => Promise<void>;
  /** 쓰기 요청이 401을 받았을 때(세션 만료) 화면을 조회 전용으로 되돌린다. */
  markLoggedOut: () => void;
}

const Ctx = createContext<AppState | null>(null);

export function AppProvider({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(false);
  const [admin, setAdmin] = useState(false);
  const [config, setConfig] = useState<Config | null>(null);
  const [codes, setCodes] = useState<ReadonlyMap<string, CodeInfo>>(new Map());

  useEffect(() => {
    let live = true;
    void Promise.allSettled([api.session(), api.config(), api.codes()]).then(([s, c, k]) => {
      if (!live) return;
      if (s.status === "fulfilled") setAdmin(s.value);
      if (c.status === "fulfilled") setConfig(c.value);
      if (k.status === "fulfilled") setCodes(new Map(k.value.map((x): [string, CodeInfo] => [x.code, x])));
      setReady(true);
    });
    return () => {
      live = false;
    };
  }, []);

  const login = useCallback(async (key: string) => {
    await api.login(key);
    setAdmin(true);
  }, []);
  const logout = useCallback(async () => {
    try {
      await api.logout();
    } finally {
      setAdmin(false);
    }
  }, []);
  const markLoggedOut = useCallback(() => setAdmin(false), []);

  const value = useMemo<AppState>(
    () => ({
      ready,
      admin,
      config,
      checkIntervalMs: config ? parseGoDuration(config.check_interval) : null,
      codes,
      login,
      logout,
      markLoggedOut,
    }),
    [ready, admin, config, codes, login, logout, markLoggedOut],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useApp(): AppState {
  const v = useContext(Ctx);
  if (!v) throw new Error("useApp must be used inside AppProvider");
  return v;
}
```

`web/src/app/useResource.ts`:

```ts
import { useCallback, useEffect, useRef, useState } from "react";
import { errorMessage } from "../lib/errors.ts";

/** 화면을 열 때 한 번, 그 뒤 refreshMs마다(0이면 안 함) 데이터를 불러온다. 서버 부하를 줄이려고 60초를 쓴다. */
export const REFRESH_MS = 60_000;

/**
 * 읽기 데이터를 불러오는 훅. 새로 고침이 실패해도 이전 데이터는 남기고 error만 채운다.
 * deps가 바뀌면(다른 도메인으로 이동 등) 이전 데이터를 버리고 다시 불러온다.
 * 늦게 도착한 옛 요청의 응답은 무시한다.
 */
export function useResource<T>(load: () => Promise<T>, deps: readonly unknown[], refreshMs = 0) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const seq = useRef(0);
  const loadRef = useRef(load);
  loadRef.current = load;

  const reload = useCallback(async () => {
    const mine = ++seq.current;
    try {
      const next = await loadRef.current();
      if (mine !== seq.current) return;
      setData(next);
      setError(null);
    } catch (e) {
      if (mine !== seq.current) return;
      setError(errorMessage(e));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    setData(null);
    setLoading(true);
    void reload();
    if (refreshMs <= 0) return;
    const id = setInterval(() => void reload(), refreshMs);
    return () => clearInterval(id);
  }, [reload, refreshMs, ...deps]);

  return { data, error, loading, reload };
}
```

`web/src/app/useAction.ts`:

```ts
import { useCallback, useState } from "react";
import { ApiError } from "../api/client.ts";
import { errorMessage } from "../lib/errors.ts";
import { useApp } from "./context.tsx";

/**
 * 쓰기 요청을 실행하고 진행 중 여부와 오류 문구를 관리한다.
 * 실패하면 undefined를 돌려준다. 401이면 세션이 만료된 것이므로 화면을 조회 전용으로 되돌린다.
 */
export function useAction() {
  const { markLoggedOut } = useApp();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true);
      setError(null);
      try {
        return await fn();
      } catch (e) {
        if (e instanceof ApiError && e.status === 401) markLoggedOut();
        setError(errorMessage(e));
        return undefined;
      } finally {
        setBusy(false);
      }
    },
    [markLoggedOut],
  );
  const clearError = useCallback(() => setError(null), []);
  return { run, busy, error, clearError };
}
```

`web/src/app/usePageTitle.ts`:

```ts
import { useEffect } from "react";

export function usePageTitle(title: string) {
  useEffect(() => {
    document.title = `${title} · ssl-checker`;
  }, [title]);
}
```

- [ ] **Step 2: 공용 컴포넌트를 만든다**

`web/src/components/StatusBadge.tsx`:

```tsx
import type { Status } from "../api/types.ts";
import { useApp } from "../app/context.tsx";
import { STATUS_LABEL } from "../lib/status.ts";

/** 색만으로 구분하지 않도록 글자를 함께 쓴다. */
export function StatusBadge({ status }: { status: Status }) {
  return <span className={`badge badge-${status}`}>{STATUS_LABEL[status]}</span>;
}

/** 판정 코드 태그. 마우스를 올리면 서버가 알려 준 설명이 보인다. */
export function ReasonTags({ reasons }: { reasons: readonly string[] }) {
  const { codes } = useApp();
  if (reasons.length === 0) return null;
  return (
    <span className="reasons">
      {reasons.map((code) => (
        <span key={code} className={`tag tag-${codes.get(code)?.status ?? "unknown"}`} title={codes.get(code)?.meaning}>
          {code}
        </span>
      ))}
    </span>
  );
}

/** 판정 코드와 그 뜻을 줄마다 보여 준다(도메인 상세 상단). */
export function ReasonList({ reasons }: { reasons: readonly string[] }) {
  const { codes } = useApp();
  if (reasons.length === 0) return null;
  return (
    <ul className="reason-list">
      {reasons.map((code) => (
        <li key={code}>
          <span className={`tag tag-${codes.get(code)?.status ?? "unknown"}`}>{code}</span>
          <span>{codes.get(code)?.meaning ?? ""}</span>
        </li>
      ))}
    </ul>
  );
}
```

`web/src/components/Notice.tsx`:

```tsx
import type { ReactNode } from "react";

export function Notice({ kind = "info", children }: { kind?: "info" | "error" | "warning"; children: ReactNode }) {
  return (
    <div className={`notice notice-${kind}`} role={kind === "error" ? "alert" : "status"}>
      {children}
    </div>
  );
}
```

`web/src/components/Dialog.tsx`:

```tsx
import { useEffect, useRef, type ReactNode } from "react";

interface Props {
  title: string;
  /** Esc로 닫혔을 때 부른다. 부모가 창을 렌더링하지 않으면 닫힌다. */
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
  /** true면 Esc로 닫히지 않는다. 한 번만 보여 주는 정보를 담은 창에 쓴다. */
  persistent?: boolean;
}

/** 브라우저 <dialog>를 모달로 연다. 포커스 가둠과 Esc 처리는 브라우저가 한다. */
export function Dialog({ title, onClose, children, wide, persistent }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      className={wide ? "dialog wide" : "dialog"}
      aria-labelledby="dialog-title"
      onClose={onClose}
      onCancel={persistent ? (e) => e.preventDefault() : undefined}
    >
      <h2 id="dialog-title">{title}</h2>
      {children}
    </dialog>
  );
}
```

`web/src/components/CopyButton.tsx`:

```tsx
import { useState } from "react";

/** 클립보드는 https나 localhost에서만 된다. 안 되면 "복사 실패"를 보여 직접 선택해 복사하게 한다. */
export function CopyButton({ text, label = "복사" }: { text: string; label?: string }) {
  const [state, setState] = useState<"idle" | "done" | "fail">("idle");
  async function copy() {
    try {
      await navigator.clipboard.writeText(text);
      setState("done");
    } catch {
      setState("fail");
    }
    setTimeout(() => setState("idle"), 2000);
  }
  return (
    <button type="button" className="btn small" onClick={() => void copy()}>
      {state === "done" ? "복사됨" : state === "fail" ? "복사 실패" : label}
    </button>
  );
}
```

`web/src/components/LoginDialog.tsx`:

```tsx
import { useState, type FormEvent } from "react";
import { ApiError } from "../api/client.ts";
import { useApp } from "../app/context.tsx";
import { errorMessage } from "../lib/errors.ts";
import { Dialog } from "./Dialog.tsx";
import { Notice } from "./Notice.tsx";

export function LoginDialog({ onClose }: { onClose: () => void }) {
  const { login } = useApp();
  const [key, setKey] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await login(key);
      onClose();
    } catch (err) {
      // 로그인 화면의 401은 "세션 만료"가 아니라 "키가 틀림"이다.
      setError(err instanceof ApiError && err.status === 401 ? "관리자 키가 맞지 않습니다." : errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog title="관리자 로그인" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>관리자 키 (ADMIN_KEY)</span>
          <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" autoFocus required />
        </label>
        <p className="muted small-text">로그인하면 12시간 동안 유지됩니다. 공용 PC에서는 사용 뒤 로그아웃하세요.</p>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || key === ""}>로그인</button>
        </div>
      </form>
    </Dialog>
  );
}
```

`web/src/components/Layout.tsx`:

```tsx
import { useState } from "react";
import { Link, NavLink, Outlet } from "react-router-dom";
import { useApp } from "../app/context.tsx";
import { LoginDialog } from "./LoginDialog.tsx";

export function Layout() {
  const { ready, admin, logout } = useApp();
  const [loginOpen, setLoginOpen] = useState(false);
  return (
    <>
      <header className="topbar">
        <div className="topbar-inner">
          <Link to="/" className="brand">ssl-checker</Link>
          <nav aria-label="주 메뉴">
            <NavLink to="/" end>대시보드</NavLink>
            {admin && <NavLink to="/audit">감사 로그</NavLink>}
          </nav>
          <div className="spacer" />
          {ready &&
            (admin ? (
              <button type="button" className="btn" onClick={() => void logout()}>로그아웃</button>
            ) : (
              <button type="button" className="btn primary" onClick={() => setLoginOpen(true)}>관리자 로그인</button>
            ))}
        </div>
      </header>
      <main className="page">
        <Outlet />
      </main>
      {loginOpen && <LoginDialog onClose={() => setLoginOpen(false)} />}
    </>
  );
}
```

- [ ] **Step 3: 도메인 추가 창과 두 페이지를 만든다**

`web/src/dialogs/AddDomainDialog.tsx`:

```tsx
import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

export function AddDomainDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (id: number) => void }) {
  const [fqdn, setFqdn] = useState("");
  const [memo, setMemo] = useState("");
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const d = await run(() => api.createDomain(fqdn.trim(), memo));
    if (d) onCreated(d.id);
  }

  return (
    <Dialog title="도메인 추가" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>도메인</span>
          <input value={fqdn} onChange={(e) => setFqdn(e.target.value)} placeholder="a.com" autoFocus required />
          <small>와일드카드 없이 입력합니다. 등록해도 ACME는 꺼져 있고, 상세 화면에서 켤 수 있습니다.</small>
        </label>
        <label className="field">
          <span>메모 (선택)</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || fqdn.trim() === ""}>추가</button>
        </div>
      </form>
    </Dialog>
  );
}
```

`web/src/pages/Dashboard.tsx`:

```tsx
import { useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { REFRESH_MS, useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { ReasonTags, StatusBadge } from "../components/StatusBadge.tsx";
import { AddDomainDialog } from "../dialogs/AddDomainDialog.tsx";
import { daysLeftText, formatDate, formatDateTime, relativeTime } from "../lib/format.ts";
import { CNAME_LABEL, isStale, parseStatus, sortByUrgency, STATUS_LABEL, STATUS_ORDER } from "../lib/status.ts";

export function Dashboard() {
  usePageTitle("대시보드");
  const { admin, checkIntervalMs } = useApp();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const filter = parseStatus(params.get("status"));
  const [adding, setAdding] = useState(false);

  const { data, error, loading } = useResource(
    async () => {
      const [summary, domains] = await Promise.all([api.summary(), api.domains()]);
      return { summary, domains };
    },
    [],
    REFRESH_MS,
  );

  function toggle(status: (typeof STATUS_ORDER)[number]) {
    setParams(filter === status ? {} : { status });
  }

  const now = Date.now();
  const rows = sortByUrgency((data?.domains ?? []).filter((d) => filter === null || d.status === filter));
  const anyStale = (data?.domains ?? []).some((d) => isStale(d.last_checked_at, checkIntervalMs, now));

  return (
    <>
      <div className="page-head">
        <h1>도메인 {data ? `${data.summary.domains}개` : ""}</h1>
        {admin && (
          <button type="button" className="btn primary" onClick={() => setAdding(true)}>도메인 추가</button>
        )}
      </div>

      {error && <Notice kind="error">{data ? `새로 고치지 못했습니다: ${error}` : error}</Notice>}
      {anyStale && (
        <Notice kind="warning">
          일부 점검 결과가 오래됐습니다(점검 주기의 2배 넘게 지남). 상태는 점검할 때 저장되므로, worker가 멈췄다면 아래 "정상"도 최신이 아닐 수 있습니다.
        </Notice>
      )}

      {data && (
        <div className="summary" role="group" aria-label="상태별 도메인 수">
          {STATUS_ORDER.map((s) => (
            <button
              key={s}
              type="button"
              className={`summary-card summary-${s}`}
              aria-pressed={filter === s}
              onClick={() => toggle(s)}
            >
              <span className="summary-count">{data.summary[s]}</span>
              <span className="summary-label">{STATUS_LABEL[s]}</span>
            </button>
          ))}
        </div>
      )}

      {loading && !data && <p className="muted">불러오는 중…</p>}

      {data && rows.length === 0 && (
        <div className="empty">
          {filter ? (
            <p>"{STATUS_LABEL[filter]}" 상태인 도메인이 없습니다.</p>
          ) : (
            <p>
              등록된 도메인이 없습니다.
              {admin ? " 위의 \"도메인 추가\"로 시작하세요." : " 관리자로 로그인하면 도메인을 추가할 수 있습니다."}
            </p>
          )}
        </div>
      )}

      {rows.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>상태</th>
                <th>도메인</th>
                <th>가장 이른 만료</th>
                <th>대상</th>
                <th>ACME</th>
                <th>마지막 점검</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((d) => (
                <tr key={d.id}>
                  <td>
                    <StatusBadge status={d.status} />
                    <ReasonTags reasons={d.reasons} />
                  </td>
                  <td>
                    <Link to={`/domains/${d.id}`}>{d.fqdn}</Link>
                    {d.memo && <div className="muted small-text">{d.memo}</div>}
                  </td>
                  <td>
                    {formatDate(d.earliest_not_after)}
                    {d.days_left !== null && <div className="muted small-text">{daysLeftText(d.days_left)}</div>}
                  </td>
                  <td className="num">{d.targets}</td>
                  <td>
                    {d.acme.enabled ? (
                      <span>
                        켜짐 <span className="muted small-text">{d.acme.cname_status ? CNAME_LABEL[d.acme.cname_status] : ""}</span>
                      </span>
                    ) : (
                      <span className="muted">꺼짐</span>
                    )}
                  </td>
                  <td className="nowrap" title={formatDateTime(d.last_checked_at)}>
                    {relativeTime(d.last_checked_at, now)}
                    {isStale(d.last_checked_at, checkIntervalMs, now) && (
                      <span className="tag tag-warning" title="점검이 멈춘 것 같습니다">오래됨</span>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {adding && <AddDomainDialog onClose={() => setAdding(false)} onCreated={(id) => navigate(`/domains/${id}`)} />}
    </>
  );
}
```

`web/src/pages/NotFound.tsx`:

```tsx
import { Link } from "react-router-dom";
import { usePageTitle } from "../app/usePageTitle.ts";

export function NotFound() {
  usePageTitle("찾을 수 없음");
  return (
    <div className="empty">
      <h1>페이지를 찾을 수 없습니다</h1>
      <p><Link to="/">대시보드로 가기</Link></p>
    </div>
  );
}
```

- [ ] **Step 4: 진입점과 스타일을 만든다**

`web/src/main.tsx` (도메인 상세·감사 로그 경로는 Task 6·7에서 더한다):

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AppProvider } from "./app/context.tsx";
import { Layout } from "./components/Layout.tsx";
import { Dashboard } from "./pages/Dashboard.tsx";
import { NotFound } from "./pages/NotFound.tsx";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <AppProvider>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </AppProvider>
    </BrowserRouter>
  </StrictMode>,
);
```

`web/src/styles.css`:

```css
/* 색은 모두 토큰이다. 라이트가 기본이고, 시스템이 다크면 아래 미디어 쿼리가 덮어쓴다. */
:root {
  --bg: #f5f6f8;
  --surface: #ffffff;
  --text: #1a1f26;
  --muted: #5b6573;
  --border: #dde1e7;
  --accent: #2456d3;
  --accent-contrast: #ffffff;
  --focus: #2456d3;

  --critical: #b3261e;
  --critical-bg: #fde8e7;
  --warning: #99560a;
  --warning-bg: #fdefd9;
  --info: #1b5bb8;
  --info-bg: #e3edfd;
  --ok: #17703a;
  --ok-bg: #def3e5;
  --unknown: #566070;
  --unknown-bg: #e9ecf1;

  --radius: 8px;
  --font: system-ui, -apple-system, "Segoe UI", "Apple SD Gothic Neo", "Noto Sans KR", sans-serif;
  --mono: ui-monospace, "SF Mono", Menlo, Consolas, monospace;
  color-scheme: light dark;
}

@media (prefers-color-scheme: dark) {
  :root {
    --bg: #0f1318;
    --surface: #181d24;
    --text: #e6eaf0;
    --muted: #97a2b1;
    --border: #2b323c;
    --accent: #7ba2ff;
    --accent-contrast: #0b1226;
    --focus: #9db9ff;

    --critical: #ff9a92;
    --critical-bg: #3b1d1b;
    --warning: #f5b86a;
    --warning-bg: #38280f;
    --info: #8fb6ff;
    --info-bg: #17284a;
    --ok: #7fd49c;
    --ok-bg: #143122;
    --unknown: #a8b2c0;
    --unknown-bg: #262d37;
  }
}

* { box-sizing: border-box; }
html { -webkit-text-size-adjust: 100%; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--text);
  font: 15px/1.5 var(--font);
}
a { color: var(--accent); }
code, pre { font-family: var(--mono); font-size: 0.9em; }
h1 { font-size: 1.5rem; margin: 0; line-height: 1.3; }
h2 { font-size: 1.1rem; margin: 0; }
h3 { font-size: 1rem; margin: 0 0 0.15rem; }
:focus-visible { outline: 2px solid var(--focus); outline-offset: 2px; }

.muted { color: var(--muted); }
.small-text { font-size: 0.85rem; }
.error-text { color: var(--critical); margin-left: 0.5rem; }
.warning-text { color: var(--warning); }
.num { font-variant-numeric: tabular-nums; text-align: right; }
.nowrap { white-space: nowrap; }

/* 상단 바와 페이지 */
.topbar { background: var(--surface); border-bottom: 1px solid var(--border); }
.topbar-inner { display: flex; align-items: center; flex-wrap: wrap; gap: 0.4rem 1.25rem; max-width: 1100px; margin: 0 auto; padding: 0.6rem 16px; }
.brand, .topbar nav a, .btn { white-space: nowrap; }
.brand { font-weight: 700; font-size: 1.05rem; color: var(--text); text-decoration: none; }
.topbar nav { display: flex; gap: 1rem; }
.topbar nav a { color: var(--muted); text-decoration: none; padding: 0.2rem 0; border-bottom: 2px solid transparent; }
.topbar nav a.active { color: var(--text); border-bottom-color: var(--accent); }
.spacer { flex: 1; }
.page { max-width: 1100px; margin: 0 auto; padding: 1.25rem 16px 3rem; }
.page-head, .section-head, .target-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; flex-wrap: wrap; }
.page-head { margin-bottom: 1rem; }
.section-head { margin: 1.5rem 0 0.75rem; align-items: center; }
.crumb { margin: 0 0 0.5rem; }

/* 버튼 */
.btn {
  font: inherit; cursor: pointer; padding: 0.45rem 0.9rem; border-radius: 6px;
  border: 1px solid var(--border); background: var(--surface); color: var(--text);
}
.btn:hover:not(:disabled) { border-color: var(--muted); }
.btn:disabled { opacity: 0.55; cursor: default; }
.btn.primary { background: var(--accent); border-color: var(--accent); color: var(--accent-contrast); }
.btn.danger { background: var(--critical); border-color: var(--critical); color: var(--surface); }
.btn.danger-text { color: var(--critical); }
.btn.small { padding: 0.25rem 0.6rem; font-size: 0.85rem; }
.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; align-items: center; }
.actions.center { justify-content: center; margin-top: 1rem; }

/* 상태 배지: 색과 글자를 함께 쓴다 */
.badge, .tag { display: inline-block; border-radius: 999px; font-size: 0.78rem; font-weight: 600; padding: 0.1rem 0.6rem; vertical-align: middle; }
.tag { font-weight: 500; font-family: var(--mono); font-size: 0.72rem; padding: 0.05rem 0.5rem; margin: 0.15rem 0.3rem 0 0; }
.badge-critical, .tag-critical { color: var(--critical); background: var(--critical-bg); }
.badge-warning, .tag-warning { color: var(--warning); background: var(--warning-bg); }
.badge-info, .tag-info { color: var(--info); background: var(--info-bg); }
.badge-ok, .tag-ok { color: var(--ok); background: var(--ok-bg); }
.badge-unknown, .tag-unknown { color: var(--unknown); background: var(--unknown-bg); }
.reasons { display: block; }
.reason-list { list-style: none; padding: 0; margin: 0 0 0.5rem; display: grid; gap: 0.35rem; }
.reason-list li { display: flex; gap: 0.6rem; align-items: baseline; flex-wrap: wrap; }

/* 요약 카드 */
.summary { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 0.6rem; margin-bottom: 1.25rem; }
.summary-card {
  font: inherit; cursor: pointer; text-align: left; padding: 0.7rem 0.9rem; border-radius: var(--radius);
  border: 1px solid var(--border); background: var(--surface); color: var(--text);
  display: flex; flex-direction: column; gap: 0.1rem; border-left-width: 5px;
}
.summary-card[aria-pressed="true"] { outline: 2px solid var(--accent); outline-offset: -2px; }
.summary-count { font-size: 1.6rem; font-weight: 700; font-variant-numeric: tabular-nums; line-height: 1.2; }
.summary-label { font-size: 0.85rem; color: var(--muted); }
.summary-critical { border-left-color: var(--critical); }
.summary-warning { border-left-color: var(--warning); }
.summary-info { border-left-color: var(--info); }
.summary-ok { border-left-color: var(--ok); }
.summary-unknown { border-left-color: var(--unknown); }

/* 표와 카드 */
.card { background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); padding: 1rem 1.1rem; margin-bottom: 1rem; }
.target-head { margin-bottom: 0.6rem; }
.table-wrap { overflow-x: auto; background: var(--surface); border: 1px solid var(--border); border-radius: var(--radius); }
.card .table-wrap { border: 0; border-radius: 0; background: transparent; }
.table { border-collapse: collapse; width: 100%; font-size: 0.9rem; }
.table th { text-align: left; font-weight: 600; color: var(--muted); font-size: 0.8rem; white-space: nowrap; }
.table th, .table td { padding: 0.55rem 0.75rem; border-bottom: 1px solid var(--border); vertical-align: top; }
.table tbody tr:last-child td { border-bottom: 0; }
.table tr.detail-row td { padding-top: 0; border-top: 0; }
.empty { text-align: center; color: var(--muted); padding: 2rem 1rem; background: var(--surface); border: 1px dashed var(--border); border-radius: var(--radius); }
.kv { display: grid; grid-template-columns: 11rem 1fr; gap: 0.5rem 1rem; margin: 0; }
.kv dt { color: var(--muted); }
.kv dd { margin: 0; overflow-wrap: anywhere; }

/* 알림 */
.notice { padding: 0.65rem 0.9rem; border-radius: 6px; margin: 0 0 1rem; border: 1px solid transparent; }
.notice-error { color: var(--critical); background: var(--critical-bg); }
.notice-warning { color: var(--warning); background: var(--warning-bg); }
.notice-info { color: var(--info); background: var(--info-bg); }

/* 대화상자와 폼 */
.dialog { width: min(32rem, calc(100vw - 2rem)); max-height: calc(100vh - 2rem); overflow: auto; border: 1px solid var(--border); border-radius: 10px; padding: 1.25rem; background: var(--surface); color: var(--text); }
.dialog.wide { width: min(46rem, calc(100vw - 2rem)); }
.dialog::backdrop { background: rgba(0, 0, 0, 0.5); }
.dialog h2 { margin-bottom: 0.9rem; }
.dialog .actions { justify-content: flex-end; margin-top: 1rem; }
.dialog-body p { margin: 0 0 0.6rem; }
.dialog-section { margin: 1rem 0; }
.dialog-section h3 { margin-bottom: 0.4rem; }
.field { display: grid; gap: 0.25rem; margin-bottom: 0.85rem; }
.field > span { font-weight: 600; font-size: 0.9rem; }
.field small { color: var(--muted); }
input, textarea {
  font: inherit; color: var(--text); background: var(--bg); width: 100%;
  border: 1px solid var(--border); border-radius: 6px; padding: 0.45rem 0.6rem;
}
textarea { font-family: var(--mono); font-size: 0.9rem; resize: vertical; }
.code { background: var(--bg); border: 1px solid var(--border); border-radius: 6px; padding: 0.7rem 0.8rem; overflow-x: auto; white-space: pre; margin: 0 0 0.5rem; }
.copy-row { display: flex; gap: 0.6rem; align-items: center; flex-wrap: wrap; }

@media (max-width: 720px) {
  .summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .kv { grid-template-columns: 1fr; gap: 0.1rem; }
  .kv dd { margin-bottom: 0.6rem; }
}
```

- [ ] **Step 5: 빌드를 확인한다**

Run: `cd web && npm test && npm run build`
Expected: 테스트 33개 통과, `tsc` 오류 없음, `vite build`가 `../internal/webui/dist/index.html`과 `assets/index-*.css`, `assets/index-*.js`를 만든다.
그다음 `git status --short`에 `internal/webui/dist` 아래 변경이 **나타나지 않아야** 한다(`.keep`만 추적되고 나머지는 무시된다. `.keep`이 삭제로 보이면 `web/public/.keep`이 없는 것이다).

Run: `go build ./... && go test ./internal/webui`
Expected: 성공(내장된 `dist`가 바뀌어도 webui 테스트는 통과한다).

- [ ] **Step 6: 커밋**

```bash
git add web
git commit -m "feat(web): add app shell, login and dashboard" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 6: 도메인 상세 화면과 관리 동작

**Files:**
- Create: `web/src/components/ResultsTable.tsx`, `TargetCard.tsx`, `AcmePanel.tsx`
- Create: `web/src/dialogs/MemoDialog.tsx`, `TargetDialog.tsx`, `AllowFromDialog.tsx`, `ConfirmDialog.tsx`, `CredentialsDialog.tsx`
- Create: `web/src/pages/DomainDetail.tsx`
- Modify: `web/src/main.tsx`

**Interfaces:**
- Consumes: Task 5의 `useApp`, `useResource`, `useAction`, `Dialog`, `Notice`, `CopyButton`, `StatusBadge`/`ReasonTags`/`ReasonList`, Task 4의 `api`·`lib`.
- Produces: `/domains/:id` 경로의 `DomainDetail`. 쓰기 동작은 모두 `useAction().run`을 거치고, 성공하면 `reload()`로 다시 불러온다.
  - 일회성 창(`CredentialsDialog`)은 `persistent`이고 `kind`가 `"enable"`이면 CNAME 등록 안내를, `"rotate"`이면 `--force` 경고를 보여 준다.

- [ ] **Step 1: 결과 표와 대상 카드, ACME 패널을 만든다**

`web/src/components/ResultsTable.tsx`:

```tsx
import { Fragment } from "react";
import type { CheckResult } from "../api/types.ts";
import { daysLeftText, daysUntil, formatDate, formatDateTime, relativeTime, shortSerial } from "../lib/format.ts";
import { ReasonTags, StatusBadge } from "./StatusBadge.tsx";

/** 대상의 접속 주소별 최신 점검 결과. */
export function ResultsTable({ results, now }: { results: readonly CheckResult[]; now: number }) {
  if (results.length === 0) return <p className="muted">아직 점검 결과가 없습니다.</p>;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>상태</th>
            <th>접속 주소</th>
            <th>만료일</th>
            <th>발급자</th>
            <th>serial</th>
            <th>TLS</th>
            <th>점검</th>
          </tr>
        </thead>
        <tbody>
          {results.map((r) => (
            <Fragment key={r.address}>
              <tr>
                <td><StatusBadge status={r.status} /></td>
                <td><code>{r.address}</code></td>
                <td>
                  {formatDate(r.not_after)}
                  {r.not_after && <div className="muted small-text">{daysLeftText(daysUntil(r.not_after, now))}</div>}
                </td>
                <td>{r.issuer || "-"}</td>
                <td><code title={r.serial}>{shortSerial(r.serial) || "-"}</code></td>
                <td>{r.tls_version || "-"}</td>
                <td className="nowrap" title={formatDateTime(r.checked_at)}>{relativeTime(r.checked_at, now)}</td>
              </tr>
              {(r.reasons.length > 0 || r.error) && (
                <tr className="detail-row">
                  <td colSpan={7}>
                    <ReasonTags reasons={r.reasons} />
                    {r.error && <span className="error-text">{r.error}</span>}
                  </td>
                </tr>
              )}
            </Fragment>
          ))}
        </tbody>
      </table>
    </div>
  );
}

/** 대상의 점검 이력(최신순). */
export function HistoryTable({ results }: { results: readonly CheckResult[] }) {
  if (results.length === 0) return <p className="muted">이력이 없습니다.</p>;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th>점검 시각</th>
            <th>접속 주소</th>
            <th>상태</th>
            <th>만료일</th>
            <th>serial</th>
            <th>사유·오류</th>
          </tr>
        </thead>
        <tbody>
          {results.map((r) => (
            <tr key={`${r.address}-${r.checked_at}`}>
              <td className="nowrap">{formatDateTime(r.checked_at)}</td>
              <td><code>{r.address}</code></td>
              <td><StatusBadge status={r.status} /></td>
              <td>{formatDate(r.not_after)}</td>
              <td><code title={r.serial}>{shortSerial(r.serial) || "-"}</code></td>
              <td>
                <ReasonTags reasons={r.reasons} />
                {r.error && <span className="error-text">{r.error}</span>}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
```

`web/src/components/TargetCard.tsx`:

```tsx
import { useState } from "react";
import { api } from "../api/endpoints.ts";
import type { Target } from "../api/types.ts";
import { useResource } from "../app/useResource.ts";
import { useAction } from "../app/useAction.ts";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { TargetDialog } from "../dialogs/TargetDialog.tsx";
import { Notice } from "./Notice.tsx";
import { HistoryTable, ResultsTable } from "./ResultsTable.tsx";
import { ReasonTags, StatusBadge } from "./StatusBadge.tsx";

interface Props {
  domainId: number;
  target: Target;
  admin: boolean;
  now: number;
  onChanged: () => void;
}

function TargetHistory({ targetId }: { targetId: number }) {
  const { data, error, loading } = useResource(() => api.targetResults(targetId, 50), [targetId]);
  if (loading && !data) return <p className="muted">불러오는 중…</p>;
  if (error && !data) return <Notice kind="error">{error}</Notice>;
  return <HistoryTable results={data ?? []} />;
}

export function TargetCard({ domainId, target, admin, now, onChanged }: Props) {
  const [dialog, setDialog] = useState<"edit" | "delete" | null>(null);
  const [history, setHistory] = useState(false);
  const check = useAction();

  async function checkNow() {
    const r = await check.run(() => api.checkTarget(target.id));
    if (r) onChanged();
  }

  const where =
    target.ips.length > 0 ? `IP ${target.ips.join(", ")}` : "IP 지정 없음 (호스트명으로 접속)";
  return (
    <section className="card target">
      <div className="target-head">
        <div>
          <h3>
            {target.hostname}:{target.port} <StatusBadge status={target.status} />
          </h3>
          <div className="muted small-text">
            {target.sni && `SNI ${target.sni} · `}
            {where}
            {target.memo && ` · ${target.memo}`}
          </div>
          <ReasonTags reasons={target.reasons} />
        </div>
        <div className="actions">
          <button type="button" className="btn small" aria-expanded={history} onClick={() => setHistory((h) => !h)}>
            이력
          </button>
          {admin && (
            <>
              <button type="button" className="btn small" disabled={check.busy} onClick={() => void checkNow()}>
                {check.busy ? "점검 중…" : "지금 점검"}
              </button>
              <button type="button" className="btn small" onClick={() => setDialog("edit")}>수정</button>
              <button type="button" className="btn small danger-text" onClick={() => setDialog("delete")}>삭제</button>
            </>
          )}
        </div>
      </div>
      {check.error && <Notice kind="error">{check.error}</Notice>}
      <ResultsTable results={target.results} now={now} />
      {history && <TargetHistory targetId={target.id} />}

      {dialog === "edit" && (
        <TargetDialog domainId={domainId} target={target} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title="대상 삭제"
          confirmLabel="삭제"
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.deleteTarget(target.id);
            onChanged();
          }}
        >
          <p><strong>{target.hostname}:{target.port}</strong> 대상과 점검 이력을 삭제합니다.</p>
        </ConfirmDialog>
      )}
    </section>
  );
}
```

`web/src/components/AcmePanel.tsx`:

```tsx
import { useState } from "react";
import { api } from "../api/endpoints.ts";
import type { Credentials, DomainDetail } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { AllowFromDialog } from "../dialogs/AllowFromDialog.tsx";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { CredentialsDialog } from "../dialogs/CredentialsDialog.tsx";
import { formatDateTime, relativeTime } from "../lib/format.ts";
import { CNAME_LABEL } from "../lib/status.ts";
import { CopyButton } from "./CopyButton.tsx";
import { Notice } from "./Notice.tsx";

interface Props {
  domain: DomainDetail;
  admin: boolean;
  now: number;
  onChanged: () => void;
}

type DialogKind = "enable" | "allow" | "rotate" | "disable";

function when(iso: string | null, now: number): string {
  return iso ? `${formatDateTime(iso)} (${relativeTime(iso, now)})` : "";
}

export function AcmePanel({ domain, admin, now, onChanged }: Props) {
  const acme = domain.acme;
  const [dialog, setDialog] = useState<DialogKind | null>(null);
  const [creds, setCreds] = useState<{ kind: "enable" | "rotate"; value: Credentials } | null>(null);
  const check = useAction();

  async function checkCname() {
    const r = await check.run(() => api.checkAcme(domain.id));
    if (r) onChanged();
  }

  return (
    <section className="card">
      <div className="target-head">
        <h2>ACME (DNS-01 위임)</h2>
        {admin && !acme.enabled && (
          <button type="button" className="btn primary small" onClick={() => setDialog("enable")}>ACME 켜기</button>
        )}
        {admin && acme.enabled && (
          <div className="actions">
            <button type="button" className="btn small" disabled={check.busy} onClick={() => void checkCname()}>
              {check.busy ? "확인 중…" : "CNAME 지금 확인"}
            </button>
            <button type="button" className="btn small" onClick={() => setDialog("allow")}>허용 IP 수정</button>
            <button type="button" className="btn small" onClick={() => setDialog("rotate")}>키 재발급</button>
            <button type="button" className="btn small danger-text" onClick={() => setDialog("disable")}>ACME 끄기</button>
          </div>
        )}
      </div>
      {check.error && <Notice kind="error">{check.error}</Notice>}

      {!acme.enabled ? (
        <p className="muted">
          ACME 위임이 꺼져 있습니다. 켜면 이 도메인의 인증서를 DNS API 없이 acme.sh로 발급·갱신할 수 있습니다.
        </p>
      ) : (
        <dl className="kv">
          <dt>CNAME 레코드</dt>
          <dd>
            <code>{acme.cname_record}</code> <CopyButton text={acme.cname_record} />
          </dd>
          <dt>CNAME 확인</dt>
          <dd>
            {CNAME_LABEL[acme.cname_status]}
            {acme.cname_checked_at && <span className="muted"> · {when(acme.cname_checked_at, now)}</span>}
            {acme.cname_status === "mismatch" && acme.cname_value && (
              <div className="small-text">
                현재 값: <code>{acme.cname_value}</code>. 위 레코드의 값으로 고쳐야 합니다.
              </div>
            )}
          </dd>
          <dt>마지막 TXT 갱신</dt>
          <dd>{acme.last_update_at ? when(acme.last_update_at, now) : <span className="muted">아직 없음</span>}</dd>
          <dt>마지막 갱신 실패</dt>
          <dd>
            {acme.last_failure_at ? when(acme.last_failure_at, now) : <span className="muted">없음</span>}
            {acme.consecutive_failures > 0 && <span className="error-text"> · 연속 {acme.consecutive_failures}회 실패</span>}
          </dd>
          <dt>허용 IP</dt>
          <dd>
            {acme.allowfrom.length > 0 ? acme.allowfrom.join(", ") : <span className="warning-text">제한 없음</span>}
          </dd>
          <dt>username</dt>
          <dd><code>{acme.username}</code></dd>
        </dl>
      )}

      {dialog === "enable" && (
        <AllowFromDialog
          title="ACME 켜기"
          submitLabel="켜기"
          initial={[]}
          onClose={() => setDialog(null)}
          onSubmit={async (allowfrom) => {
            const value = await api.enableAcme(domain.id, allowfrom);
            setCreds({ kind: "enable", value });
            onChanged();
          }}
        />
      )}
      {dialog === "allow" && acme.enabled && (
        <AllowFromDialog
          title="허용 IP 수정"
          submitLabel="저장"
          initial={acme.allowfrom}
          onClose={() => setDialog(null)}
          onSubmit={async (allowfrom) => {
            await api.patchAcme(domain.id, allowfrom);
            onChanged();
          }}
        />
      )}
      {dialog === "rotate" && (
        <ConfirmDialog
          title="키 재발급"
          confirmLabel="재발급"
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            const value = await api.rotateAcme(domain.id);
            setCreds({ kind: "rotate", value });
            onChanged();
          }}
        >
          <p>새 키를 만듭니다. <strong>옛 키는 즉시 무효</strong>가 되어, 서버의 acme.sh에 새 키를 설정하기 전까지 갱신이 실패합니다.</p>
          <p>서브도메인과 CNAME은 그대로라 DNS는 바꾸지 않아도 됩니다.</p>
        </ConfirmDialog>
      )}
      {dialog === "disable" && (
        <ConfirmDialog
          title="ACME 끄기"
          confirmLabel="끄기"
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.disableAcme(domain.id);
            onChanged();
          }}
        >
          <p>계정을 폐기하고 챌린지 TXT를 지웁니다. 서버의 acme.sh 갱신이 더 이상 되지 않습니다.</p>
          <p>다시 켜면 서브도메인이 새로 생겨 <strong>CNAME을 다시 걸어야</strong> 합니다.</p>
        </ConfirmDialog>
      )}
      {creds && <CredentialsDialog kind={creds.kind} creds={creds.value} onClose={() => setCreds(null)} />}
    </section>
  );
}
```

- [ ] **Step 2: 대화상자를 만든다**

`web/src/dialogs/MemoDialog.tsx`:

```tsx
import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

export function MemoDialog({ domainId, initial, onClose, onSaved }: { domainId: number; initial: string; onClose: () => void; onSaved: () => void }) {
  const [memo, setMemo] = useState(initial);
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const ok = await run(async () => {
      await api.patchDomain(domainId, memo);
      return true;
    });
    if (ok) {
      onSaved();
      onClose();
    }
  }

  return (
    <Dialog title="메모 수정" onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>메모</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} autoFocus />
        </label>
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy}>저장</button>
        </div>
      </form>
    </Dialog>
  );
}
```

`web/src/dialogs/TargetDialog.tsx`:

```tsx
import { useState, type FormEvent } from "react";
import { api } from "../api/endpoints.ts";
import type { Target } from "../api/types.ts";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { parsePort, splitList } from "../lib/input.ts";

interface Props {
  domainId: number;
  /** 있으면 수정, 없으면 추가. */
  target?: Target;
  onClose: () => void;
  onSaved: () => void;
}

export function TargetDialog({ domainId, target, onClose, onSaved }: Props) {
  const [hostname, setHostname] = useState(target?.hostname ?? "");
  const [port, setPort] = useState(String(target?.port ?? 443));
  const [sni, setSni] = useState(target?.sni ?? "");
  const [ips, setIps] = useState((target?.ips ?? []).join("\n"));
  const [memo, setMemo] = useState(target?.memo ?? "");
  const [localError, setLocalError] = useState<string | null>(null);
  const { run, busy, error } = useAction();

  async function submit(e: FormEvent) {
    e.preventDefault();
    const p = parsePort(port);
    if (p === null) {
      setLocalError("포트는 1~65535 사이의 정수여야 합니다.");
      return;
    }
    setLocalError(null);
    const input = { hostname: hostname.trim(), port: p, sni: sni.trim(), ips: splitList(ips), memo };
    const ok = await run(async () => {
      if (target) await api.patchTarget(target.id, input);
      else await api.createTarget(domainId, input);
      return true;
    });
    if (ok) {
      onSaved();
      onClose();
    }
  }

  return (
    <Dialog title={target ? "대상 수정" : "모니터링 대상 추가"} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>호스트명</span>
          <input value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="www.a.com" autoFocus required />
        </label>
        <label className="field">
          <span>포트</span>
          <input value={port} onChange={(e) => setPort(e.target.value)} inputMode="numeric" required />
        </label>
        <label className="field">
          <span>SNI (선택)</span>
          <input value={sni} onChange={(e) => setSni(e.target.value)} placeholder="비우면 호스트명" />
        </label>
        <label className="field">
          <span>서버 IP (선택, 한 줄에 하나)</span>
          <textarea value={ips} onChange={(e) => setIps(e.target.value)} rows={3} placeholder={"10.0.1.10\n10.0.1.11"} />
          <small>
            비우면 호스트명으로 한 번만 접속합니다(DNS가 여러 주소를 줘도 하나만 점검). 서버마다 같은 인증서가 깔렸는지 보려면 IP를 모두 적습니다(최대 16개).
          </small>
        </label>
        <label className="field">
          <span>메모 (선택)</span>
          <input value={memo} onChange={(e) => setMemo(e.target.value)} maxLength={500} />
        </label>
        <p className="muted small-text">저장하면 바로 한 번 점검합니다. 접속 설정을 바꾸면 옛 주소의 결과는 더 이상 보이지 않습니다.</p>
        {(localError ?? error) && <Notice kind="error">{localError ?? error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy || hostname.trim() === ""}>
            {busy ? "점검 중…" : "저장"}
          </button>
        </div>
      </form>
    </Dialog>
  );
}
```

`web/src/dialogs/AllowFromDialog.tsx`:

```tsx
import { useState, type FormEvent, type ReactNode } from "react";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";
import { splitList } from "../lib/input.ts";

interface Props {
  title: string;
  submitLabel: string;
  initial: readonly string[];
  hint?: ReactNode;
  /** 실패하면 던진다. 성공하면 창이 닫힌다. */
  onSubmit: (allowfrom: string[]) => Promise<unknown>;
  onClose: () => void;
}

/** ACME를 켤 때와 허용 IP를 고칠 때 함께 쓴다. /update를 부를 수 있는 서버의 IP·CIDR 목록을 받는다. */
export function AllowFromDialog({ title, submitLabel, initial, hint, onSubmit, onClose }: Props) {
  const [text, setText] = useState(initial.join("\n"));
  const { run, busy, error } = useAction();
  const list = splitList(text);

  async function submit(e: FormEvent) {
    e.preventDefault();
    const ok = await run(async () => {
      await onSubmit(list);
      return true;
    });
    if (ok) onClose();
  }

  return (
    <Dialog title={title} onClose={onClose}>
      <form onSubmit={(e) => void submit(e)}>
        <label className="field">
          <span>허용 IP (한 줄에 하나, IP 또는 CIDR)</span>
          <textarea value={text} onChange={(e) => setText(e.target.value)} rows={4} placeholder={"203.0.113.10/32\n10.0.1.0/24"} autoFocus />
          <small>/update를 부를 수 있는 서버입니다. 최대 64개.</small>
        </label>
        {hint}
        {list.length === 0 && <Notice kind="warning">목록이 비어 있으면 키를 아는 누구나 어디서든 /update를 부를 수 있습니다. 가능하면 서버 IP를 지정하세요.</Notice>}
        {error && <Notice kind="error">{error}</Notice>}
        <div className="actions">
          <button type="button" className="btn" onClick={onClose}>취소</button>
          <button type="submit" className="btn primary" disabled={busy}>{submitLabel}</button>
        </div>
      </form>
    </Dialog>
  );
}
```

`web/src/dialogs/ConfirmDialog.tsx`:

```tsx
import type { ReactNode } from "react";
import { useAction } from "../app/useAction.ts";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

interface Props {
  title: string;
  children: ReactNode;
  confirmLabel: string;
  /** 되돌릴 수 없는 작업이면 확인 버튼을 빨갛게 한다. */
  danger?: boolean;
  /** 실패하면 던진다. 성공하면 창이 닫힌다. */
  onConfirm: () => Promise<unknown>;
  onClose: () => void;
}

export function ConfirmDialog({ title, children, confirmLabel, danger, onConfirm, onClose }: Props) {
  const { run, busy, error } = useAction();

  async function confirm() {
    const ok = await run(async () => {
      await onConfirm();
      return true;
    });
    if (ok) onClose();
  }

  return (
    <Dialog title={title} onClose={onClose}>
      <div className="dialog-body">{children}</div>
      {error && <Notice kind="error">{error}</Notice>}
      <div className="actions">
        <button type="button" className="btn" onClick={onClose}>취소</button>
        <button type="button" className={danger ? "btn danger" : "btn primary"} disabled={busy} onClick={() => void confirm()}>
          {confirmLabel}
        </button>
      </div>
    </Dialog>
  );
}
```

`web/src/dialogs/CredentialsDialog.tsx`:

```tsx
import type { Credentials } from "../api/types.ts";
import { CopyButton } from "../components/CopyButton.tsx";
import { Dialog } from "../components/Dialog.tsx";
import { Notice } from "../components/Notice.tsx";

interface Props {
  kind: "enable" | "rotate";
  creds: Credentials;
  onClose: () => void;
}

/**
 * ACME를 켜거나 키를 재발급한 직후 한 번만 보여 주는 창이다. 비밀번호는 이 응답에만 있어서
 * Esc로 실수로 닫히지 않게 하고, 버튼으로만 닫는다.
 */
export function CredentialsDialog({ kind, creds, onClose }: Props) {
  return (
    <Dialog title={kind === "enable" ? "ACME를 켰습니다" : "키를 재발급했습니다"} onClose={onClose} wide persistent>
      <Notice kind="warning">
        <strong>이 창을 닫으면 비밀번호를 다시 볼 수 없습니다.</strong> 지금 서버에 설정하세요. 잃어버리면 키를 재발급해야 합니다.
      </Notice>

      {kind === "enable" ? (
        <section className="dialog-section">
          <h3>1. CNAME 레코드 등록</h3>
          <p>도메인의 DNS에 아래 한 줄을 등록합니다. 한 번만 하면 됩니다.</p>
          <div className="copy-row">
            <code>{creds.cname_record}</code>
            <CopyButton text={creds.cname_record} />
          </div>
        </section>
      ) : (
        <section className="dialog-section">
          <h3>CNAME은 그대로입니다</h3>
          <p>서브도메인이 같아서 DNS는 바꾸지 않아도 됩니다.</p>
        </section>
      )}

      <section className="dialog-section">
        <h3>{kind === "enable" ? "2. " : ""}자격 증명</h3>
        <dl className="kv">
          <dt>username</dt>
          <dd><code>{creds.username}</code> <CopyButton text={creds.username} /></dd>
          <dt>password</dt>
          <dd><code>{creds.password}</code> <CopyButton text={creds.password} /></dd>
          <dt>subdomain</dt>
          <dd><code>{creds.subdomain}</code></dd>
          <dt>허용 IP</dt>
          <dd>{creds.allowfrom.length > 0 ? creds.allowfrom.join(", ") : "제한 없음"}</dd>
        </dl>
      </section>

      <section className="dialog-section">
        <h3>{kind === "enable" ? "3. " : ""}인증서를 받을 서버에서 실행</h3>
        {kind === "rotate" && (
          <Notice kind="warning">
            새 키를 acme.sh 설정에 저장하려면 아래 <code>--issue ... --force</code>를 서버에서 다시 실행해야 합니다. 그렇지 않으면 다음 자동 갱신이 401로 실패합니다.
          </Notice>
        )}
        <pre className="code">{creds.acme_sh}</pre>
        <CopyButton text={creds.acme_sh} label="명령 복사" />
        <p className="muted small-text">
          비밀번호가 셸 기록에 남지 않게 하려면 <code>export ACMEDNS_PASSWORD=...</code> 줄 대신{" "}
          <code>read -rs ACMEDNS_PASSWORD &amp;&amp; export ACMEDNS_PASSWORD</code>를 실행하고 비밀번호를 붙여 넣으세요.
        </p>
      </section>

      <div className="actions">
        <button type="button" className="btn primary" onClick={onClose}>비밀번호를 저장했습니다. 닫기</button>
      </div>
    </Dialog>
  );
}
```

- [ ] **Step 3: 상세 페이지를 만들고 경로를 연결한다**

`web/src/pages/DomainDetail.tsx`:

```tsx
import { useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { api } from "../api/endpoints.ts";
import { useApp } from "../app/context.tsx";
import { usePageTitle } from "../app/usePageTitle.ts";
import { REFRESH_MS, useResource } from "../app/useResource.ts";
import { AcmePanel } from "../components/AcmePanel.tsx";
import { Notice } from "../components/Notice.tsx";
import { ReasonList, StatusBadge } from "../components/StatusBadge.tsx";
import { TargetCard } from "../components/TargetCard.tsx";
import { ConfirmDialog } from "../dialogs/ConfirmDialog.tsx";
import { MemoDialog } from "../dialogs/MemoDialog.tsx";
import { TargetDialog } from "../dialogs/TargetDialog.tsx";
import { NotFound } from "./NotFound.tsx";

export function DomainDetail() {
  const params = useParams();
  const id = Number(params.id);
  const valid = Number.isInteger(id) && id > 0;
  const { admin } = useApp();
  const navigate = useNavigate();
  const [dialog, setDialog] = useState<"memo" | "target" | "delete" | null>(null);

  const { data, error, loading, reload } = useResource(() => api.domain(id), [id], REFRESH_MS);
  usePageTitle(data?.fqdn ?? "도메인");

  if (!valid) return <NotFound />;
  if (loading && !data) return <p className="muted">불러오는 중…</p>;
  if (!data) {
    return (
      <>
        <Notice kind="error">{error}</Notice>
        <p><Link to="/">← 대시보드로</Link></p>
      </>
    );
  }

  const now = Date.now();
  const onChanged = () => void reload();
  return (
    <>
      <p className="crumb"><Link to="/">← 대시보드</Link></p>
      <div className="page-head">
        <div>
          <h1>
            {data.fqdn} <StatusBadge status={data.status} />
          </h1>
          <div className="muted">
            {data.memo || "메모 없음"}
          </div>
        </div>
        {admin && (
          <div className="actions">
            <button type="button" className="btn" onClick={() => setDialog("memo")}>메모 수정</button>
            <button type="button" className="btn danger-text" onClick={() => setDialog("delete")}>도메인 삭제</button>
          </div>
        )}
      </div>

      {error && <Notice kind="error">새로 고치지 못했습니다: {error}</Notice>}
      <ReasonList reasons={data.reasons} />

      <div className="section-head">
        <h2>모니터링 대상</h2>
        {admin && (
          <button type="button" className="btn primary small" onClick={() => setDialog("target")}>대상 추가</button>
        )}
      </div>
      {data.targets.length === 0 ? (
        <div className="empty">
          <p>모니터링 대상이 없습니다.{admin ? " \"대상 추가\"로 점검할 호스트를 등록하세요." : ""}</p>
        </div>
      ) : (
        data.targets.map((t) => (
          <TargetCard key={t.id} domainId={data.id} target={t} admin={admin} now={now} onChanged={onChanged} />
        ))
      )}

      <AcmePanel domain={data} admin={admin} now={now} onChanged={onChanged} />

      {dialog === "memo" && (
        <MemoDialog domainId={data.id} initial={data.memo} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "target" && (
        <TargetDialog domainId={data.id} onClose={() => setDialog(null)} onSaved={onChanged} />
      )}
      {dialog === "delete" && (
        <ConfirmDialog
          title="도메인 삭제"
          confirmLabel="삭제"
          danger
          onClose={() => setDialog(null)}
          onConfirm={async () => {
            await api.deleteDomain(data.id);
            navigate("/");
          }}
        >
          <p><strong>{data.fqdn}</strong>과 모든 대상, 점검 이력을 삭제합니다.</p>
          {data.acme.enabled && <p>ACME가 켜져 있어서 먼저 끄고 챌린지 TXT를 지웁니다. 서버의 acme.sh 갱신은 더 이상 되지 않습니다.</p>}
        </ConfirmDialog>
      )}
    </>
  );
}
```

`web/src/main.tsx`를 아래로 바꾼다(`domains/:id` 경로 추가):

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AppProvider } from "./app/context.tsx";
import { Layout } from "./components/Layout.tsx";
import { Dashboard } from "./pages/Dashboard.tsx";
import { DomainDetail } from "./pages/DomainDetail.tsx";
import { NotFound } from "./pages/NotFound.tsx";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <AppProvider>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="domains/:id" element={<DomainDetail />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </AppProvider>
    </BrowserRouter>
  </StrictMode>,
);
```

- [ ] **Step 4: 빌드를 확인한다**

Run: `cd web && npm test && npm run build`
Expected: 테스트 33개 통과, 타입 오류 없음, 빌드 성공.

- [ ] **Step 5: 커밋**

```bash
git add web
git commit -m "feat(web): add domain detail with target and ACME management" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 7: 감사 로그 화면

**Files:**
- Create: `web/src/pages/Audit.tsx`
- Modify: `web/src/main.tsx`

**Interfaces:**
- Consumes: `api.audit(beforeId?, limit)`, `useApp().admin/ready`, `useResource`, `useAction`.
- Produces: `/audit` 경로(관리자만 내용을 본다. 비로그인이면 안내 문구). `더 보기`는 마지막 항목의 `id`를 `before_id`로 넘긴다.

- [ ] **Step 1: 페이지를 만든다**

`web/src/pages/Audit.tsx`:

```tsx
import { useEffect, useState } from "react";
import { api } from "../api/endpoints.ts";
import type { AuditEntry } from "../api/types.ts";
import { useApp } from "../app/context.tsx";
import { useAction } from "../app/useAction.ts";
import { usePageTitle } from "../app/usePageTitle.ts";
import { useResource } from "../app/useResource.ts";
import { Notice } from "../components/Notice.tsx";
import { formatDateTime } from "../lib/format.ts";

const PAGE = 100;

export function Audit() {
  usePageTitle("감사 로그");
  const { admin, ready } = useApp();
  const { data, error, loading } = useResource(() => (admin ? api.audit(undefined, PAGE) : Promise.resolve([])), [admin]);
  const [older, setOlder] = useState<AuditEntry[]>([]);
  const [exhausted, setExhausted] = useState(false);
  const more = useAction();

  // 첫 페이지를 다시 불러오면 "더 보기"로 붙인 것은 버린다.
  useEffect(() => {
    setOlder([]);
    setExhausted(false);
  }, [data]);

  if (!ready) return <p className="muted">불러오는 중…</p>;
  if (!admin) return <Notice kind="info">감사 로그는 관리자만 볼 수 있습니다. 오른쪽 위에서 로그인하세요.</Notice>;

  const entries = [...(data ?? []), ...older];
  const last = entries[entries.length - 1];

  async function loadMore() {
    if (!last) return;
    const page = await more.run(() => api.audit(last.id, PAGE));
    if (!page) return;
    setOlder((o) => [...o, ...page]);
    if (page.length < PAGE) setExhausted(true);
  }

  return (
    <>
      <div className="page-head">
        <h1>감사 로그</h1>
      </div>
      {error && <Notice kind="error">{error}</Notice>}
      {loading && !data && <p className="muted">불러오는 중…</p>}
      {data && entries.length === 0 && <div className="empty"><p>기록이 없습니다.</p></div>}
      {entries.length > 0 && (
        <div className="table-wrap">
          <table className="table">
            <thead>
              <tr>
                <th>시각</th>
                <th>동작</th>
                <th>대상</th>
                <th>사용자</th>
                <th>IP</th>
                <th>상세</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id}>
                  <td className="nowrap">{formatDateTime(e.at)}</td>
                  <td><code>{e.action}</code></td>
                  <td>{e.target}</td>
                  <td>{e.actor}</td>
                  <td><code>{e.source_ip}</code></td>
                  <td className="muted">{e.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {more.error && <Notice kind="error">{more.error}</Notice>}
      {entries.length >= PAGE && !exhausted && (
        <div className="actions center">
          <button type="button" className="btn" disabled={more.busy} onClick={() => void loadMore()}>더 보기</button>
        </div>
      )}
    </>
  );
}
```

- [ ] **Step 2: 경로를 연결한다**

`web/src/main.tsx`를 아래 최종 내용으로 바꾼다:

```tsx
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter, Route, Routes } from "react-router-dom";
import { AppProvider } from "./app/context.tsx";
import { Layout } from "./components/Layout.tsx";
import { Audit } from "./pages/Audit.tsx";
import { Dashboard } from "./pages/Dashboard.tsx";
import { DomainDetail } from "./pages/DomainDetail.tsx";
import { NotFound } from "./pages/NotFound.tsx";
import "./styles.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <BrowserRouter>
      <AppProvider>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<Dashboard />} />
            <Route path="domains/:id" element={<DomainDetail />} />
            <Route path="audit" element={<Audit />} />
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </AppProvider>
    </BrowserRouter>
  </StrictMode>,
);
```

- [ ] **Step 3: 빌드를 확인한다**

Run: `cd web && npm test && npm run build`
Expected: 테스트 33개 통과, 타입 오류 없음, 빌드 성공.

- [ ] **Step 4: 커밋**

```bash
git add web
git commit -m "feat(web): add audit log page" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 8: Docker 빌드에 화면 포함, Makefile, smoke 확장

**Files:**
- Modify: `Dockerfile`
- Modify: `.dockerignore`
- Modify: `Makefile`
- Modify: `scripts/smoke.sh`

**Interfaces:**
- Consumes: Task 3~7의 `internal/webui/dist`(빌드 결과를 내장), `web/`의 `npm test`·`npm run build`.
- Produces: 화면이 들어간 이미지, `make web-test`, `make web-build`, `scripts/smoke.sh`의 화면·llms.txt·`/api/codes` 확인.

- [ ] **Step 1: Dockerfile을 바꾼다**

`Dockerfile`을 아래 내용으로 바꾼다. Node 단계에서 테스트와 빌드가 통과해야 이미지가 만들어진다.

```dockerfile
# syntax=docker/dockerfile:1
# 1) 대시보드(web/)를 빌드한다. 타입 검사와 로직 테스트가 통과해야 이미지가 만들어진다.
FROM node:24 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite.config.ts가 결과를 ../internal/webui/dist로 내보낸다.
RUN npm test && npm run build

# 2) 빌드 결과를 내장해 Go 바이너리를 만든다.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/webui/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/ssl-checker ./cmd/ssl-checker

# distroless/static: 셸·curl 없음. CA 인증서는 포함돼 Cloudflare HTTPS 호출이 된다.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ssl-checker /usr/local/bin/ssl-checker
USER nonroot
CMD ["ssl-checker", "serve", "--listen", ":8080"]
```

- [ ] **Step 2: `.dockerignore`와 Makefile을 바꾼다**

```diff
diff --git a/.dockerignore b/.dockerignore
index 52bc885..fd6b814 100644
--- a/.dockerignore
+++ b/.dockerignore
@@ -5,3 +5,5 @@ deploy
 scripts
 test
 *.md
+web/node_modules
+internal/webui/dist
```

```diff
diff --git a/Makefile b/Makefile
index ef2e98a..28808a2 100644
--- a/Makefile
+++ b/Makefile
@@ -1,6 +1,6 @@
 TEST_DATABASE_URL ?= postgres://postgres:test@localhost:55432/postgres?sslmode=disable
 
-.PHONY: db-up db-down test
+.PHONY: db-up db-down test web-test web-build
 
 db-up:
 	docker run -d --rm --name sslchk-testdb -e POSTGRES_PASSWORD=test -p 55432:5432 postgres:17
@@ -11,3 +11,11 @@ db-down:
 
 test:
 	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...
+
+# 대시보드(web/). Go 테스트와 따로 돌린다. Node 24가 필요하다.
+web-test:
+	cd web && npm ci && npm test
+
+# 빌드 결과는 internal/webui/dist로 간다(바이너리가 내장한다).
+web-build:
+	cd web && npm ci && npm run build
```

- [ ] **Step 3: smoke에 확인을 더한다**

```diff
diff --git a/scripts/smoke.sh b/scripts/smoke.sh
index e87c102..638bf6d 100755
--- a/scripts/smoke.sh
+++ b/scripts/smoke.sh
@@ -1,5 +1,5 @@
 #!/bin/sh
-# compose 스택(memory 공급자)을 저장소에서 빌드해 띄우고 register → update → health를 확인한다.
+# compose 스택(memory 공급자)을 저장소에서 빌드해 띄우고 register → update → 관리 API → 화면·llms.txt를 확인한다.
 set -eu
 cd "$(dirname "$0")/.."
 
@@ -76,5 +76,22 @@ curl -fsS -c "$jar" -X POST "$BASE/api/session" -H "$json" -d "{\"key\":\"$ADMIN
 curl -fsS -b "$jar" "$BASE/api/session" | grep -q '"admin":true' || { echo "session cookie not accepted" >&2; exit 1; }
 rm -f "$jar"
 
+# 화면과 llms.txt. 화면 파일은 이미지를 빌드할 때 내장된다.
+ui=$(curl -fsS -o /dev/null -w '%{http_code} %{content_type}' "$BASE/")
+case "$ui" in "200 text/html"*) ;; *) echo "dashboard not served: $ui" >&2; exit 1 ;; esac
+spa=$(curl -fsS -o /dev/null -w '%{http_code}' "$BASE/domains/1")
+[ "$spa" = 200 ] || { echo "SPA fallback returned $spa" >&2; exit 1; }
+js=$(curl -fsS "$BASE/" | sed -n 's/.*src="\(\/assets\/[^"]*\.js\)".*/\1/p')
+[ -n "$js" ] || { echo "no script in index.html" >&2; exit 1; }
+curl -fsS -D - -o /dev/null "$BASE$js" | grep -qi 'cache-control: public, max-age=31536000, immutable' || { echo "asset cache header missing" >&2; exit 1; }
+code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/assets/missing.js")
+[ "$code" = 404 ] || { echo "missing asset returned $code" >&2; exit 1; }
+code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/update")
+[ "$code" = 405 ] || { echo "GET /update returned $code (화면이 API 경로를 가로챘다)" >&2; exit 1; }
+llms=$(curl -fsS "$BASE/llms.txt")
+echo "$llms" | grep -q "챌린지 존: $CHALLENGE_ZONE" || { echo "llms.txt missing zone" >&2; exit 1; }
+echo "$llms" | grep -q "$BASE/api/domains" || { echo "llms.txt missing base url" >&2; exit 1; }
+curl -fsS "$BASE/api/codes" | grep -q '"code":"CONNECT_FAILED"' || { echo "api/codes missing codes" >&2; exit 1; }
+
 $COMPOSE ps worker --format '{{.State}}' | grep -q running || { $COMPOSE logs worker; echo "worker not running" >&2; exit 1; }
 echo "smoke OK"
```

- [ ] **Step 4: 확인한다**

Run: `make web-test`
Expected: `ℹ pass 33`.

Run: `docker build -t sslchk-plan3-check .`
Expected: 이미지가 만들어진다(Node 단계의 `npm test`와 `npm run build`가 로그에 보인다). 끝나면 `docker rmi sslchk-plan3-check`.

Run: `./scripts/smoke.sh`
Expected: 마지막 줄 `smoke OK`. (이미지를 빌드해 compose 스택을 띄우고 화면 `/`, SPA 폴백 `/domains/1`, 에셋 캐시 헤더, 없는 에셋 404, `GET /update` 405, `/llms.txt`, `/api/codes`를 확인한다.)

- [ ] **Step 5: 커밋**

```bash
git add Dockerfile .dockerignore Makefile scripts/smoke.sh
git commit -m "build: build the dashboard in the image and extend smoke checks" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 9: 문서

**Files:**
- Create: `docs/guide/dashboard.md`
- Modify: `README.md`, `docs/guide/api.md`, `docs/guide/onboarding.md`, `docs/guide/installation.md`, `docs/guide/operations.md`

**Interfaces:**
- Consumes: 앞 태스크의 동작(화면, 일회성 창, `오래됨`, 세션 한계, `/llms.txt`, `make web-*`).
- Produces: 사용자 문서. 코드 변경 없음.

- [ ] **Step 1: 대시보드 가이드를 쓴다**

`docs/guide/dashboard.md`:

````markdown
# 대시보드

웹 화면에서 도메인의 인증서 상태를 보고, 관리자는 도메인과 점검 대상을 관리하고 ACME를 켭니다. 아래에서 `<앱>`은 api 주소입니다([설치](installation.md) 참고). 화면은 `<앱>/`에서 열립니다.

## 누가 무엇을 하나

| | 비로그인 | 관리자 (`ADMIN_KEY`로 로그인) |
| --- | --- | --- |
| 도메인 상태, 점검 결과, 이력, ACME 상태 | 볼 수 있음 | 볼 수 있음 |
| 도메인·대상 추가·수정·삭제, 지금 점검 | 버튼이 보이지 않음 | 가능 |
| ACME 켜기·허용 IP 수정·키 재발급·CNAME 확인·끄기 | 버튼이 보이지 않음 | 가능 |
| 감사 로그 | 볼 수 없음 | 볼 수 있음 |

조회는 인증 없이 되므로, 서버 IP와 메모가 화면에 보입니다. 외부에 공개하기 어려우면 앞단 프록시에서 접근을 제한하세요([설정](configuration.md)의 Caddy 예시).

## 화면

**대시보드(`/`)**: 위쪽 카드는 상태별 도메인 수입니다. 카드를 누르면 그 상태만 보이고, 다시 누르면 풀립니다. 표는 나쁜 상태, 만료가 가까운 순서로 정렬됩니다. 60초마다 자동으로 새로 고칩니다.

**도메인 상세(`/domains/<id>`)**: 대상별로 접속 주소마다 상태, 만료일, 발급자, serial 앞 8자, TLS 버전, 마지막 점검을 보여 줍니다. `이력`을 누르면 그 대상의 최근 점검 50건이 나옵니다. 아래쪽 ACME 패널에는 CNAME 레코드, CNAME 확인 결과, 마지막 TXT 갱신, 연속 실패 횟수, 허용 IP가 있습니다.

**감사 로그(`/audit`, 관리자)**: 로그인, 쓰기 작업, ACME 갱신 기록이 최신순으로 나옵니다.

### 상태와 판정 코드

배지는 색과 글자를 함께 씁니다: **긴급**(빨강), **경고**(주황), **정보**(파랑), **정상**(초록), **미점검**(회색). 배지 아래의 `CONNECT_FAILED` 같은 코드에 마우스를 올리면 뜻이 보입니다. 전체 표는 [API 레퍼런스](api.md)에 있습니다.

**`오래됨` 표시**: 마지막 점검이 점검 주기(`CHECK_INTERVAL`)의 2배보다 오래되면 붙습니다. 상태는 점검할 때 저장되므로, worker가 멈추면 마지막 "정상"이 그대로 남아 보입니다. 이 표시가 보이면 worker를 확인하세요([운영](operations.md)).

## 관리자 로그인

오른쪽 위 `관리자 로그인`에서 `ADMIN_KEY`를 입력합니다. 로그인은 12시간 유지됩니다.

- **HTTPS로 접속하세요.** 공개 주소가 `http://`이면 로그인 쿠키에 `Secure`를 붙이지 않아(붙이면 브라우저가 쿠키를 보내지 않습니다) 네트워크에서 쿠키가 보일 수 있습니다.
- **로그아웃은 브라우저의 쿠키만 지웁니다.** 서버에는 세션이 없어서, 이미 복사된 쿠키는 만료(12시간)까지 유효합니다. 모든 세션을 끊으려면 `ADMIN_KEY`를 바꿔 다시 배포합니다.
- 틀린 키는 IP당 5번까지 받고, 그 뒤로는 12초에 1번만 받습니다.
- 세션이 만료된 채로 쓰기를 시도하면 화면이 조회 전용으로 돌아갑니다. 다시 로그인하세요.

## 도메인과 대상 등록

1. `도메인 추가`에서 도메인을 입력합니다(와일드카드 없이 `a.com`). 이 단계에서는 ACME가 꺼져 있습니다.
2. 도메인 상세에서 `대상 추가`로 점검할 TLS 엔드포인트를 등록합니다.

| 항목 | 설명 |
| --- | --- |
| 호스트명, 포트 | 필수. 예: `www.a.com`, `443` |
| SNI | 비우면 호스트명을 씁니다 |
| 서버 IP | 비우면 호스트명으로 한 번만 접속합니다(DNS가 여러 주소를 줘도 하나만 점검). 적으면 IP마다 따로 접속해, 서버마다 같은 인증서가 깔렸는지 비교합니다. 최대 16개 |

저장하면 바로 한 번 점검해서 결과를 보여 줍니다. 그 뒤로는 worker가 `CHECK_INTERVAL`(기본 6시간)마다 점검합니다. 접속 설정을 바꾸면 옛 주소의 결과는 더 이상 보이지 않습니다.

## ACME 켜기

도메인 상세의 ACME 패널에서 `ACME 켜기`를 누릅니다.

1. **허용 IP**를 입력합니다. 인증서를 받을 서버의 IP나 CIDR입니다. 키가 새어도 다른 곳에서는 못 쓰게 가능한 한 지정하세요.
2. 켜면 **한 번만 보이는 창**이 뜹니다. CNAME 레코드, username·password, 서버에서 실행할 명령이 들어 있습니다. **이 창을 닫으면 password를 다시 볼 수 없습니다.** Esc로는 닫히지 않고, 버튼으로만 닫힙니다.
3. 도메인의 DNS에 CNAME 한 줄을 등록하고, 패널의 `CNAME 지금 확인`으로 `CNAME 정상`이 되는지 봅니다.
4. 인증서를 받을 서버에서 창의 명령을 실행합니다([도메인 연결과 인증서 발급](onboarding.md)). 비밀번호가 셸 기록에 남지 않게 하려면 `export ACMEDNS_PASSWORD=...` 줄 대신 `read -rs ACMEDNS_PASSWORD && export ACMEDNS_PASSWORD`를 쓰세요.
5. 발급이 되면 패널의 `마지막 TXT 갱신`이 채워집니다.

**키 재발급**: 옛 키는 즉시 무효가 되고 서브도메인(CNAME)은 그대로입니다. 서버에서 `--issue ... --force`를 다시 실행해야 새 키가 acme.sh 설정에 저장됩니다. 그렇지 않으면 다음 자동 갱신이 401로 실패합니다.

**ACME 끄기**: 계정을 폐기하고 챌린지 TXT를 지웁니다. 다시 켜면 서브도메인이 새로 생기므로 CNAME을 다시 걸어야 합니다.

## 에이전트를 위한 `/llms.txt`

`<앱>/llms.txt`는 에이전트가 이 서버로 ACME를 구성할 때 따라 할 절차를 담은 텍스트입니다. 이 서버의 주소(`PUBLIC_URL`, 비어 있으면 요청에서 만든 값)와 챌린지 존(`CHALLENGE_ZONE`)이 채워져 있고, 인증 없이 읽을 수 있습니다. 에이전트에게 이 주소를 알려 주고 관리자 키만 따로 전달하세요. 키를 채팅이나 티켓에 붙여 넣지 않는 것이 안전합니다.

## 로컬에서 띄워 보기

Docker가 있으면 메모리 공급자(실제 DNS에 기록하지 않음)로 바로 띄울 수 있습니다.

```bash
docker compose -p sslchk-demo -f deploy/compose.yaml -f deploy/compose.ports.yaml \
  --env-file deploy/local.env.example up -d --build
# http://127.0.0.1:18090 을 열고, deploy/local.env.example의 ADMIN_KEY로 로그인
docker compose -p sslchk-demo -f deploy/compose.yaml -f deploy/compose.ports.yaml \
  --env-file deploy/local.env.example down -v
```

## 화면 개발

화면은 `web/`의 React + Vite + TypeScript입니다. Node 24가 필요합니다.

```bash
make web-test      # 로직 테스트(node --test)
make web-build     # 타입 검사 + 빌드. 결과는 internal/webui/dist로 가서 바이너리에 내장됩니다
cd web && npm run dev   # 개발 서버(:5173). /api는 로컬 ssl-checker serve(:8080)로 넘어갑니다
```

Docker 이미지는 빌드할 때 화면의 테스트와 빌드를 먼저 돌리므로, 둘 중 하나가 실패하면 이미지가 만들어지지 않습니다. 화면을 빌드하지 않고 `go build`만 한 바이너리는 `<앱>/`에 "화면이 빌드되지 않았습니다" 안내(503)를 보여 주며, API와 `/llms.txt`는 그대로 동작합니다.
````

- [ ] **Step 2: 기존 문서를 고친다**

README의 소개를 "모니터링 + ACME 위임"으로 바꾸고, 기능·문서 표·명령 표·개발 절차를 갱신하고, 더 이상 맞지 않는 "아직 없는 것"을 고친다. API 문서에 `/api/codes`와 화면·`/llms.txt`를, 연결 가이드·설치·운영 문서에 화면 안내를 더한다:

````diff
diff --git a/README.md b/README.md
index 2546cb4..ff26102 100644
--- a/README.md
+++ b/README.md
@@ -1,10 +1,10 @@
 # ssl-checker
 
-DNS API가 없는 도메인에서도 acme.sh로 Let's Encrypt 인증서를 자동 발급·갱신할 수 있게 해 주는 DNS-01 검증 대행 서버입니다. 와일드카드 인증서도 됩니다.
+등록한 도메인의 TLS 인증서 상태(만료일, 발급자, 서버별 일치 여부)를 주기적으로 점검해 웹 대시보드에 보여 주는 서버입니다. 부가 기능으로, DNS API가 없는 도메인에서도 acme.sh로 Let's Encrypt 인증서를 자동 발급·갱신할 수 있게 해 주는 DNS-01 검증 대행을 제공합니다. 와일드카드 인증서도 됩니다. ACME는 등록한 도메인마다 켜고 끌 수 있습니다.
 
 [acme-dns](https://github.com/joohoi/acme-dns)와 같은 API를 제공하므로, 각 서버는 acme.sh에 들어 있는 `dns_acmedns` 플러그인을 그대로 씁니다. 클라이언트 쪽에 따로 설치할 것은 없습니다. acme-dns와 달리 직접 DNS 서버를 띄우지 않고, 챌린지 TXT를 Cloudflare 같은 DNS 공급자 API로 기록합니다. 그래서 53번 포트를 열거나 NS를 위임할 필요가 없습니다.
 
-## 동작 방식
+## ACME 위임 동작 방식
 
 ```
  서버(acme.sh) ──(1) 발급 요청──▶ Let's Encrypt
@@ -24,6 +24,9 @@ DNS API가 없는 도메인에서도 acme.sh로 Let's Encrypt 인증서를 자
 
 ## 기능
 
+- 인증서 모니터링: 도메인마다 점검 대상(호스트명·포트·SNI·서버 IP 목록)을 등록하면 주기적으로 만료일, 발급자, 체인, 호스트명, TLS 버전을 점검합니다. 서버마다 같은 인증서가 깔렸는지도 비교합니다.
+- 웹 대시보드: 누구나 상태를 볼 수 있고, 등록·수정·ACME 설정은 `ADMIN_KEY`로 로그인한 관리자만 합니다([대시보드](docs/guide/dashboard.md)).
+- `/llms.txt`: 에이전트가 ACME를 구성할 때 따라 할 절차입니다. 서버 주소와 챌린지 존이 채워져 있습니다.
 - acme-dns 호환 `/update` API: 원본 acme-dns와 응답이 같은지 자동 테스트로 확인합니다.
 - 도메인별 계정과 키: 키는 bcrypt 해시로만 저장하고, 계정마다 허용 IP 대역(`allowfrom`)을 지정할 수 있습니다.
 - 값 2개 유지: `a.com`과 `*.a.com`을 한 인증서로 발급할 때 필요한 TXT 2개를 함께 유지합니다.
@@ -34,8 +37,8 @@ DNS API가 없는 도메인에서도 acme.sh로 Let's Encrypt 인증서를 자
 
 ### 아직 없는 것
 
-- 관리 화면. 관리 API는 있습니다([API](docs/guide/api.md)). 화면은 아직 없습니다.
-- 인증서 만료 모니터링과 알림.
+- 알림(Slack, 메일 등). 상태는 대시보드와 API로 확인합니다.
+- 사용자별 계정과 역할. 관리자 키 하나로 관리합니다.
 
 ## 빠른 시작 (로컬에서 써 보기)
 
@@ -55,7 +58,8 @@ git clone <이 저장소> ssl-checker && cd ssl-checker
 | [설치와 배포](docs/guide/installation.md) | 준비물, 이미지 빌드, docker compose 배포, 첫 기동 확인 |
 | [설정](docs/guide/configuration.md) | 환경변수 전체 목록, 네트워크·프록시 설정, 비밀값 관리 |
 | [도메인 연결과 인증서 발급](docs/guide/onboarding.md) | 계정 등록, CNAME 등록, acme.sh 발급·설치·갱신, 서버 추가 |
-| [API 레퍼런스](docs/guide/api.md) | `/update`, `/register`, `/health`의 요청·응답·오류 코드 |
+| [대시보드](docs/guide/dashboard.md) | 화면 사용법, 관리자 로그인, 대상 등록, ACME 켜기, `/llms.txt`, 화면 개발 |
+| [API 레퍼런스](docs/guide/api.md) | `/update`, `/register`, `/health`와 `/api`의 요청·응답·오류 코드 |
 | [운영](docs/guide/operations.md) | 업데이트, 백업, 키 교체, 계정 폐기, 감사 로그, 문제 해결 |
 
 ## 명령
@@ -65,16 +69,17 @@ git clone <이 저장소> ssl-checker && cd ssl-checker
 | 명령 | 역할 |
 | --- | --- |
 | `ssl-checker migrate` | DB 마이그레이션을 적용하고 종료 |
-| `ssl-checker serve --listen :8080` | `/update`, `/register`, `/health` API |
-| `ssl-checker worker` | 시작할 때 한 번, 그 뒤 24시간마다 고아 TXT 정리 |
+| `ssl-checker serve --listen :8080` | `/update`, `/register`, `/health`, `/api`, 대시보드, `/llms.txt` |
+| `ssl-checker worker` | 시작할 때 한 번, 그 뒤 24시간마다 고아 TXT 정리. `CHECK_INTERVAL`마다 TLS 점검과 CNAME 확인 |
 | `ssl-checker healthcheck [--url URL]` | 컨테이너 헬스체크용 `/health` 확인 |
 
 ## 개발
 
-Go 1.27 이상과 Docker가 필요합니다.
+Go 1.27 이상과 Docker가 필요합니다. 화면(`web/`)을 고치려면 Node 24도 필요합니다.
 
 ```bash
 make db-up && make test && make db-down   # 단위·통합 테스트 (Postgres 컨테이너 사용)
+make web-test && make web-build           # 화면 로직 테스트, 타입 검사와 빌드
 ./scripts/smoke.sh                        # 이미지 + compose 스택 smoke 테스트
 ./scripts/compat.sh                       # 원본 acme-dns와 /update 응답 대조
 ```
````

````diff
diff --git a/docs/guide/api.md b/docs/guide/api.md
index e2609e0..8d5ea50 100644
--- a/docs/guide/api.md
+++ b/docs/guide/api.md
@@ -109,6 +109,9 @@ DB 연결과 DNS 공급자 상태를 확인합니다.
 | `GET /api/domains/{id}` | 대상별·접속 주소별 최신 점검 결과와 ACME 상태 |
 | `GET /api/targets/{id}/results?limit=50` | 대상의 점검 이력, 최신순. `limit` 최대 500 |
 | `GET /api/config` | `challenge_zone`, `check_interval`(Go 시간 문자열, 예: `"6h0m0s"`), `public_url` |
+| `GET /api/codes` | 판정 코드 표 `{"codes":[{"code","status","meaning"}]}`. 나쁜 상태부터 정렬됩니다. 화면과 `/llms.txt`가 같은 표를 씁니다 |
+
+인증서를 받지 못한 점검 결과(접속 실패 등)에는 `not_before`·`not_after` 필드가 없습니다.
 
 상태는 `critical` > `warning` > `info` > `ok` > `unknown` 순으로 나쁩니다. 도메인 상태는 대상과 ACME 상태 중 가장 나쁜 것입니다.
 
@@ -210,3 +213,12 @@ read -rs ADMIN_KEY
 curl -s -X POST '<앱>/api/domains' -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
   -d '{"fqdn":"a.com"}'
 ```
+
+## 화면과 `/llms.txt`
+
+| 요청 | 내용 |
+| --- | --- |
+| `GET /`, `/domains/<id>`, `/audit` | 대시보드(SPA). 화면을 빌드하지 않은 바이너리는 503과 안내 페이지를 돌려줍니다 |
+| `GET /llms.txt` | 에이전트가 ACME를 구성할 때 따라 할 절차(`text/plain`). 서버 주소와 `CHALLENGE_ZONE`이 채워져 있고 인증이 필요 없습니다 |
+
+화면은 `/api/`, `/update`, `/register`, `/health`, `/llms.txt`를 가로채지 않습니다. 사용법은 [대시보드](dashboard.md)에 있습니다.
````

```diff
diff --git a/docs/guide/onboarding.md b/docs/guide/onboarding.md
index 43404fb..5246f3b 100644
--- a/docs/guide/onboarding.md
+++ b/docs/guide/onboarding.md
@@ -4,6 +4,8 @@
 
 아래에서 `<앱>`은 클라이언트가 부르는 api 주소입니다. 호스트 포트로 열었다면 `http://<BIND_ADDR>:<API_PORT>`, Caddy 뒤라면 Caddy에 설정한 주소(예: `https://ssl-checker.example.com`)입니다. `<존>`은 `CHALLENGE_ZONE` 값입니다.
 
+아래 API 호출은 [대시보드](dashboard.md)에서도 할 수 있습니다. 도메인 상세의 `ACME 켜기`가 1단계, `CNAME 지금 확인`이 2단계의 확인에 해당하고, 켠 직후 나오는 창에 3단계 명령이 값이 채워진 채로 들어 있습니다. 에이전트에게 맡길 때는 `<앱>/llms.txt`를 알려 주세요.
+
 ## 전체 흐름
 
 | 단계 | 누가 | 횟수 |
```

```diff
diff --git a/docs/guide/installation.md b/docs/guide/installation.md
index e0006cf..66f640c 100644
--- a/docs/guide/installation.md
+++ b/docs/guide/installation.md
@@ -109,6 +109,8 @@ curl https://ssl-checker.example.com/health        # Caddy 뒤일 때 (Caddy에
 
 `provider`가 `ok`가 될 때까지는 도메인 연결을 시작하지 않습니다.
 
+브라우저로 `<앱>/`을 열어 [대시보드](dashboard.md)가 보이는지도 확인합니다. 이미지를 빌드할 때 화면(Node)도 함께 빌드하므로 처음 빌드는 npm 패키지를 받느라 조금 더 걸립니다.
+
 ## Komodo로 배포 (저장소 모드)
 
 Komodo Stack의 Git 저장소 모드를 쓰면 위의 3~5단계를 Komodo가 대신합니다. 배포할 때마다 이 저장소를 받아 compose로 띄우고, 이미지는 compose가 저장소의 Dockerfile로 빌드합니다. 레지스트리와 서버 접속이 필요 없습니다. 설정 전체는 [`deploy/komodo/ssl-checker.toml`](../../deploy/komodo/ssl-checker.toml)에 있습니다.
```

```diff
diff --git a/docs/guide/operations.md b/docs/guide/operations.md
index 192f449..953b8fa 100644
--- a/docs/guide/operations.md
+++ b/docs/guide/operations.md
@@ -147,6 +147,8 @@ psql-sslchk -c "SELECT target, count(*), max(at) FROM audit_logs WHERE action =
 | `update_failed` + `transient` | Cloudflare 장애·요청 제한. acme.sh가 다음 실행 때 재시도합니다. |
 | CA 검증 실패 (TXT를 찾지 못함) | CNAME 대상 오타, 챌린지 존의 네임서버 위임, `dig TXT <fulldomain> @1.1.1.1`로 값이 보이는지 |
 | CA가 CAA 때문에 거부 | 도메인의 CAA 레코드에 `letsencrypt.org` 추가 |
+| 대시보드에 `오래됨` 표시 | worker가 멈췄거나 점검 주기(`CHECK_INTERVAL`)의 2배 넘게 점검이 안 됐습니다. `dc ps worker`, `dc logs worker`를 확인합니다. |
+| `<앱>/`이 503 "화면이 빌드되지 않았습니다" | Docker 이미지가 아니라 화면 없이 `go build`한 바이너리입니다. `make web-build` 뒤 다시 빌드하거나 이미지를 씁니다. API는 정상입니다. |
 | 발급은 됐는데 웹서버가 옛 인증서를 씀 | `--install-cert`의 `--reloadcmd`가 실행 권한이 있는지, 웹서버 설정의 인증서 경로 |
 
 ## 제거
```

- [ ] **Step 3: 확인한다**

Run: `grep -rn "화면은 아직 없습니다\|아직 없는 것: 관리 화면" README.md docs/guide`
Expected: 출력 없음.

Run: `grep -n "dashboard.md" README.md docs/guide/*.md | head`
Expected: README, api, onboarding, installation 문서에서 `dashboard.md` 링크가 보인다.

문서의 링크(`dashboard.md`, `api.md`, `operations.md`, `onboarding.md`, `installation.md`, `configuration.md`)가 모두 실제 파일을 가리키는지 눈으로 확인한다.

- [ ] **Step 4: 커밋**

```bash
git add README.md docs/guide
git commit -m "docs: document the dashboard, /llms.txt and /api/codes" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```

---

### Task 10: 브라우저 확인 (컨트롤러가 직접 한다)

구현 에이전트가 아니라 컨트롤러가, 모든 태스크가 끝난 뒤 실제 브라우저로 확인한다. 컴포넌트 테스트가 없는 화면의 동작과 Review Focus 두 항목을 여기서 고정한다. 확인 도구는 Playwright MCP나 Chrome 확장을 쓴다.

**준비:**

```bash
# 테스트 DB 컨테이너가 떠 있어야 한다(make db-up). 확인용 DB를 따로 만든다.
docker exec sslchk-testdb psql -U postgres -c 'CREATE DATABASE plan3ui'
make web-build && go build -o /tmp/plan3-ssl-checker ./cmd/ssl-checker
export DATABASE_URL='postgres://postgres:test@localhost:55432/plan3ui?sslmode=disable' DNS_PROVIDER=memory \
  CHALLENGE_ZONE=acme.test ADMIN_KEY=0123456789abcdef0123456789abcdef0123 CHECK_INTERVAL=5m
/tmp/plan3-ssl-checker migrate
/tmp/plan3-ssl-checker serve --listen :18080 &
/tmp/plan3-ssl-checker worker &
# 자료: 정상(example.com), 접속 실패(localhost:1), 미점검(대상 없음), ACME 켜짐
A="Authorization: Bearer $ADMIN_KEY"; J="Content-Type: application/json"; B=http://localhost:18080
curl -s -X POST $B/api/domains -H "$A" -H "$J" -d '{"fqdn":"example.com","memo":"공개 예시"}'
curl -s -X POST $B/api/domains/1/targets -H "$A" -H "$J" -d '{"hostname":"example.com","port":443}'
curl -s -X POST $B/api/domains -H "$A" -H "$J" -d '{"fqdn":"broken.test"}'
curl -s -X POST $B/api/domains/2/targets -H "$A" -H "$J" -d '{"hostname":"localhost","port":1,"ips":["127.0.0.1"]}'
curl -s -X POST $B/api/domains -H "$A" -H "$J" -d '{"fqdn":"empty.test"}'
```

**확인 목록** (`http://localhost:18080/`):

- [ ] 비로그인 대시보드: 요약 카드 5개, 표가 긴급 → 정상 → 미점검 순서, 쓰기 버튼 없음, 콘솔 오류 없음. 카드를 누르면 `?status=`로 필터된다.
- [ ] 로그인: 틀린 키는 "관리자 키가 맞지 않습니다.", 맞는 키는 로그인되고 새로 고침해도 유지된다(쿠키).
- [ ] 도메인 상세(관리자): 대상 카드, 지금 점검, 이력 펼침, 수정·삭제 확인 창이 동작한다.
- [ ] **일회성 창(Review Focus):** `empty.test`에서 `ACME 켜기` → 허용 IP 입력 → 켜기. 자격 증명 창이 뜨고 **Esc를 눌러도 닫히지 않으며**, `비밀번호를 저장했습니다. 닫기`로만 닫힌다. 닫은 뒤 패널에는 password가 어디에도 없다.
- [ ] 키 재발급 창에 `--force` 경고가 있다.
- [ ] **HTML 주입(Review Focus):** `메모 수정`에 `<img src=x onerror="document.title='XSS'">`를 저장한다. 메모가 글자 그대로 보이고 탭 제목이 바뀌지 않는다.
- [ ] 세션 만료: 개발자 도구에서 `sslchk_session` 쿠키를 지우고 쓰기 동작(메모 저장)을 시도하면 오류 문구가 보이고, 화면이 조회 전용(쓰기 버튼 사라짐)으로 돌아간다.
- [ ] 감사 로그: 방금 한 작업이 최신순으로 보이고, 어디에도 password·키가 없다.
- [ ] `GET /api/domains`, `GET /llms.txt`(서버 주소·`acme.test` 채워짐), `GET /update`(405)가 화면에 가로채이지 않는다.
- [ ] 폭 390px(휴대폰)에서 상단 바와 카드가 깨지지 않는다. 시스템을 다크 모드로 바꿔도 글자가 읽힌다.

**정리:**

```bash
pkill -f plan3-ssl-checker; rm -f /tmp/plan3-ssl-checker
docker exec sslchk-testdb psql -U postgres -c 'DROP DATABASE plan3ui WITH (FORCE)'
```

---

## Self-Review (작성자 점검)

- **스펙 커버리지:** 화면 3개(대시보드·상세·감사)와 상단 로그인(Task 5~7), 일회성 창(Task 6), 상태 색·글자(Task 5 CSS·StatusBadge), 60초 새로 고침(`REFRESH_MS`), `internal/webui`의 embed·SPA 폴백·캐시·placeholder(Task 3), Dockerfile Node 단계·`.dockerignore`(Task 8), `internal/llmstxt` 8개 절(Task 2), smoke 확장(Task 8), 문서(Task 9). 스펙의 "화면: `npm run build` 통과"는 Docker 빌드가 `npm test`까지 더해 강제한다.
- **계획 1·2 이월:** 오래됨 표시(Task 4 `isStale`, Task 5), JSON 모양 결정(types.ts 주석), 일회성 창의 `read -rs`·`--force`(Task 6), README·대시보드 문서(Task 9). 7일 규칙 결정은 2026-10-07에 끝났다.
- **타입 일관성:** `useAction().run`은 실패하면 `undefined`를 돌려주므로, 반환값이 없는 호출은 `run(async () => { await ...; return true; })`로 감싸 성공 여부를 구분한다(MemoDialog·TargetDialog·AllowFromDialog·ConfirmDialog가 같은 방식). `Credentials`·`AcmeOn`·`Target`의 필드는 `internal/adminapi/view.go`·`acme.go`의 JSON 태그와 같다.
