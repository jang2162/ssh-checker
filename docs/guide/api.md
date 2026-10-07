# API 레퍼런스

모든 요청과 응답 본문은 JSON입니다. 오류는 원본 acme-dns처럼 `{"error": "<코드>"}` 형태로 돌려줍니다. 요청 본문은 4KB까지 받습니다.

| 메서드 | 경로 | 호출 주체 | 인증 |
| --- | --- | --- | --- |
| `POST` | `/update` | 각 서버의 acme.sh (`dns_acmedns`) | `X-Api-User`, `X-Api-Key` |
| `POST` | `/register` | 관리자 | `Authorization: Bearer <ADMIN_KEY>` |
| `GET` | `/health` | 모니터링, 컨테이너 헬스체크 | 없음 |

## POST /update

챌린지 TXT 값을 갱신합니다. 요청·응답 형식이 acme-dns의 `/update`와 같습니다. acme.sh `dns_acmedns`로 동작을 확인했습니다. lego나 certbot의 acme-dns 연동도 같은 형식을 쓰지만, 이들은 계정을 직접 `/register`하려 할 수 있습니다. 그럴 때는 미리 등록한 자격 증명을 쓰도록 설정해야 하며, 아직 검증하지 않았습니다.

```http
POST /update
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

상태는 `critical` > `warning` > `info` > `ok` > `unknown` 순으로 나쁩니다. 도메인 상태는 대상과 ACME 상태 중 가장 나쁜 것입니다.

| 코드 | 상태 | 뜻 |
| --- | --- | --- |
| `CONNECT_FAILED` | critical | 접속이나 TLS 핸드셰이크 실패(10초). Go TLS 클라이언트가 거부하는 아주 오래된 서버(RSA 키 교환만 지원, 1024비트 미만 RSA 키, SHA-1 서명 등)도 `CONNECT_FAILED`로 나오므로 `error` 필드를 확인합니다. |
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
curl -s '<앱>/api/domains?status=critical'
```

도메인과 모니터링 대상을 추가·수정하는 API는 다음 버전에서 추가됩니다.
