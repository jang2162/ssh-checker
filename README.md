# ssl-checker

사내 도메인의 DNS-01 검증을 대행하는 acme-dns 호환 API. 각 서버의 acme.sh가 `dns_acmedns` 플러그인으로 `/update`를 호출하면, 앱이 챌린지 전용 존(`CHALLENGE_ZONE`)의 TXT를 DNS 공급자(기본 Cloudflare)에 기록한다. 인증서와 개인키는 각 서버가 직접 다룬다.

설계 문서: 「ssl-checker 구현 계획서 (acme-dns 호환 API 방식)」 https://claude.ai/code/artifact/4b4d7215-cacb-4e14-967d-ddb156a69e9d

## 명령

| 명령 | 역할 |
| --- | --- |
| `ssl-checker migrate` | DB 마이그레이션 후 종료 |
| `ssl-checker serve --listen :8080` | `/update`, `/register`(ADMIN_KEY), `/health` |
| `ssl-checker worker` | 하루 한 번 고아 TXT 정리 |
| `ssl-checker healthcheck` | compose 헬스체크용 `/health` 확인 |

## 개발

```bash
make db-up && make test && make db-down   # 단위·통합 테스트
./scripts/smoke.sh                        # 이미지 + compose 스택 smoke
./scripts/compat.sh                       # 원본 acme-dns와 응답 대조
```

## 배포

`deploy/compose.yaml`을 Komodo git 기반 Stack으로 등록한다. 비밀값은 Environment에 `[[SSLCHK_ADMIN_KEY]]`처럼 보간으로만 넣는다. 변수 목록과 운영 절차는 설계 문서의 「배포」 절을 따른다. 첫 실서버 적용 전 `docs/runbooks/gate1-staging.md`의 스테이징 발급을 통과해야 한다.

- `BIND_ADDR`, `IMAGE`, `APP_VERSION`, `POSTGRES_PASSWORD`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, `ADMIN_KEY`는 필수다. 비어 있으면 compose가 배포를 거부한다.
- `TRUSTED_PROXIES`에는 앞단 리버스 프록시 주소만 넣는다. 클라이언트 대역을 넣으면 `X-Forwarded-For`로 IP를 꾸며 allowfrom을 우회할 수 있다. 프록시가 없으면 비워 두고, 프록시 뒤인데 비워 두면 모든 클라이언트가 프록시 IP로 보여 allowfrom이 깨지고 관리자 인증 잠금도 함께 받는다.
- rootless Docker나 userland proxy에서는 게시 포트의 출발지가 브리지 게이트웨이로 보일 수 있다. `/32` allowfrom을 넣기 전에 `audit_logs.source_ip`에 찍히는 클라이언트 IP를 확인한다.
- `POSTGRES_PASSWORD`는 `DATABASE_URL`에 그대로 들어가므로 URL에 안전한 값을 쓴다(예: `openssl rand -hex 24`).
