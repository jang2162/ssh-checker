# 설치와 배포

[English](installation.md) | **한국어**

이 문서는 ssl-checker를 Docker Compose로 띄우는 데까지 다룹니다. 도메인을 연결하고 인증서를 발급하는 방법은 [도메인 연결과 인증서 발급](onboarding.ko.md)에 있습니다.

## 1. 준비물

| 항목 | 설명 |
| --- | --- |
| 서버 1대 | Docker와 Docker Compose v2가 설치된 리눅스. 인증서를 받을 서버들이 이 서버의 api에 닿아야 합니다(호스트 포트 또는 Caddy 경유). |
| 챌린지 TXT를 둘 이름 | 아래 두 방식 중 하나. 예: 전용 도메인 `acme-chk.example`, 기존 존의 하위 이름 `acme.example.com` |
| Cloudflare 계정 | 챌린지 TXT를 둘 존이 Cloudflare에 있어야 합니다. 무료 플랜이면 충분합니다. |
| Cloudflare API 토큰 | 그 존의 `Zone > DNS > Edit` 권한**만** 가진 토큰. 다른 서비스(Caddy 등)와 같은 토큰을 쓰지 않습니다. |
| 인증서를 받을 도메인 | `_acme-challenge` CNAME을 한 번 등록할 수 있으면 됩니다. 그 도메인의 DNS API는 필요 없습니다. |

### 챌린지 TXT를 둘 이름 정하기

레코드는 `<uuid>.<CHALLENGE_ZONE>`으로 생기고, 각 도메인의 `_acme-challenge` CNAME이 이 이름을 가리킵니다. 나중에 바꾸면 모든 도메인의 CNAME을 다시 등록해야 하므로 처음에 확정합니다.

| 방식 | `CHALLENGE_ZONE` 예 | 장점 | 주의 |
| --- | --- | --- | --- |
| **전용 도메인 (권장)** | `acme-chk.example` | 토큰 권한이 이 존에만 걸려, 토큰이 새도 피해가 챌린지 TXT에서 멈춥니다. | 도메인을 하나 새로 사서 Cloudflare 존으로 추가해야 합니다. |
| 기존 존의 하위 이름 | `acme.example.com` (존은 `example.com`) | 새 도메인이 필요 없습니다. 레코드가 `acme.` 아래에만 생겨 존이 어지럽지 않습니다. | 토큰이 `example.com` 존 전체를 편집할 수 있게 되므로, 토큰이 새면 서비스 레코드도 바뀔 수 있습니다. **`CF_ZONE_ID`가 필수**입니다. |

Cloudflare 토큰은 레코드 단위가 아니라 존 단위로만 권한을 줄 수 있습니다. 그래서 피해 범위를 줄이려면 전용 도메인이 낫습니다. 하위 존을 따로 분리하는 기능은 Cloudflare Enterprise 플랜에서만 됩니다. 어느 방식이든 ssl-checker는 `<소문자 UUID>.<CHALLENGE_ZONE>` 이름만 만들고 지우며, 존의 다른 레코드는 건드리지 않습니다.

## 2. Cloudflare 준비

1. **전용 도메인을 쓴다면** 대시보드 첫 화면의 **Add a domain**으로 존을 추가합니다.
   - DNS 레코드 가져오기는 건너뛰고, 플랜은 Free를 고릅니다.
   - 등록기관에서 네임서버를 Cloudflare가 알려 준 두 값으로 바꿉니다. 존 상태가 **Active**가 될 때까지 기다립니다.
   - 레코드는 하나도 만들 필요가 없습니다. 기존 존의 하위 이름을 쓴다면 이 단계는 건너뜁니다.
2. **My Profile → API Tokens → Create Token → Create Custom Token**에서 토큰을 만듭니다.
   - Permissions: `Zone` / `DNS` / `Edit`
   - Zone Resources: `Include` / `Specific zone` / 챌린지 TXT를 둘 존 하나
   - Client IP Address Filtering: ssl-checker 서버의 공인 출구 IP(권장)
3. 그 존의 **Overview** 화면 오른쪽 API 섹션에서 **Zone ID**를 복사해 `CF_ZONE_ID`에 넣습니다. 존 단위 토큰은 존 목록을 조회하지 못할 수 있으므로 항상 넣는 것을 권장합니다.

## 3. 저장소 받기

공식 이미지는 배포하지 않습니다. compose가 저장소의 `Dockerfile`로 이미지를 직접 빌드하므로 저장소만 받으면 됩니다.

```bash
git clone <이 저장소> ssl-checker && cd ssl-checker
```

이미지는 distroless 기반입니다. 셸과 curl이 없고, 컨테이너 헬스체크는 `ssl-checker healthcheck` 하위 명령으로 합니다.

## 4. 환경변수 작성

```bash
cp deploy/production.env.example deploy/.env
chmod 600 deploy/.env
```

`deploy/.env`를 열어 값을 채웁니다. 최소한 아래 값은 넣어야 합니다. 비어 있으면 compose가 시작을 거부합니다.

| 변수 | 넣을 값 |
| --- | --- |
| `BIND_ADDR` | 호스트 포트로 열 때: API를 열 호스트 IP. 인증서를 받을 서버들만 닿는 내부 주소 |
| `CADDY_NETWORK`, `TRUSTED_PROXIES` | Caddy 뒤에 둘 때: Caddy의 Docker 네트워크 이름과 Caddy 주소 |
| `DNS_PROVIDER` | `cloudflare` |
| `CHALLENGE_ZONE` | 챌린지 TXT를 둘 이름. 전용 존 이름, 또는 기존 존의 하위 이름(예: `acme.example.com`) |
| `CF_API_TOKEN`, `CF_ZONE_ID` | 2단계에서 만든 토큰과 Zone ID |
| `ADMIN_KEY` | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` |

변수마다의 의미와 프록시 뒤에서 쓸 때의 주의점은 [설정](configuration.ko.md)에 있습니다.

## 5. 기동

api를 여는 방법에 따라 compose 파일을 하나 더 붙입니다. 자세한 내용은 [설정 → api를 여는 방법](configuration.ko.md#api를-여는-방법)에 있습니다.

```bash
# Caddy 뒤에 둘 때 (호스트 포트를 열지 않음)
docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.caddy.yaml --env-file deploy/.env up -d

# 호스트 포트로 직접 열 때
docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.ports.yaml --env-file deploy/.env up -d
```

처음 실행할 때 이미지를 빌드하므로 몇 분 걸립니다. 이후 `ps`·`logs` 같은 명령에도 같은 `-f` 목록을 붙입니다. 서비스 네 개가 뜹니다.

| 서비스 | 역할 |
| --- | --- |
| `db` | PostgreSQL 17. 데이터는 `pgdata` 볼륨에 둡니다. 포트를 외부에 열지 않습니다. |
| `migrate` | 이미지를 빌드하고(`ssl-checker:local`), DB 마이그레이션을 적용한 뒤 종료합니다. 성공해야 api와 worker가 시작합니다. |
| `api` | `/acme/*`(인증서를 받는 서버용 API와 스크립트), `/api/*`, 화면, `/register`, `/health`. 옛 `/update`도 남아 있습니다. `BIND_ADDR:API_PORT`로 열리거나, Caddy 네트워크에서 `ssl-checker-api:8080`으로 닿습니다. |
| `worker` | `CHECK_INTERVAL`마다 TLS 점검과 ACME CNAME 점검을 돌리고, 하루에 한 번 고아 TXT를 정리하며 90일이 지난 점검 기록을 지웁니다. 작업 중복을 막기 위해 1개만 띄웁니다. |

`migrate`가 `exited (0)`이고 `api`가 `healthy`면 정상입니다.

## 6. 첫 기동 확인

인증서를 받을 서버에서 `/health`를 호출합니다.

```bash
curl http://<BIND_ADDR>:<API_PORT>/health          # 호스트 포트로 열었을 때
curl https://ssl-checker.example.com/health        # Caddy 뒤일 때 (Caddy에 설정한 주소)
# {"db":"ok","provider":"ok"}
```

| 응답 | 의미 |
| --- | --- |
| `provider":"ok"` | Cloudflare 토큰과 존 접근이 정상 |
| `provider":"config"` | 토큰 권한, Zone ID, 토큰의 IP 필터를 확인합니다. |
| `provider":"transient"` | Cloudflare 응답 지연이나 요청 제한. 1분 뒤 다시 확인합니다. |
| `db":"error"` | `db` 컨테이너와 `POSTGRES_PASSWORD`를 확인합니다. |
| 연결 안 됨 | 호스트 포트면 `BIND_ADDR`, 방화벽, 서버 간 네트워크를 확인합니다. Caddy 뒤면 Caddyfile의 `reverse_proxy ssl-checker-api:8080`, `remote_ip` 허용 대역, api가 `CADDY_NETWORK`에 붙었는지(`docker network inspect <네트워크>`)를 확인합니다. |

`provider`가 `ok`가 될 때까지는 도메인 연결을 시작하지 않습니다.

브라우저로 `<앱>/`을 열어 [대시보드](dashboard.ko.md)가 보이는지도 확인합니다. 이미지를 빌드할 때 화면(Node)도 함께 빌드하므로 처음 빌드는 npm 패키지를 받느라 조금 더 걸립니다.

## Komodo로 배포 (저장소 모드)

Komodo Stack의 Git 저장소 모드를 쓰면 위의 3~5단계를 Komodo가 대신합니다. 배포할 때마다 이 저장소를 받아 compose로 띄우고, 이미지는 compose가 저장소의 Dockerfile로 빌드합니다. 레지스트리와 서버 접속이 필요 없습니다. 설정 전체는 [`deploy/komodo/ssl-checker.toml`](../../deploy/komodo/ssl-checker.toml)에 있습니다.

### 1. 비밀값 등록

Komodo **Settings → Variables**에서 아래 세 값을 만들고 **Secret**을 켭니다. Secret을 켜면 로그에 값이 가려지고 관리자가 아닌 사용자는 볼 수 없습니다.

| 이름 | 값 |
| --- | --- |
| `SSLCHK_ADMIN_KEY` | `openssl rand -hex 32` |
| `SSLCHK_CF_API_TOKEN` | Cloudflare 토큰 |
| `SSLCHK_POSTGRES_PASSWORD` | `openssl rand -hex 24` |

비밀값을 Komodo Core에 두고 싶지 않다면 배포 서버의 Periphery 설정 파일 `[secrets]`에 같은 이름으로 넣어도 됩니다. Periphery 시크릿은 Core를 거쳐 전송되지 않습니다.

### 2. Stack 만들기

UI에서 **Stacks → New Stack**을 누르고 아래처럼 채웁니다. 항목 이름은 Komodo v2.3 UI 기준입니다.

| UI 항목 | 값 |
| --- | --- |
| Server | 배포할 서버 |
| Choose Mode | **Git Repo**. "Files On Server"를 고르면 저장소를 받지 않아 `compose.yaml`을 찾지 못합니다. |
| Source: Git Provider / Repo / Branch | 이 저장소를 둔 Git 서버 / `<조직>/<저장소>` / `main` (예: `git.example.com` / `ops/ssl-checker` / `main`. GitHub이면 `github.com`) |
| Source: Git Account | 비공개 저장소면 Komodo에 등록한 Git 계정 |
| Files: Run Directory | `deploy` (비우면 저장소 루트에서 찾아 실패합니다) |
| Files: File Paths | Caddy 뒤: `compose.yaml`, `compose.caddy.yaml` 두 줄. 호스트 포트로 열 때는 두 번째 줄을 `compose.ports.yaml`로 바꿉니다. 비우면 `compose.yaml` 하나만 써서 api가 어디에도 열리지 않습니다. |
| Ignore Services | `migrate` (정상 종료하는 서비스라 상태 점검에서 제외) |
| Build Images: Pre Build Images | 선택. 꺼도 `compose.yaml`의 `pull_policy: build` 때문에 Deploy할 때 이미지를 빌드합니다. 켜면 Deploy 전에 `docker compose build`를 먼저 실행합니다. Swarm Stack에서는 보이지 않습니다. |
| Environment | `deploy/komodo/ssl-checker.toml`의 `environment` 블록. `<...>` 값을 채웁니다. 이 블록은 Caddy 뒤 구성 기준입니다. 호스트 포트로 열 때는 `CADDY_NETWORK` 대신 `BIND_ADDR`와 `API_PORT`를 넣습니다. |

Environment에서 비밀값은 반드시 `ADMIN_KEY=[[SSLCHK_ADMIN_KEY]]`처럼 보간으로만 넣습니다. Komodo는 이 내용을 `deploy/.env`로 써서 `--env-file`로 넘깁니다.

Resource Sync를 쓴다면 UI 입력 대신 `deploy/komodo/ssl-checker.toml`을 sync의 `resource_path`에 넣고, `<...>` 값은 그 파일을 복사한 사본에서 채웁니다.

### 3. 배포와 확인

Stack 화면에서 **Deploy**를 누릅니다. Komodo가 저장소를 받아 이미지를 빌드하고 `migrate` → `api`·`worker` 순으로 띄웁니다. 서비스 상태가 `api` healthy, `worker` running이 되면 위의 [6. 첫 기동 확인](#6-첫-기동-확인)을 합니다.

### 업데이트

`main`에 새 커밋이 올라오면 Stack에서 **Deploy**를 다시 누릅니다. 최신 커밋을 받아 다시 빌드하고 컨테이너를 교체합니다. 푸시할 때마다 자동으로 배포하려면, Stack 설정의 **Webhook URL**을 복사해 GitLab 프로젝트 **Settings → Webhooks**에 Push events로 등록합니다.

## 다음 단계

[도메인 연결과 인증서 발급](onboarding.ko.md)으로 첫 도메인을 연결합니다. 처음에는 Let's Encrypt **스테이징**으로 발급해 보는 것을 권장합니다.
