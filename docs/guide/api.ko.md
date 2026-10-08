# API 레퍼런스

[English](api.md) | **한국어**

모든 요청과 응답 본문은 JSON입니다. 오류는 원본 acme-dns처럼 `{"error": "<코드>"}` 형태로 돌려줍니다. 요청 본문은 4KB까지 받습니다.

| 메서드 | 경로 | 호출 주체 | 인증 |
| --- | --- | --- | --- |
| `POST` | `/acme/update` (옛 `/update`) | 각 서버의 acme.sh (`dns_acmedns`) | `X-Api-User`, `X-Api-Key` |
| `POST` | `/acme/verify` (옛 `/verify`) | 세팅 스크립트(사전 점검) | `X-Api-User`, `X-Api-Key` |
| `GET` | `/acme/setup.sh` | 인증서를 받을 서버 | 없음 |
| `POST` | `/register` | 관리자 | `Authorization: Bearer <ADMIN_KEY>` |
| `GET` | `/health` | 모니터링, 컨테이너 헬스체크 | 없음 |

인증서를 받는 서버가 부르는 경로는 `/acme/*` 하나에 모여 있습니다(API는 `update`, `verify`, 스크립트는 `setup.sh`). 리버스 프록시에서 이 접두사만 바깥에 열면 되고, 나머지(`/api/*`, 화면, `/register`)는 내부망에 둘 수 있습니다. acme.sh에는 `ACMEDNS_BASE_URL=<주소>/acme`를 주면 `/acme/update`를 부릅니다(세팅 스크립트가 이렇게 정합니다). 루트의 `/update`, `/verify`는 기존 설정을 위해 같은 동작으로 남아 있습니다.

## POST /acme/update

챌린지 TXT 값을 갱신합니다. 요청·응답 형식이 acme-dns의 `/update`와 같습니다. acme.sh `dns_acmedns`로 동작을 확인했습니다. lego나 certbot의 acme-dns 연동도 같은 형식을 쓰지만, 이들은 계정을 직접 `/register`하려 할 수 있습니다. 그럴 때는 미리 등록한 자격 증명을 쓰도록 설정해야 하며, 아직 검증하지 않았습니다.

```http
POST /acme/update
X-Api-User: c36f50e8-4632-44f0-83fe-e070fef28a10
X-Api-Key: <등록 때 받은 password>
Content-Type: application/json

{"subdomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a", "txt": "<43자 검증 값>"}
```

성공하면 `200`과 `{"txt": "<저장한 값>"}`을 돌려줍니다.

처리 규칙:

- `txt`는 ACME DNS-01 검증 값입니다. 정확히 43자이고 `[A-Za-z0-9_-]`만 씁니다.
- 서브도메인마다 **최근 값 2개**를 유지합니다. 3번째 값이 오면 가장 오래된 값을 교체합니다. 이미 있는 값이 다시 오면 그대로 둡니다. acme.sh는 TXT 삭제를 호출하지 않으므로 이 규칙이 정리 역할도 합니다.
- DNS 공급자에 기록이 성공했을 때만 DB에 저장합니다. 공급자가 실패하면 아무것도 바뀌지 않고 5xx를 돌려주므로, acme.sh가 실패로 보고 재시도합니다.
- 같은 계정의 요청은 순서대로 처리합니다. `a.com`과 `*.a.com` 값이 동시에 와도 둘 다 남습니다.

| 상태 | `error` | 원인 |
| --- | --- | --- |
| 401 | `forbidden` | 사용자·키 불일치, 폐기된 계정, `allowfrom` 밖의 IP, 본문 `subdomain`이 계정과 다름, 본문 JSON 오류. 원본 acme-dns처럼 이유를 구분하지 않습니다. 이유는 감사 로그에 남습니다. |
| 400 | `bad_txt` | `txt` 형식 오류 |
| 429 | `rate_limited` | 계정별 갱신 한도 초과 (10번 연속, 이후 6초에 1번) |
| 429 | `too_many_attempts` | 이 IP의 인증 실패 한도 초과 (20번, 이후 3초에 1번). 이 응답은 감사 로그에 남지 않습니다. |
| 500 | `dns_provider_error` | DNS 공급자 기록 실패. 감사 로그 `detail`에 `transient`(재시도) 또는 `config`(설정 확인)가 남습니다. |
| 500 | `db_error` | DB 오류 |

## POST /register

도메인의 검증 계정을 만듭니다. 관리자 전용입니다.

```http
POST /register
Authorization: Bearer <ADMIN_KEY>
Content-Type: application/json

{"domain": "a.com", "allowfrom": ["10.0.1.0/24", "10.0.2.15"]}
```

| 필드 | 필수 | 설명 |
| --- | --- | --- |
| `domain` | 예 | 인증서를 받을 도메인. 소문자로 정규화하고 끝의 점은 지웁니다. 와일드카드는 받지 않습니다. |
| `allowfrom` | 아니오 | `/update`를 허용할 CIDR이나 IP 목록. IP는 `/32`(IPv6는 `/128`)로 바꿉니다. 비우면 모든 IP를 허용합니다. |

성공하면 `201`을 돌려줍니다. 응답에는 `Cache-Control: no-store`가 붙습니다.

```json
{
  "username": "c36f50e8-4632-44f0-83fe-e070fef28a10",
  "password": "<40자, 이때 한 번만 나옴>",
  "fulldomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example",
  "subdomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a",
  "allowfrom": ["10.0.1.0/24", "10.0.2.15/32"]
}
```

- `username`과 `subdomain`은 무작위 UUID입니다. 도메인 이름에서 계산하지 않습니다.
- `fulldomain`은 `_acme-challenge.<domain>` CNAME이 가리킬 대상입니다.

| 상태 | `error` | 원인 |
| --- | --- | --- |
| 400 | `malformed_json_payload` | 본문 JSON 오류 |
| 400 | `invalid_allowfrom_cidr` | `allowfrom` 항목 형식 오류 |
| 400 | `invalid_domain` | 도메인 형식 오류, 와일드카드 |
| 401 | `unauthorized` | `ADMIN_KEY` 불일치 |
| 409 | `account_exists` | 이미 활성 계정이 있는 도메인 |
| 429 | `too_many_attempts` | 이 IP의 관리자 키 실패 한도 초과 (5번, 이후 12초에 1번) |
| 500 | `db_error` | DB 오류 |

원본 acme-dns의 `/register`는 인증 없이 열려 있고 본문도 다릅니다. ssl-checker에서는 관리자 전용이며 `domain` 필드가 필수입니다.

## GET /health

DB 연결과 DNS 공급자 상태를 확인합니다.

```json
{"db": "ok", "provider": "ok"}
```

| 필드 | 값 |
| --- | --- |
| `db` | `ok`, `error` |
| `provider` | `ok`, `transient`(응답 지연·요청 제한), `config`(토큰·존 설정 오류) |

둘 다 `ok`면 `200`, 아니면 `503`을 돌려줍니다. 공급자 확인은 외부 API를 부르므로 결과를 1분 동안 재사용하고, 한 번에 3초까지만 기다립니다.

## 조회 API (`/api`)

인증서 점검 결과와 ACME 상태를 JSON으로 봅니다. 인증 없이 누구나 부를 수 있습니다. 시각은 RFC 3339 UTC입니다. 키와 키 해시는 어떤 응답에도 들어가지 않습니다.

| 요청 | 내용 |
| --- | --- |
| `GET /api/summary` | 도메인 수와 상태별 개수, 마지막 점검 시각 |
| `GET /api/domains` | 도메인 목록. `?status=critical`처럼 상태로 거를 수 있습니다 |
| `GET /api/domains/{id}` | 대상별·접속 주소별 최신 점검 결과와 ACME 상태 |
| `GET /api/targets/{id}/results?limit=50` | 대상의 점검 이력, 최신순. `limit` 최대 500 |
| `GET /api/config` | `challenge_zone`, `check_interval`(Go 시간 문자열, 예: `"6h0m0s"`), `public_url` |
| `GET /api/codes` | 판정 코드 표 `{"codes":[{"code","status","meaning"}]}`. 나쁜 상태부터 정렬됩니다. 화면과 `/llms.txt`가 같은 표를 씁니다 |

인증서를 받지 못한 점검 결과(접속 실패 등)에는 `not_before`·`not_after` 필드가 없습니다.

상태는 `critical` > `warning` > `info` > `ok` > `unknown` 순으로 나쁩니다. 도메인 상태는 대상과 ACME 상태 중 가장 나쁜 것입니다.

| 코드 | 상태 | 뜻 |
| --- | --- | --- |
| `CONNECT_FAILED` | critical | 접속이나 TLS 핸드셰이크 실패(10초). Go TLS 클라이언트가 거부하는 아주 오래된 서버(RSA 키 교환만 지원, 1024비트 미만 RSA 키, SHA-1 서명 등)도 `CONNECT_FAILED`로 나오므로 `error` 필드를 확인합니다. |
| `CERT_EXPIRED` | critical | 인증서 만료 |
| `CERT_EXPIRES_SOON` | critical | 만료까지 `min(7일, 수명의 1/3)` 미만. 90일짜리는 7일, 6일짜리 단기 인증서는 2일 |
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
curl -s '<앱>/api/domains?status=critical'
```

## 관리 API (`/api`, 관리자)

도메인·모니터링 대상을 관리하고 ACME를 켜고 끕니다. 관리자만 부를 수 있습니다.

### 인증

| 방법 | 쓰는 곳 |
| --- | --- |
| `Authorization: Bearer <ADMIN_KEY>` | 스크립트, 에이전트 |
| 세션 쿠키 `sslchk_session` (`POST /api/session`로 발급) | 대시보드 화면 |

- 관리자가 아니면 401 `unauthorized`입니다. 틀린 키(로그인·Bearer)는 IP별로 5번까지 받고, 그 뒤로는 12초에 1번만 받습니다(429 `too_many_attempts`). 잠긴 IP는 틀린 키 시도와 Bearer 요청이 막히지만, 로그인으로 받은 쿠키는 그대로 통과합니다.
- GET이 아닌 요청은 `Content-Type: application/json`이 있어야 합니다(본문이 없어도). 없으면 415 `unsupported_media_type`입니다. 모르는 필드가 있거나 본문이 64KiB를 넘으면 400 `malformed_json`입니다.
- `allowfrom`은 최대 64개이고, 넘으면 400 `invalid_allowfrom`입니다.
- 모든 쓰기는 감사 로그에 남습니다(`GET /api/audit`).

### 세션

| 요청 | 본문 | 응답 |
| --- | --- | --- |
| `POST /api/session` | `{"key":"<ADMIN_KEY>"}` | 200 `{"admin":true}` + 쿠키. 틀리면 401 |
| `GET /api/session` | | `{"admin":true|false}` (세션 쿠키만 봅니다. Bearer 키는 보고하지 않습니다) |
| `DELETE /api/session` | | 204, 쿠키 삭제 |

쿠키는 12시간 유효합니다. 서버에 세션을 저장하지 않으므로, **로그아웃해도 이미 복사된 쿠키는 만료까지 유효합니다.** 모든 세션을 끊으려면 `ADMIN_KEY`를 바꿉니다. 쿠키에는 `HttpOnly`, `SameSite=Strict`가 붙고, 공개 주소가 `http://`가 아니면 `Secure`도 붙습니다. 화면 로그인은 HTTPS(Caddy 뒤)에서 쓰는 것을 권장합니다.

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
  "setup": {
    "setup": {"id": "setup", "label": "세팅", "url": "https://.../acme/setup.sh", "cmd": "curl -fsSL https://.../acme/setup.sh -o setup.sh && bash setup.sh", "script": "#!/usr/bin/env bash\n..."},
    "servers": [{"id": "nginx", "label": "nginx", "config": "ssl_certificate ...;"}]
  },
  "rotated": false
}
```

`setup`은 스크립트 하나(`setup`)의 내려받기 주소, 실행 명령(`cmd`), 내용(`script`)과, 웹서버(`nginx`, `apache`, `caddy`)별 설정 예(`config`)입니다. 스크립트 하나가 acme.sh 설치 확인부터 웹서버 지정까지 모두 합니다. **스크립트에는 `password` 같은 비밀값이 없습니다.** 서버에서 실행할 때 `username`, `password`를 입력하거나 환경변수로 넘깁니다(`subdomain`은 `/acme/verify`가 알려 줍니다). 주소는 `PUBLIC_URL`(없으면 요청 주소) 기준입니다. 키 재발급(`rotate`) 응답은 `rotated: true`이고, 이미 인증서가 있어도 새 키가 도메인 설정에 저장되도록 서버에서 세팅 스크립트를 `FORCE=1`로 다시 실행해야 합니다(예: `FORCE=1 bash setup.sh`).

### 설치·세팅 스크립트

- `GET /acme/setup.sh`: 스크립트 파일(`text/plain`). 로그인 없이 받을 수 있고 비밀값이 없습니다. `curl -fsSL <주소>/acme/setup.sh -o setup.sh && bash setup.sh` 한 줄로 설정이 진행됩니다. `curl ... | bash`도 되며, 이때 입력은 `/dev/tty`에서 받습니다.
- `GET /api/setup`: 위 스크립트의 내용과 실행 명령, 웹서버 설정 예(JSON).

스크립트는 이 순서로 진행하고, 문제가 있으면 발급 전에 이유를 알려 주고 멈춥니다.

1. 자격 증명을 입력받아 `POST /acme/verify`로 **자격 증명과 이 서버의 IP가 허용 목록(`allowfrom`)에 있는지** 확인합니다.
2. acme.sh가 3.1.4 이상인지 확인하고, 없거나 낮으면 설치합니다(만료 알림 메일 주소를 물어봅니다).
3. 웹서버를 감지해 기본값으로 제안하고(`nginx`, `apache`, `caddy`), 실행 파일과 설정 디렉터리를 확인합니다. 감지되지 않거나 직접 reload하려면 `manual`을 골라 **reload 스크립트나 명령**(`RELOAD_CMD`)을 입력합니다. 경로로 준 스크립트는 실행할 수 있는지 확인합니다.
4. 도메인을 입력받아(`*.`는 빼고 `a.com`처럼 입력하며, 와일드카드 `*.a.com`은 항상 함께 발급합니다. `*.a.com`을 입력해도 `a.com`으로 봅니다) `_acme-challenge.<도메인>`의 **CNAME이 `/acme/verify`가 돌려준 `fulldomain`을 가리키는지** 공개 DNS(`DNS_RESOLVER`, 기본 `1.1.1.1`)로 확인합니다(`dig`, `host`, `nslookup`, 없으면 DNS over HTTPS). 없거나 다르면 등록할 레코드를 보여 주고, 터미널이면 등록한 뒤 다시 확인하고 아니면 멈춥니다. `SKIP_CNAME_CHECK=1`로 건너뜁니다.
5. 인증서 저장 경로(`CERT_DIR`, 기본 `/etc/ssl/<도메인>`)와 reload 명령을 정하고 발급해 설치합니다. 저장 디렉터리는 sudo 없이 만들거나 쓸 수 있으면 sudo를 쓰지 않습니다.
6. 웹서버 설정이 이 인증서를 이미 쓰는지 확인하고, 아니면 넣을 설정을 출력합니다.
7. reload 명령이 `sudo`를 쓰는데 이 계정이 sudo에 비밀번호가 필요하면 경고합니다. 자동 갱신(cron)은 비밀번호를 입력할 수 없어 reload가 실패하기 때문입니다.

환경변수로 미리 넘기면 묻지 않습니다: `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD`, `ACMEDNS_SUBDOMAIN`, `SSLCHK_URL`, `ACME_EMAIL`, `WEB_SERVER`, `DOMAIN`, `CERT_DIR`, `CERT_GROUP`(Caddy), `RELOAD_CMD`. 웹서버 설정 위치는 `WEB_CONF_DIR`, 실행 파일을 더 찾아볼 위치는 `WEB_SEARCH_DIRS`로 정하고, 실행 파일 확인은 `SKIP_WEB_CHECK=1`로 건너뜁니다. root가 아닌 계정은 reload 기본 명령과 디렉터리 생성에 `sudo`를 쓰고, root는 쓰지 않습니다(`id -u`로 판단하며 `SUDO_CMD`로 바꿉니다. 비우면 sudo 없음). reload에 sudo가 필요한 서버는 root 셸(`sudo -i`)에서 실행하거나, reload 명령만 비밀번호 없이 허용(sudoers NOPASSWD)하세요.

## POST /acme/verify

세팅 전에 쓰는 사전 점검입니다. `/acme/update`와 같은 인증과 허용 IP 검사를 하지만 TXT는 건드리지 않습니다.

| 응답 | 뜻 |
| --- | --- |
| 200 `{"ok":true,"source_ip":"<호출한 IP>","subdomain":"...","fulldomain":"..."}` | 자격 증명이 맞고 허용 IP입니다. `subdomain`은 acme.sh의 `ACMEDNS_SUBDOMAIN`이고, `fulldomain`은 도메인의 `_acme-challenge` CNAME이 가리켜야 할 주소입니다 |
| 401 `forbidden` | username이나 password가 틀렸습니다 |
| 403 `ip_not_allowed` + `source_ip` | 자격 증명은 맞지만 이 IP가 허용 목록에 없습니다 |
| 429 `too_many_attempts` | 이 IP의 인증 실패가 너무 많습니다 |

거부는 `/acme/update`와 같은 IP별 실패 횟수를 씁니다. 감사 로그에는 `verify_denied`로 남습니다.

### 감사 로그

`GET /api/audit?limit=100&before_id=<id>` (관리자, 최대 500) → `{"entries":[{"id","at","actor","action","target","source_ip","detail"}]}` 최신순. 다음 페이지는 마지막 항목의 `id`를 `before_id`로 넘깁니다. `before_id`가 숫자가 아니면 400 `invalid_before_id`입니다.

```bash
read -rs ADMIN_KEY
curl -s -X POST '<앱>/api/domains' -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"fqdn":"a.com"}'
```

## 화면과 `/llms.txt`

| 요청 | 내용 |
| --- | --- |
| `GET /`, `/domains/<id>`, `/audit` | 대시보드(SPA). 화면을 빌드하지 않은 바이너리는 503과 안내 페이지를 돌려줍니다 |
| `GET /llms.txt` | 에이전트가 ACME를 구성할 때 따라 할 절차(`text/plain`). 서버 주소와 `CHALLENGE_ZONE`이 채워져 있고 인증이 필요 없습니다 |

화면은 `/api/`, `/update`, `/register`, `/health`, `/llms.txt`를 가로채지 않습니다. 사용법은 [대시보드](dashboard.ko.md)에 있습니다.
