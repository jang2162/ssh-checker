# 설정

ssl-checker는 모든 설정을 환경변수로 받습니다. `deploy/compose.yaml`은 서비스마다 필요한 변수만 넘깁니다. 그래서 `ADMIN_KEY`는 api에만 들어가고, migrate는 DB 주소만 받습니다.

## 환경변수

구분이 "필수"인 변수가 비어 있으면 compose가 시작을 거부합니다. "조건부"는 특정 설정일 때만 필요합니다.

| 변수 | 설명 | 사용 서비스 | 기본값·예시 | 구분 |
| --- | --- | --- | --- | --- |
| `IMAGE` | 실행할 이미지 이름. 로컬 빌드면 `ssl-checker`, 레지스트리면 전체 경로 | 전체 | `registry.example.com/ssl-checker` | 필수 |
| `APP_VERSION` | 이미지 태그. 업데이트할 때 이 값만 바꿔 재배포합니다. | 전체 | `1.0.0` | 필수 |
| `BIND_ADDR` | api 포트를 열 호스트 IP. 인증서를 받을 서버들만 닿는 내부 인터페이스 주소를 넣습니다. 비우면 모든 인터페이스에 열리므로 compose가 막습니다. | api | `10.0.0.5` | 필수 |
| `API_PORT` | api를 열 호스트 포트 | api | `8080` | 선택 |
| `TRUSTED_PROXIES` | `X-Forwarded-For`를 믿을 리버스 프록시 주소. 쉼표로 구분한 CIDR이나 IP. 아래 [네트워크와 프록시](#네트워크와-프록시)를 꼭 읽습니다. | api | 비움 | 선택 |
| `DNS_PROVIDER` | TXT를 기록할 공급자. `cloudflare` 또는 `memory`. `memory`는 테스트용이며 실제 DNS에 기록하지 않습니다. | api, worker | `cloudflare` | 필수 |
| `CHALLENGE_ZONE` | 챌린지 TXT를 둘 전용 존. 각 도메인의 CNAME이 가리키는 곳이라 처음에 확정합니다. 대소문자와 끝의 점은 무시합니다. | api, worker | `acme-chk.example` | 필수 |
| `CF_API_TOKEN` | 전용 존의 `Zone > DNS > Edit` 권한만 가진 Cloudflare 토큰 | api, worker | | 조건부 (`cloudflare`일 때 필수) |
| `CF_ZONE_ID` | 전용 존의 Zone ID. 비우면 존 이름으로 조회합니다. 존 단위 토큰은 조회 권한이 없을 수 있으므로 넣는 것을 권장합니다. | api, worker | | 선택 |
| `TXT_TTL` | 챌린지 TXT의 TTL(초, 1~86400). Cloudflare는 60 미만이면 60으로 올려 기록합니다. | api, worker | `60` | 선택 |
| `CHECK_INTERVAL` | TLS 점검 주기. 아직 구현되지 않은 모니터링 기능용이며 지금은 읽지 않습니다. | worker | `6h` | 선택 |
| `ADMIN_KEY` | `/register`의 Bearer 키. 32바이트 이상이어야 api가 시작합니다. | api | `openssl rand -hex 32` | 필수 |
| `POSTGRES_PASSWORD` | DB 비밀번호. compose가 이 값으로 `DATABASE_URL`을 만들어 넘기므로 URL에 그대로 쓸 수 있는 문자만 씁니다. | 전체 | `openssl rand -hex 24` | 필수 |

compose가 내부에서 만드는 변수도 있습니다. 직접 넣을 필요는 없습니다.

| 변수 | 설명 |
| --- | --- |
| `DATABASE_URL` | `postgres://sslchk:${POSTGRES_PASSWORD}@db:5432/sslchk`. compose 없이 바이너리를 직접 실행할 때는 이 값을 직접 넣습니다. |

### 바이너리를 직접 실행할 때

compose 없이 실행할 때는 역할마다 아래 변수를 넣습니다.

| 명령 | 필요한 변수 |
| --- | --- |
| `ssl-checker migrate` | `DATABASE_URL` |
| `ssl-checker serve --listen :8080` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, `ADMIN_KEY`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`, `TRUSTED_PROXIES`) |
| `ssl-checker worker` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`) |
| `ssl-checker healthcheck --url http://127.0.0.1:8080/health` | 없음 |

설정이 잘못되면 프로세스가 문제 있는 변수 이름을 모두 출력하고 종료합니다.

## 네트워크와 프록시

ssl-checker는 클라이언트 IP로 두 가지를 판단합니다. 하나는 계정의 `allowfrom`(허용 IP 대역)이고, 다른 하나는 IP별 인증 실패 제한입니다. 클라이언트 IP는 이렇게 정합니다.

- 직접 연결한 주소가 `TRUSTED_PROXIES`에 속하지 않으면, 그 주소가 곧 클라이언트 IP입니다. `X-Forwarded-For`는 무시합니다.
- `TRUSTED_PROXIES`에 속하면, `X-Forwarded-For`를 오른쪽부터 읽어 처음 나오는 신뢰하지 않는 주소를 클라이언트 IP로 씁니다.

여기서 세 가지를 지켜야 합니다.

1. **`TRUSTED_PROXIES`에는 프록시 주소만 넣습니다.** 클라이언트가 있는 대역(예: `10.0.0.0/8` 전체)을 넣으면, 그 대역의 클라이언트가 `X-Forwarded-For`로 자기 IP를 마음대로 정해 `allowfrom`을 우회할 수 있습니다.
2. **앞단 프록시가 없으면 비워 둡니다.** 반대로 프록시 뒤에 두면서 비워 두면 모든 요청이 프록시 IP에서 온 것으로 보입니다. 그러면 `allowfrom`이 의미가 없어집니다. 인증 실패 제한도 모든 클라이언트가 함께 받아, 한 서버의 잘못된 키 때문에 다른 서버의 갱신까지 막힐 수 있습니다.
3. **rootless Docker나 userland proxy에서는 출발지 IP를 먼저 확인합니다.** 이런 환경에서는 게시한 포트로 들어온 요청의 출발지가 브리지 게이트웨이로 보일 수 있습니다. `/32` `allowfrom`을 넣기 전에 일부러 틀린 키로 `/update`를 한 번 보내고, 감사 로그의 `source_ip`에 어떤 주소가 찍히는지 봅니다. 감사 로그 조회 방법은 [운영](operations.md#감사-로그)에 있습니다.

API는 인터넷에 노출할 필요가 없습니다. 인증 기관(Let's Encrypt)은 DNS만 조회하고 ssl-checker를 직접 호출하지 않습니다. 인증서를 받을 서버들만 닿는 내부망이나 VPN에 두고, HTTP 평문이 내부망을 벗어나지 않게 합니다.

## 비밀값

| 비밀값 | 새었을 때 | 교체 방법 |
| --- | --- | --- |
| `ADMIN_KEY` | 새 계정을 만들 수 있습니다. 기존 계정의 키는 알 수 없습니다. | 값을 바꾸고 재배포 |
| `CF_API_TOKEN` | 챌린지 존의 TXT를 바꿀 수 있습니다. 서비스 도메인은 안전합니다. | Cloudflare에서 새 토큰 발급 → 값 교체 → 재배포 → 옛 토큰 삭제 |
| 계정 키(`/register` 응답의 `password`) | 그 도메인의 인증서를 발급받을 수 있습니다. | 계정 폐기 후 재등록 ([운영](operations.md#계정-폐기와-키-재발급)) |
| `POSTGRES_PASSWORD` | DB에 접근할 수 있습니다. 포트를 열지 않으므로 같은 Docker 네트워크 안에서만 해당합니다. | DB 비밀번호 변경과 함께 교체 |

- `deploy/.env`는 권한 600으로 두고 저장소에 커밋하지 않습니다. `.gitignore`에 들어 있습니다.
- ssl-checker는 비밀값을 로그나 응답에 남기지 않습니다. 계정 키는 bcrypt 해시로만 저장합니다. 평문은 등록 응답에 한 번만 나옵니다.

## 요청 제한

값은 코드에 고정돼 있습니다.

| 대상 | 기준 | 한도 | 초과 시 |
| --- | --- | --- | --- |
| `/update` 성공 요청 | 계정 | 10번 연속, 이후 6초에 1번 | 429 `rate_limited` |
| `/update` 인증 실패 | 클라이언트 IP | 20번, 이후 3초에 1번 | 429 `too_many_attempts` |
| `/register` 관리자 키 실패 | 클라이언트 IP | 5번, 이후 12초에 1번 | 429 `too_many_attempts` |

acme.sh는 인증서 하나를 발급할 때 도메인 이름마다 한 번씩 `/update`를 부릅니다. 그래서 `a.com`과 `*.a.com`을 함께 받으면 2번입니다. 일반적인 사용에서는 한도에 걸리지 않습니다.
