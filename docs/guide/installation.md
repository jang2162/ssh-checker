# 설치와 배포

이 문서는 ssl-checker를 Docker Compose로 띄우는 데까지 다룹니다. 도메인을 연결하고 인증서를 발급하는 방법은 [도메인 연결과 인증서 발급](onboarding.md)에 있습니다.

## 1. 준비물

| 항목 | 설명 |
| --- | --- |
| 서버 1대 | Docker와 Docker Compose v2가 설치된 리눅스. 인증서를 발급받을 서버들이 이 서버의 API 포트에 닿아야 합니다. |
| 챌린지 전용 도메인 | 챌린지 TXT만 두는 도메인(예: `acme-chk.example`). 서비스용 도메인과 분리합니다. 이유는 아래에 있습니다. |
| Cloudflare 계정 | 챌린지 전용 도메인을 Cloudflare 존으로 추가합니다. 무료 플랜이면 충분합니다. |
| Cloudflare API 토큰 | 그 존의 `Zone > DNS > Edit` 권한**만** 가진 토큰 |
| 인증서를 받을 도메인 | `_acme-challenge` CNAME을 한 번 등록할 수 있으면 됩니다. 그 도메인의 DNS API는 필요 없습니다. |

### 챌린지 도메인을 따로 두는 이유

Cloudflare API 토큰은 레코드 단위가 아니라 존 단위로만 권한을 줄 수 있습니다. 서비스 도메인과 같은 존을 쓰면, 토큰이 새었을 때 서비스 레코드까지 바뀔 수 있습니다. 전용 존을 두면 피해가 챌린지 TXT에서 멈춥니다.

이 도메인 이름은 각 도메인의 CNAME이 가리키는 곳입니다. 나중에 바꾸면 모든 도메인의 CNAME을 다시 등록해야 하므로 처음에 확정합니다.

## 2. Cloudflare 준비

1. Cloudflare에서 챌린지 전용 도메인을 존으로 추가합니다. 도메인 등록기관에서 네임서버를 Cloudflare가 알려 준 값으로 바꿉니다.
2. **My Profile → API Tokens → Create Token → Create Custom Token**에서 토큰을 만듭니다.
   - Permissions: `Zone` / `DNS` / `Edit`
   - Zone Resources: `Include` / `Specific zone` / 챌린지 전용 존
   - Client IP Address Filtering: ssl-checker 서버의 공인 출구 IP(권장)
3. 존 개요 화면 오른쪽의 **Zone ID**를 복사해 둡니다. 존 단위 토큰은 존 목록을 조회하지 못할 수 있으므로 `CF_ZONE_ID`로 직접 넣습니다.

## 3. 이미지 빌드

공식 이미지는 아직 배포하지 않습니다. 저장소에서 직접 빌드합니다.

```bash
git clone <이 저장소> ssl-checker && cd ssl-checker
docker build -t ssl-checker:1.0.0 .
```

서버 한 대에서만 쓴다면 이 로컬 이미지를 그대로 쓰면 됩니다. 여러 서버에 배포하거나 Komodo 같은 도구를 쓴다면 사내 레지스트리에 푸시합니다.

```bash
docker tag ssl-checker:1.0.0 registry.example.com/ssl-checker:1.0.0
docker push registry.example.com/ssl-checker:1.0.0
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
| `IMAGE`, `APP_VERSION` | 3단계에서 빌드한 이미지 이름과 태그 (예: `ssl-checker`, `1.0.0`) |
| `BIND_ADDR` | API를 열 호스트 IP. 인증서를 받을 서버들만 닿는 내부 주소 |
| `DNS_PROVIDER` | `cloudflare` |
| `CHALLENGE_ZONE` | 챌린지 전용 존 이름 |
| `CF_API_TOKEN`, `CF_ZONE_ID` | 2단계에서 만든 토큰과 Zone ID |
| `ADMIN_KEY` | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` |

변수마다의 의미와 프록시 뒤에서 쓸 때의 주의점은 [설정](configuration.md)에 있습니다.

## 5. 기동

```bash
docker compose -p ssl-checker -f deploy/compose.yaml --env-file deploy/.env up -d
docker compose -p ssl-checker -f deploy/compose.yaml --env-file deploy/.env ps
```

서비스 네 개가 뜹니다.

| 서비스 | 역할 |
| --- | --- |
| `db` | PostgreSQL 17. 데이터는 `pgdata` 볼륨에 둡니다. 포트를 외부에 열지 않습니다. |
| `migrate` | DB 마이그레이션을 적용하고 종료합니다. 성공해야 api와 worker가 시작합니다. |
| `api` | `/update`, `/register`, `/health`. `BIND_ADDR:API_PORT`로 열립니다. |
| `worker` | 고아 TXT 정리. 작업 중복을 막기 위해 1개만 띄웁니다. |

`migrate`가 `exited (0)`이고 `api`가 `healthy`면 정상입니다.

## 6. 첫 기동 확인

인증서를 받을 서버에서 `/health`를 호출합니다.

```bash
curl http://<BIND_ADDR>:<API_PORT>/health
# {"db":"ok","provider":"ok"}
```

| 응답 | 의미 |
| --- | --- |
| `provider":"ok"` | Cloudflare 토큰과 존 접근이 정상 |
| `provider":"config"` | 토큰 권한, Zone ID, 토큰의 IP 필터를 확인합니다. |
| `provider":"transient"` | Cloudflare 응답 지연이나 요청 제한. 1분 뒤 다시 확인합니다. |
| `db":"error"` | `db` 컨테이너와 `POSTGRES_PASSWORD`를 확인합니다. |
| 연결 안 됨 | `BIND_ADDR`, 방화벽, 서버 간 네트워크를 확인합니다. |

`provider`가 `ok`가 될 때까지는 도메인 연결을 시작하지 않습니다.

## Komodo로 배포할 때

`deploy/compose.yaml`을 git 기반 Stack으로 등록합니다. `deploy/.env` 대신 Stack의 **Environment**에 같은 변수를 넣습니다.

- 비밀값은 `ADMIN_KEY=[[SSLCHK_ADMIN_KEY]]`처럼 보간으로만 넣고, 실제 값은 배포 서버의 Periphery 설정 `[secrets]`나 시크릿 옵션을 켠 Komodo Variables에 둡니다.
- `migrate`는 정상 종료되는 서비스이므로 상태 점검 제외 서비스에 넣습니다.
- Build 리소스로 이 저장소의 `Dockerfile`을 빌드해 레지스트리에 푸시하고, `APP_VERSION`을 그 태그로 맞춥니다.

## 다음 단계

[도메인 연결과 인증서 발급](onboarding.md)으로 첫 도메인을 연결합니다. 처음에는 Let's Encrypt **스테이징**으로 발급해 보는 것을 권장합니다.
