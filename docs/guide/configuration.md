# 설정

ssl-checker는 모든 설정을 환경변수로 받습니다. `deploy/compose.yaml`은 서비스마다 필요한 변수만 넘깁니다. 그래서 `ADMIN_KEY`는 api에만 들어가고, migrate는 DB 주소만 받습니다.

## 환경변수

구분이 "필수"인 변수가 비어 있으면 compose가 시작을 거부합니다. "조건부"는 특정 설정일 때만 필요합니다.

| 변수 | 설명 | 사용 서비스 | 기본값·예시 | 구분 |
| --- | --- | --- | --- | --- |
| `BIND_ADDR` | api 포트를 열 호스트 IP. 인증서를 받을 서버들만 닿는 내부 인터페이스 주소를 넣습니다. 비우면 모든 인터페이스에 열리므로 compose가 막습니다. | api | `10.0.0.5` | 조건부 (`compose.ports.yaml`을 쓸 때 필수) |
| `API_PORT` | api를 열 호스트 포트 | api | `8080` | 선택 (`compose.ports.yaml`을 쓸 때만) |
| `CADDY_NETWORK` | Caddy 컨테이너가 붙어 있는 외부 Docker 네트워크 이름. api만 이 네트워크에 붙습니다. | api | `caddy` | 선택 (`compose.caddy.yaml`을 쓸 때만) |
| `TRUSTED_PROXIES` | `X-Forwarded-For`를 믿을 리버스 프록시 주소. 쉼표로 구분한 CIDR이나 IP. 아래 [네트워크와 프록시](#네트워크와-프록시)를 꼭 읽습니다. | api | 비움 | 선택 |
| `DNS_PROVIDER` | TXT를 기록할 공급자. `cloudflare` 또는 `memory`. `memory`는 테스트용이며 실제 DNS에 기록하지 않습니다. | api, worker | `cloudflare` | 필수 |
| `CHALLENGE_ZONE` | 챌린지 TXT를 둘 이름. 레코드는 `<uuid>.<CHALLENGE_ZONE>`으로 생깁니다. 전용 존 이름이나 기존 존의 하위 이름(예: `acme.example.com`)을 씁니다. 각 도메인의 CNAME이 가리키는 곳이라 처음에 확정합니다. 대소문자와 끝의 점은 무시합니다. | api, worker | `acme.example.com` | 필수 |
| `CF_API_TOKEN` | 전용 존의 `Zone > DNS > Edit` 권한만 가진 Cloudflare 토큰 | api, worker | | 조건부 (`cloudflare`일 때 필수) |
| `CF_ZONE_ID` | 챌린지 TXT를 둘 Cloudflare 존의 Zone ID. 비우면 `CHALLENGE_ZONE`을 존 이름으로 보고 조회합니다. **`CHALLENGE_ZONE`이 하위 이름이면 필수**이고, 존 단위 토큰은 조회 권한이 없을 수 있으므로 항상 넣는 것을 권장합니다. | api, worker | | 조건부 |
| `TXT_TTL` | 챌린지 TXT의 TTL(초, 1~86400). Cloudflare는 60 미만이면 60으로 올려 기록합니다. | api, worker | `60` | 선택 |
| `CHECK_INTERVAL` | 인증서 점검과 ACME CNAME 확인 주기. Go duration 형식이고 최소 `5m`입니다. api는 `/api/config`에 보여 주기만 합니다. | api, worker | `6h` | 선택 |
| `DNS_RESOLVER` | ACME CNAME 확인에 쓸 리졸버 `host:port`. Let's Encrypt가 보는 공개 DNS 기준으로 확인하려고 사내 DNS 대신 공개 리졸버를 씁니다. 포트 없는 IP는 53번으로 봅니다. | api, worker | `1.1.1.1:53` | 선택 |
| `PUBLIC_URL` | 클라이언트가 api에 닿는 외부 주소. `http(s)://host[:port]` 형식이며 경로, 쿼리, 프래그먼트는 붙일 수 없습니다. 다른 형식이면 api가 시작하지 않습니다. 화면과 자격 증명 안내의 `ACMEDNS_BASE_URL`에 씁니다. 비우면 요청에서 만듭니다(신뢰 프록시의 `X-Forwarded-Proto`·`X-Forwarded-Host` 포함). | api | `https://ssl-checker.example.com` | 선택 |
| `ADMIN_KEY` | `/register`의 Bearer 키. 32바이트 이상이어야 api가 시작합니다. 관리 API의 Bearer 키이자 화면 로그인 키입니다. 바꾸면 모든 로그인 세션이 끊깁니다. | api | `openssl rand -hex 32` | 필수 |
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
| `ssl-checker serve --listen :8080` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, `ADMIN_KEY`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`, `TRUSTED_PROXIES`, `CHECK_INTERVAL`, `DNS_RESOLVER`, `PUBLIC_URL`) |
| `ssl-checker worker` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`, `CHECK_INTERVAL`, `DNS_RESOLVER`) |
| `ssl-checker healthcheck --url http://127.0.0.1:8080/health` | 없음 |

설정이 잘못되면 프로세스가 문제 있는 변수 이름을 모두 출력하고 종료합니다.

## api를 여는 방법

`compose.yaml`만으로는 api를 바깥에 열지 않습니다. 둘 중 하나를 골라 compose 파일을 하나 더 붙입니다.

| 방법 | 붙일 파일 | 필요한 변수 | 클라이언트가 부르는 주소 |
| --- | --- | --- | --- |
| 호스트 포트로 직접 열기 | `compose.ports.yaml` | `BIND_ADDR`, (`API_PORT`) | `http://<BIND_ADDR>:<API_PORT>` |
| Caddy 뒤에 두기 (포트 미공개) | `compose.caddy.yaml` | `CADDY_NETWORK`, `TRUSTED_PROXIES` | Caddy에 설정한 주소 (예: `https://ssl-checker.example.com`) |

어느 쪽이든 이 주소로 들어오는 호출은 세 가지입니다. 각 서버 acme.sh의 `/update`, 관리자의 `/register`, 모니터링의 `/health`입니다. 컨테이너 헬스체크는 컨테이너 안에서 돌기 때문에 포트가 필요 없습니다.

### Caddy 뒤에서 돌릴 때

Caddy가 같은 서버에서 컨테이너로 돌고, 다른 서비스들과 공유하는 Docker 네트워크(예: `caddy`)가 있다고 가정합니다.

1. **compose 파일**: `compose.yaml` + `compose.caddy.yaml`. api는 호스트 포트를 열지 않고 `CADDY_NETWORK`에 별칭 `ssl-checker-api`로 붙습니다. db·migrate·worker는 이 네트워크에 붙지 않습니다.
2. **Caddyfile**: [`deploy/Caddyfile.example`](../../deploy/Caddyfile.example)처럼 `reverse_proxy ssl-checker-api:8080`으로 넘깁니다. 인증 기관은 이 API를 호출하지 않으므로, `remote_ip`로 내부 대역만 통과시키고 나머지는 막습니다. **Caddy가 인터넷에 열려 있다면 이 제한은 꼭 둡니다.**
3. **`TRUSTED_PROXIES`에 Caddy의 주소를 넣습니다.** 비워 두면 모든 요청이 Caddy에서 온 것으로 보입니다. 그러면 `allowfrom`이 동작하지 않고, 인증 실패 제한도 모든 서버가 함께 받습니다. Caddy는 `X-Forwarded-For`에 실제 클라이언트 IP를 넣어 보내고, ssl-checker는 `TRUSTED_PROXIES`에서 온 요청에 한해 그 값을 씁니다.
   - **권장: Caddy 컨테이너 IP를 고정하고 그 IP 하나만 넣습니다.** Caddy의 compose에서 네트워크에 `ipv4_address`를 지정하면 됩니다.
   - 고정하지 않으면 Caddy 네트워크의 서브넷을 넣습니다. 서브넷은 `docker network inspect caddy --format '{{(index .IPAM.Config 0).Subnet}}'`로 확인합니다. 다만 이 경우 **같은 네트워크의 다른 컨테이너도 `X-Forwarded-For`로 IP를 꾸밀 수 있습니다.** 그 네트워크에 믿을 수 있는 컨테이너만 있을 때만 씁니다.
4. **HTTPS 주소를 씁니다.** 이제 클라이언트 서버의 `ACMEDNS_BASE_URL`은 Caddy 주소(예: `https://ssl-checker.example.com`)입니다.
   - 공인 도메인이면 Caddy가 인증서를 자동으로 받습니다.
   - `tls internal`처럼 사설 인증서를 쓰면, 각 서버가 Caddy의 루트 인증서를 신뢰해야 acme.sh(curl)가 접속합니다.
5. **확인**: 일부러 틀린 키로 `/update`를 한 번 보낸 뒤 감사 로그의 `source_ip`를 봅니다. **Caddy 컨테이너 IP가 아니라 호출한 서버의 IP**가 찍혀야 합니다.

Caddy 앞에 로드밸런서나 Cloudflare 프록시가 또 있다면, Caddy의 `trusted_proxies`도 설정해야 원래 클라이언트 IP가 전달됩니다.

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
| `ADMIN_KEY` | 관리 API로 `allowfrom`을 비우고(`PATCH …/acme`) 키를 재발급(`POST …/acme/rotate`)해, CNAME이 이미 걸린 모든 도메인의 쓸 수 있는 키를 얻을 수 있습니다. `a.com`/`*.a.com` 인증서를 발급받을 수 있다는 뜻입니다. 값을 바꿔도 공격자가 이미 얻은 키는 무효가 되지 않습니다. | 값을 바꾸고 재배포(로그인 세션도 모두 끊깁니다) → 감사 로그 점검 → 영향받은 도메인 키 재발급. [ADMIN_KEY가 새었을 때](operations.md#admin_key가-새었을-때) |
| `CF_API_TOKEN` | 챌린지 존의 TXT를 바꿀 수 있습니다. 서비스 도메인은 안전합니다. | Cloudflare에서 새 토큰 발급 → 값 교체 → 재배포 → 옛 토큰 삭제 |
| 계정 키(`/register` 응답의 `password`) | 그 도메인의 인증서를 발급받을 수 있습니다. | 키 재발급(CNAME은 그대로) ([운영](operations.md#계정-폐기와-키-재발급)) |
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
