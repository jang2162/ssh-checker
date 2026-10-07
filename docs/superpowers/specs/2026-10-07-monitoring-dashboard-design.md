# ssl-checker 모니터링·대시보드 설계

작성일: 2026-10-07

## 목표

ssl-checker를 **인증서 모니터링이 주목적이고, ACME DNS-01 위임을 부가로 제공하는 도구**로 확장한다.

- 등록한 도메인의 TLS 인증서 상태를 주기적으로 점검하고 웹 대시보드에서 보여 준다.
- 도메인 등록은 ACME와 별개다. 등록된 도메인 중 일부에만 ACME를 켤 수 있고, ACME 상태도 대시보드에서 본다.
- 상태 조회는 누구나, 등록·수정·설정은 `ADMIN_KEY`를 가진 관리자만 한다.
- 에이전트가 이 서버로 ACME를 구성할 수 있도록 `/llms.txt`를 제공한다.

성공 기준: 관리자가 화면에서 도메인을 등록하고 모니터링 대상을 붙이면, 6시간 안에 만료일·발급자·서버별 일치 여부가 보인다. 같은 화면에서 ACME를 켜면 CNAME과 acme.sh 명령이 나오고, 서버에 붙인 뒤 CNAME 확인과 TXT 갱신 상태가 보인다.

## 합의한 결정

| 항목 | 결정 |
| --- | --- |
| 도메인 상태 범위 | 인증서 점검(만료일 등) + ACME CNAME 확인 + TXT 갱신 상태 |
| 모니터링 대상 | ACME 사용 여부와 무관한 아무 호스트. `호스트명 + 포트 + SNI(선택) + IP 목록(선택)` |
| 도메인과 ACME | 도메인 등록 ≠ ACME. ACME는 도메인마다 켜고 끄는 옵션 |
| 알림 | 없음. 대시보드 표시만 |
| 화면 기술 | React + Vite + TypeScript SPA. 빌드 결과를 Go 바이너리에 내장 |
| 관리자 인증 | `ADMIN_KEY`로 로그인 → 서명된 쿠키 세션. API는 `Authorization: Bearer <ADMIN_KEY>`도 허용 |
| 키 재발급 | 서브도메인(CNAME 대상)은 유지하고 키만 교체 |
| IP 목록이 없을 때 | 호스트명으로 한 번 접속 (DNS가 여러 IP를 줘도 하나만 점검) |
| 구현 계획 | 3개로 나눔: ① 모니터링 엔진과 조회 API → ② 관리 API와 세션 → ③ 대시보드 화면과 llms.txt |

## 구조

프로세스는 지금과 같은 셋이다. 바이너리 하나, 이미지 하나로 배포한다.

| 구성 | 하는 일 |
| --- | --- |
| api (`ssl-checker serve`) | 기존 `/update`, `/register`, `/health` + `/api/...` JSON API + 대시보드(SPA) + `/llms.txt`. 관리자의 "지금 점검"도 직접 실행한다. |
| worker (`ssl-checker worker`) | 기존 일일 TXT 정리 + TLS 점검(`CHECK_INTERVAL`, 기본 6시간) + ACME CNAME 확인 + 오래된 점검 기록 정리(하루 한 번) |
| db | PostgreSQL. 마이그레이션 `0002`로 테이블과 컬럼을 추가한다. |

### 패키지

| 패키지 | 책임 |
| --- | --- |
| `internal/monitor` | TLS 접속·인증서 수집·검증(`Probe`), 판정 규칙(`Evaluate`), 대상 점검 실행(`Runner`) |
| `internal/cnamecheck` | 공개 리졸버로 `_acme-challenge` CNAME 조회·비교 |
| `internal/store` | 기존 + 도메인·대상·점검 결과·ACME 상태 저장 |
| `internal/adminapi` | `/api/...` 핸들러, 세션, 권한 |
| `internal/session` | 쿠키 세션 서명·검증 |
| `internal/llmstxt` | `/llms.txt` 템플릿 렌더링 |
| `internal/webui` | 내장한 SPA 정적 파일 제공, SPA 경로 폴백 |
| `web/` | React + Vite + TypeScript 소스 |

`internal/acmedns`(acme-dns 호환 `/update`, `/register`)는 유지한다. ACME 켜기·키 재발급 로직은 `acmedns.Service`에 메서드로 추가하고, `/register`와 `/api`가 함께 쓴다.

## 데이터 모델 (마이그레이션 0002)

### domains

기존 `domains(id, fqdn, memo, status, created_at)`를 그대로 쓴다. `status` 컬럼은 쓰지 않으므로 삭제한다. 도메인 상태는 대상과 ACME에서 계산한다.

### monitor_targets (새)

| 컬럼 | 타입 | 설명 |
| --- | --- | --- |
| `id` | bigserial PK | |
| `domain_id` | bigint FK → domains, ON DELETE CASCADE | |
| `hostname` | text NOT NULL | 접속할 호스트명. 소문자, 끝 점 없음. 와일드카드 불가 |
| `port` | integer NOT NULL DEFAULT 443 | 1–65535 |
| `sni` | text NOT NULL DEFAULT '' | 비우면 hostname 사용 |
| `ips` | text[] NOT NULL DEFAULT '{}' | 직접 접속할 IP(v4/v6) 목록. 비우면 hostname으로 한 번 접속 |
| `memo` | text NOT NULL DEFAULT '' | |
| `created_at` | timestamptz NOT NULL DEFAULT now() | |

`UNIQUE (domain_id, hostname, port, sni)`.

### check_results (새)

대상 × 접속 주소별 점검 한 건.

| 컬럼 | 타입 | 설명 |
| --- | --- | --- |
| `id` | bigserial PK | |
| `target_id` | bigint FK → monitor_targets, ON DELETE CASCADE | |
| `address` | text NOT NULL | 실제 접속한 `IP:port` (IP 목록이 없으면 hostname을 풀어 접속한 주소) |
| `checked_at` | timestamptz NOT NULL DEFAULT now() | |
| `status` | text NOT NULL | `ok`, `info`, `warning`, `critical` |
| `reasons` | text[] NOT NULL DEFAULT '{}' | 판정 코드 목록(아래 표) |
| `tls_version` | text | 예: `TLS 1.3` |
| `cipher` | text | |
| `subject_cn` | text | |
| `sans` | text[] | |
| `issuer` | text | 발급자 CN(없으면 O) |
| `serial` | text | 16진 소문자 |
| `not_before`, `not_after` | timestamptz | |
| `key_alg` | text | `RSA`, `ECDSA`, `Ed25519` |
| `key_bits` | integer | |
| `chain_ok` | boolean | |
| `hostname_ok` | boolean | |
| `error` | text NOT NULL DEFAULT '' | 접속 실패 등의 메시지 |

인덱스: `(target_id, checked_at DESC)`, `(checked_at)`.

### acme_accounts (컬럼 추가)

| 컬럼 | 타입 | 설명 |
| --- | --- | --- |
| `cname_status` | text NOT NULL DEFAULT 'unknown' | `ok`, `missing`, `mismatch`, `error`, `unknown` |
| `cname_value` | text NOT NULL DEFAULT '' | 조회된 CNAME 값 |
| `cname_checked_at` | timestamptz | |
| `last_update_at` | timestamptz | 마지막 `/update` 성공 |
| `last_failure_at` | timestamptz | 마지막 `update_failed` |
| `consecutive_failures` | integer NOT NULL DEFAULT 0 | 마지막 성공 이후 `update_failed` 횟수. 성공하면 0 |

`/update` 핸들러가 성공·실패 때 이 세 컬럼을 갱신한다.

외래 키 변경: `acme_accounts.domain_id`와 `txt_records.account_id`를 `ON DELETE CASCADE`로 바꾼다. 도메인 삭제 시 계정과 TXT 기록도 함께 지운다(감사 로그는 텍스트라 남는다).

## 판정 규칙

### IP(접속 주소)별

| 코드 | 상태 | 조건 |
| --- | --- | --- |
| `CONNECT_FAILED` | critical | TCP 접속 또는 TLS 핸드셰이크 실패, 타임아웃(10초) |
| `CERT_EXPIRED` | critical | `not_after` < 지금 |
| `CERT_EXPIRES_SOON` | critical | 남은 기간 < 7일 |
| `CHAIN_INVALID` | critical | 시스템 신뢰 저장소로 체인 검증 실패(자체 서명 포함) |
| `HOSTNAME_MISMATCH` | critical | SAN이 `sni`(없으면 hostname)를 덮지 않음 |
| `RENEWAL_OVERDUE` | warning | 남은 기간 < 수명(`not_after − not_before`)의 1/3. 정상이면 이미 갱신됐을 시점 |
| `TLS_OUTDATED` | warning | 협상된 버전 < TLS 1.2 |
| `WEAK_KEY` | info | RSA < 2048비트, ECDSA < 256비트 |

`CERT_EXPIRED`이면 `CERT_EXPIRES_SOON`과 `RENEWAL_OVERDUE`는 붙이지 않는다. `CERT_EXPIRES_SOON`이면 `RENEWAL_OVERDUE`는 붙이지 않는다. 상태는 붙은 코드 중 가장 나쁜 것이고, 코드가 없으면 `ok`다.

### 대상별

대상 상태 = 각 접속 주소의 **최신** 결과 중 가장 나쁜 상태. 여기에 아래를 더한다.

| 코드 | 상태 | 조건 |
| --- | --- | --- |
| `SERIAL_MISMATCH` | warning | 최신 결과 중 인증서를 받은 주소가 둘 이상이고 serial이 서로 다름 |
| `NOT_CHECKED` | unknown | 아직 점검 결과가 없음 |

### 도메인별

도메인 상태 = 대상 상태 중 가장 나쁜 것. ACME가 켜져 있으면 아래를 더한다.

| 코드 | 상태 | 조건 |
| --- | --- | --- |
| `CNAME_MISSING` | warning | `cname_status = missing` |
| `CNAME_MISMATCH` | warning | `cname_status = mismatch` |
| `TXT_UPDATE_FAILING` | warning | `consecutive_failures ≥ 3` |

상태 순서: `critical` > `warning` > `info` > `ok` > `unknown`. 대상이 하나도 없고 ACME 경고도 없으면 `unknown`이다.

판정은 저장할 때 IP별로 계산해 `check_results`에 넣고, 대상·도메인 상태는 조회할 때 계산한다.

## 모니터링 엔진

### 점검 절차 (`monitor.Probe`)

1. 접속 주소를 정한다. `ips`가 있으면 각 IP의 `IP:port`, 없으면 `hostname:port` 하나.
2. `tls.Dialer`로 접속한다. SNI는 `sni`, 없으면 hostname. `InsecureSkipVerify: true`로 접속해 잘못된 인증서도 받아 둔다. 타임아웃 10초.
3. 받은 체인을 `x509.Verify`로 따로 검증한다. 루트는 시스템 저장소, 중간 인증서는 서버가 보낸 것. 호스트명 검증은 `leaf.VerifyHostname(sni 또는 hostname)`.
4. 결과 필드를 채우고 `Evaluate`로 코드와 상태를 계산한다.

### 실행

| 작업 | 실행 주체 | 주기 |
| --- | --- | --- |
| 전체 TLS 점검 | worker | `CHECK_INTERVAL`(기본 `6h`, 최소 `5m`). 시작 직후 한 번 실행. 동시 접속 최대 8 |
| 대상 하나 즉시 점검 | api (`POST /api/targets/{id}/check`) | 요청 시. 결과를 저장하고 돌려준다 |
| ACME CNAME 확인 | worker (TLS 점검과 같은 주기), api (`POST /api/domains/{id}/acme/check`) | |
| 점검 기록 정리 | worker | 하루 한 번. 90일보다 오래된 `check_results` 삭제 |
| TXT 정리 | worker | 기존과 같이 하루 한 번 |

### CNAME 확인 (`cnamecheck`)

`DNS_RESOLVER`(기본 `1.1.1.1:53`)에 `_acme-challenge.<도메인>` CNAME을 직접 질의한다. 사내 DNS가 아니라 Let's Encrypt가 보는 공개 DNS에 맞추기 위해서다.

| 결과 | 조건 |
| --- | --- |
| `ok` | CNAME 값(끝 점 제거, 소문자)이 `<subdomain>.<CHALLENGE_ZONE>`과 같음 |
| `missing` | NXDOMAIN 또는 CNAME 없음 |
| `mismatch` | 다른 값 |
| `error` | 질의 실패·타임아웃(5초) |

구현은 `net.Resolver`에 `Dial`을 지정해 그 리졸버로만 묻는다(외부 DNS 라이브러리 없이).

## API

모든 응답은 JSON이고, 오류는 `{"error":"<코드>"}`이다. 시각은 RFC 3339 UTC.

### 인증

- 조회(`GET`)는 인증 없이 허용한다. 단 `GET /api/audit`는 관리자만.
- 쓰기는 관리자만: 유효한 세션 쿠키 또는 `Authorization: Bearer <ADMIN_KEY>`.
- 쓰기 요청은 `Content-Type: application/json`만 받는다(본문이 없는 요청도 헤더 필요). 다른 출처의 폼 위조를 막는다.
- 실패 응답: 관리자 권한이 없으면 401 `unauthorized`(세션 없음·만료·키 불일치를 구분하지 않음), JSON이 아닌 쓰기는 415 `unsupported_media_type`, 로그인 실패 한도 초과는 429 `too_many_attempts`.

### 세션 (`internal/session`)

- `POST /api/session` `{"key":"..."}` → 200, 쿠키 `sslchk_session` 발급. 키가 틀리면 401 `unauthorized`. 실패는 기존 관리자 인증과 같은 IP별 제한(5번, 이후 12초에 1번).
- `GET /api/session` → `{"admin":true|false}`
- `DELETE /api/session` → 쿠키 삭제.
- 쿠키 값: `<만료 유닉스초>.<HMAC-SHA256(서명키, 만료)>`를 base64url. 서명키 = `SHA-256("sslchk-session:" + ADMIN_KEY)`. 수명 12시간. `HttpOnly`, `Secure`, `SameSite=Strict`, `Path=/`.
- 서버에 세션을 저장하지 않는다. **`ADMIN_KEY`를 바꾸면 모든 세션이 무효**가 된다. 로그아웃은 쿠키만 지운다(이미 복사된 쿠키는 만료까지 유효). 이 한계를 문서에 적는다.

### 조회

`GET /api/summary`

```json
{"domains":12,"critical":1,"warning":2,"info":0,"ok":8,"unknown":1,"checked_at":"2026-10-07T03:00:00Z"}
```

`GET /api/domains?status=warning` (status 필터 선택)

```json
{"domains":[{
  "id":3,"fqdn":"a.com","memo":"",
  "status":"warning","reasons":["RENEWAL_OVERDUE"],
  "earliest_not_after":"2026-10-20T00:00:00Z","days_left":13,
  "targets":2,"acme":{"enabled":true,"cname_status":"ok"},
  "last_checked_at":"2026-10-07T03:00:00Z"
}]}
```

`GET /api/domains/{id}`

```json
{
  "id":3,"fqdn":"a.com","memo":"","created_at":"...",
  "status":"warning","reasons":["SERIAL_MISMATCH"],
  "targets":[{
    "id":7,"hostname":"www.a.com","port":443,"sni":"","ips":["10.0.1.10","10.0.1.11"],"memo":"",
    "status":"warning","reasons":["SERIAL_MISMATCH"],
    "results":[{"address":"10.0.1.10:443","checked_at":"...","status":"ok","reasons":[],
                "tls_version":"TLS 1.3","cipher":"...","subject_cn":"a.com","sans":["a.com","*.a.com"],
                "issuer":"R11","serial":"04ab...","not_before":"...","not_after":"...",
                "key_alg":"ECDSA","key_bits":256,"chain_ok":true,"hostname_ok":true,"error":""}]
  }],
  "acme":{
    "enabled":true,"username":"<uuid>","subdomain":"<uuid>","fulldomain":"<uuid>.acme.example.com",
    "cname_record":"_acme-challenge.a.com. CNAME <uuid>.acme.example.com.",
    "allowfrom":["10.0.1.0/24"],
    "cname_status":"ok","cname_value":"<uuid>.acme.example.com","cname_checked_at":"...",
    "last_update_at":"...","last_failure_at":null,"consecutive_failures":0
  }
}
```

ACME가 꺼져 있으면 `"acme":{"enabled":false}`. 키·키 해시는 어떤 조회 응답에도 넣지 않는다.

`GET /api/targets/{id}/results?limit=50` (최대 500) → `{"results":[...]}` 최신순.

`GET /api/audit?limit=100&before_id=123` (관리자, 최대 500) → `{"entries":[{"id":..,"at":..,"actor":..,"action":..,"target":..,"source_ip":..,"detail":..}]}` 최신순.

`GET /api/config` → 화면과 llms.txt가 쓰는 공개 설정: `{"challenge_zone":"acme.example.com","check_interval":"6h0m0s","public_url":"https://..."}`.

### 쓰기 (관리자)

| 요청 | 본문 | 응답 |
| --- | --- | --- |
| `POST /api/domains` | `{"fqdn":"a.com","memo":""}` | 201 도메인 상세. 중복 409 `domain_exists`, 형식 오류 400 `invalid_domain` |
| `PATCH /api/domains/{id}` | `{"memo":"..."}` | 200 |
| `DELETE /api/domains/{id}` | | 204. ACME가 켜져 있으면 끈 뒤(TXT 삭제) 도메인·대상·결과·계정을 지운다 |
| `POST /api/domains/{id}/targets` | `{"hostname":"www.a.com","port":443,"sni":"","ips":["10.0.1.10"],"memo":""}` | 201 대상. 중복 409 `target_exists`, 형식 오류 400 `invalid_target` |
| `PATCH /api/targets/{id}` | 위 필드 중 일부 | 200 |
| `DELETE /api/targets/{id}` | | 204 |
| `POST /api/targets/{id}/check` | | 200 이번 점검 결과 목록 |
| `POST /api/domains/{id}/acme` | `{"allowfrom":["10.0.1.0/24"]}` | 201 **일회성 자격 증명**(아래). 이미 켜짐 409 `acme_enabled` |
| `PATCH /api/domains/{id}/acme` | `{"allowfrom":[...]}` | 200 ACME 상태 |
| `POST /api/domains/{id}/acme/rotate` | | 200 일회성 자격 증명. 서브도메인·username 유지, 키만 교체. 옛 키는 즉시 무효 |
| `POST /api/domains/{id}/acme/check` | | 200 CNAME 확인 결과 |
| `DELETE /api/domains/{id}/acme` | | 204. 계정 폐기, 챌린지 TXT 삭제. 다시 켜면 서브도메인이 새로 생겨 CNAME을 다시 걸어야 한다 |

일회성 자격 증명 응답:

```json
{
  "username":"<uuid>","password":"<40자>","subdomain":"<uuid>",
  "fulldomain":"<uuid>.acme.example.com",
  "cname_record":"_acme-challenge.a.com. CNAME <uuid>.acme.example.com.",
  "allowfrom":["10.0.1.0/24"],
  "acme_sh":"export ACMEDNS_BASE_URL=\"https://...\"\nexport ACMEDNS_USERNAME=...\n..."
}
```

`Cache-Control: no-store`. `acme_sh`는 환경변수 설정, `--issue --dns dns_acmedns -d a.com -d '*.a.com'`, `--install-cert` 예시를 담는다(비밀번호 포함). 이 응답 외에는 비밀번호를 다시 얻을 방법이 없다.

모든 쓰기는 `audit_logs`에 남긴다(actor `admin`, source_ip, 대상).

### 기존 경로

| 경로 | 변화 |
| --- | --- |
| `POST /update` | 그대로. 성공·실패 시 `acme_accounts`의 `last_update_at`·`last_failure_at`·`consecutive_failures`를 갱신 |
| `POST /register` | 호환용으로 유지. 도메인이 없으면 만들고 ACME를 켠다. 활성 계정이 있으면 409 `account_exists` |
| `GET /health` | 그대로 |

### 공개 주소 (`PUBLIC_URL`)

일회성 자격 증명의 `ACMEDNS_BASE_URL`과 llms.txt의 주소는 `PUBLIC_URL`을 쓴다. 비어 있으면 요청에서 만든다: 신뢰 프록시(`TRUSTED_PROXIES`)에서 온 요청은 `X-Forwarded-Proto`·`X-Forwarded-Host`, 아니면 `Host`와 TLS 여부.

## 화면 (`web/`)

React + Vite + TypeScript. 의존성은 `react`, `react-dom`, `react-router-dom`만. UI 라이브러리 없음, CSS는 직접 작성. 문구는 한국어.

| 경로 | 화면 |
| --- | --- |
| `/` | 대시보드: 상단 요약(긴급·경고·정보·정상·미점검 수), 상태 필터, 도메인 표(상태 배지, 도메인, 가장 이른 만료일·남은 일수, 대상 수, ACME 표시, 마지막 점검). 관리자에게 "도메인 추가" |
| `/domains/:id` | 도메인 상세: 상태와 사유, 대상별 표(접속 주소별 상태·만료일·발급자·serial 앞 8자·TLS 버전·마지막 점검), 대상 이력 펼침. ACME 패널(꺼짐/켜짐, CNAME 레코드 복사, CNAME 확인 결과, 마지막 TXT 갱신, 연속 실패). 관리자에게 메모 수정, 대상 추가·수정·삭제, 지금 점검, ACME 켜기·허용 IP 수정·키 재발급·CNAME 지금 확인·끄기, 도메인 삭제 |
| `/audit` | 감사 로그(관리자) |

- 상단 바: 제목, 관리자 로그인/로그아웃 버튼. 로그인은 키 입력 대화상자 → `POST /api/session`.
- 조회 전용 사용자에게는 쓰기 버튼을 보이지 않는다(서버도 거부한다).
- ACME 켜기·키 재발급 후 **일회성 창**: CNAME 레코드, 자격 증명, `acme_sh` 명령을 복사 버튼과 함께 보여 주고 "닫으면 다시 볼 수 없습니다"를 표시한다.
- 상태 색: critical 빨강, warning 주황, info 파랑, ok 초록, unknown 회색. 색만으로 구분하지 않게 배지에 글자도 쓴다.
- 데이터 갱신: 화면을 열 때와 쓰기 후에 다시 불러온다. 자동 새로고침은 60초.

### 제공 방식 (`internal/webui`)

- Vite 빌드 출력 경로를 `internal/webui/dist`로 두고 `go:embed all:dist`로 내장한다.
- 저장소에는 `internal/webui/dist/.keep`만 커밋하고, 나머지 빌드 결과는 `.gitignore`로 제외한다. `all:` 접두사 덕분에 `.keep`만 있어도 embed가 되므로 Node 없이 `go build`가 된다.
- 내장 파일에 `index.html`이 없으면 코드에 들어 있는 안내 페이지("화면이 빌드되지 않았습니다. `web/`에서 `npm run build`")를 대신 보여 준다. API와 llms.txt는 화면 빌드와 상관없이 동작한다.
- 라우팅: `/api/`, `/update`, `/register`, `/health`, `/llms.txt`가 아닌 `GET`은 내장 파일을 찾고, 없으면 `index.html`(SPA 폴백). `/assets/*`는 `Cache-Control: public, max-age=31536000, immutable`, `index.html`은 `no-cache`.

### Dockerfile

```
FROM node:24 AS web     → web/에서 npm ci && npm run build (출력: internal/webui/dist)
FROM golang:1.27 AS build → web 단계의 internal/webui/dist를 복사한 뒤 go build
FROM distroless/static  → 바이너리만
```

`.dockerignore`에서 `web/node_modules`를 제외한다.

## /llms.txt (`internal/llmstxt`)

`GET /llms.txt`, `text/plain; charset=utf-8`. [llms.txt 형식](https://llmstxt.org)(H1, 요약 인용문, 섹션)을 따르고, 템플릿에 이 서버의 실제 값(`PUBLIC_URL` 또는 요청에서 만든 주소, `CHALLENGE_ZONE`)을 채운다. **ACME 구성 방법이 중심**이다.

목차:

1. `# ssl-checker` + 한 줄 요약(인증서 모니터링 + acme-dns 호환 DNS-01 위임)
2. 동작 방식: CNAME 위임 흐름, 레코드 형태 `<uuid>.<CHALLENGE_ZONE>`, 값 2개 유지
3. 관리자 인증: `Authorization: Bearer <ADMIN_KEY>`(에이전트용), 키는 사람이 제공해야 함
4. ACME 구성 절차(에이전트가 그대로 따라 할 순서)
   1. `POST /api/domains`로 도메인 등록(이미 있으면 `GET /api/domains`로 id 찾기)
   2. `POST /api/domains/{id}/acme`로 ACME 켜기 → 응답의 `cname_record`, 자격 증명, `acme_sh`
   3. 도메인 DNS에 CNAME 등록 → `POST /api/domains/{id}/acme/check`로 `ok` 확인
   4. 대상 서버에서 acme.sh 3.1.4 이상 설치, 환경변수, 스테이징 발급 → 운영 발급 → `--install-cert`와 reload
   5. 확인: 도메인 상세의 `last_update_at`, 모니터링 대상 추가 후 `POST /api/targets/{id}/check`
5. 키 재발급·허용 IP 수정·ACME 끄기 API
6. 상태 판정 코드 표와 의미
7. 흔한 오류와 해결(401 forbidden 원인, CNAME mismatch, CAA, `dns_provider_error`)
8. 모니터링 API 요약(링크 수준)
9. 하지 말 것: 자격 증명을 로그·티켓에 남기기, 운영 CA로 테스트 반복

## 설정 (새 환경변수)

| 변수 | 서비스 | 기본값 | 설명 |
| --- | --- | --- | --- |
| `CHECK_INTERVAL` | worker | `6h` | TLS 점검·CNAME 확인 주기. Go duration. 최소 `5m` |
| `DNS_RESOLVER` | api, worker | `1.1.1.1:53` | CNAME 확인에 쓸 리졸버 `host:port` |
| `PUBLIC_URL` | api | 비움 | 화면·llms.txt·자격 증명에 쓸 외부 주소(예: `https://ssl-checker.example.com`). 비우면 요청에서 만든다 |

compose의 `x-common-env`에 `DNS_RESOLVER`, api에 `PUBLIC_URL`, worker에 `CHECK_INTERVAL`을 기본값과 함께 추가한다.

## 오류 처리

- 점검 실패(접속·핸드셰이크)는 결과로 저장한다(`CONNECT_FAILED`). 점검 실행 자체의 DB 오류는 로그로 남기고 다음 대상으로 넘어간다.
- worker 주기 작업 하나가 실패해도 다른 작업은 계속한다.
- "지금 점검"은 대상 하나만, 요청 타임아웃(30초) 안에서 실행한다.
- 존재하지 않는 id는 404 `not_found`.

## 테스트

| 대상 | 방법 |
| --- | --- |
| `monitor.Evaluate` | 표 기반 단위 테스트: 각 코드의 경계(7일, 1/3 수명, TLS 1.1/1.2, RSA 2048), 코드 우선순위 |
| `monitor.Probe` | `httptest.NewUnstartedServer` + 테스트 인증서로 실제 TLS: 정상(테스트 CA를 루트로 주입), 자체 서명, 호스트명 불일치, 만료, 접속 거부 |
| 대상 상태 | 같은 대상 두 주소에 다른 serial → `SERIAL_MISMATCH` |
| `cnamecheck` | 테스트용 UDP DNS 서버(표준 라이브러리로 최소 응답)로 ok/missing/mismatch/error |
| store | 실제 DB(`dbtest`)로 대상·결과·ACME 상태 저장, cascade 삭제, 보관 기간 정리 |
| adminapi | 실제 DB + memory 공급자: 비로그인 쓰기 401, Bearer·쿠키 둘 다 허용, JSON 아닌 쓰기 거부, 도메인→대상→점검→ACME 켜기→재발급(서브도메인 유지, 옛 키 401)→끄기 흐름, 감사 로그 기록 |
| session | 서명·만료·변조·`ADMIN_KEY` 변경 시 무효 |
| llmstxt | `CHALLENGE_ZONE`과 공개 주소가 채워지는지, 필수 섹션이 있는지 |
| webui | 내장 파일 제공, SPA 폴백, 예약 경로는 폴백하지 않음 |
| 화면 | `npm run build`(TypeScript 타입 검사 포함)가 Docker 빌드에서 통과 |
| smoke | 기존 + `/`(HTML), `/api/domains`, `/llms.txt` 응답 확인 |

## 문서

- `docs/guide/dashboard.md` 새로 작성: 화면 사용법, 관리자 로그인, 모니터링 대상 등록, ACME 켜기 흐름.
- `docs/guide/api.md`에 `/api` 추가, `docs/guide/configuration.md`에 새 변수 추가, `README.md`의 소개를 "모니터링 + ACME 위임"으로 바꾼다.
- `docs/guide/onboarding.md`는 화면·API 기준으로 계정 등록 단계를 바꾸고, curl `/register`는 대안으로 남긴다.

## 구현 계획 분할

1. **모니터링 엔진과 조회 API**: 마이그레이션 0002, `monitor`, `cnamecheck`, store 확장, worker 주기 작업(점검·CNAME·정리), `/update`의 ACME 상태 컬럼 갱신, 조회 API(`/api/summary`, `/api/domains`, `/api/domains/{id}`, `/api/targets/{id}/results`, `/api/config`), 새 환경변수.
2. **관리 API와 세션**: `session`, 쓰기 API 전부, `acmedns.Service`의 ACME 켜기·재발급·끄기, `/register` 재구성, `/api/audit`, `PUBLIC_URL`과 일회성 자격 증명의 `acme_sh`.
3. **대시보드와 llms.txt**: `web/` SPA, `webui` 내장·폴백, Dockerfile Node 단계, `llmstxt`, smoke 확장, 문서.

각 계획은 끝나면 동작하는 소프트웨어를 남긴다. 1번 뒤에는 API로 상태를 볼 수 있고, 2번 뒤에는 API로 전부 관리할 수 있다.

## 범위 밖

- 알림(Slack, 웹훅, 메일)
- 프로토콜·암호 스위트 정밀 스캔(sslyze)
- 예상 CA 설정과 발급자 규칙
- 사용자별 계정, SSO, 역할
- 서버 측 세션 저장·강제 로그아웃
- 대상 자동 발견(DNS의 모든 A 레코드 점검)
