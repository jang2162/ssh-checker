# ssl-checker

DNS API가 없는 도메인에서도 acme.sh로 Let's Encrypt 인증서를 자동 발급·갱신할 수 있게 해 주는 DNS-01 검증 대행 서버입니다. 와일드카드 인증서도 됩니다.

[acme-dns](https://github.com/joohoi/acme-dns)와 같은 API를 제공하므로, 각 서버는 acme.sh에 들어 있는 `dns_acmedns` 플러그인을 그대로 씁니다. 클라이언트 쪽에 따로 설치할 것은 없습니다. acme-dns와 달리 직접 DNS 서버를 띄우지 않고, 챌린지 TXT를 Cloudflare 같은 DNS 공급자 API로 기록합니다. 그래서 53번 포트를 열거나 NS를 위임할 필요가 없습니다.

## 동작 방식

```
 서버(acme.sh) ──(1) 발급 요청──▶ Let's Encrypt
      │                              │
      │(2) /update (TXT 값)          │(4) _acme-challenge.a.com 조회
      ▼                              ▼
 ssl-checker ──(3) TXT 기록──▶ <uuid>.acme-chk.net   ◀──CNAME── _acme-challenge.a.com
              (Cloudflare API)    (챌린지 전용 존)                  (a.com DNS, 최초 1회 등록)
```

1. 각 서버의 acme.sh가 Let's Encrypt에 발급을 요청하고 검증 토큰을 받습니다.
2. acme.sh가 계산한 TXT 값을 ssl-checker의 `/update`로 보냅니다.
3. ssl-checker가 챌린지 전용 존의 `<uuid>.<존>`에 TXT를 기록합니다.
4. Let's Encrypt가 `_acme-challenge.a.com`을 조회하고, CNAME을 따라가 TXT를 확인합니다.

도메인마다 `_acme-challenge` CNAME을 **한 번만** 등록하면 됩니다. 그 뒤로는 a.com의 DNS를 건드리지 않습니다. 인증서와 개인키는 각 서버의 acme.sh가 직접 만들고 보관합니다. ssl-checker는 개인키를 다루지 않습니다.

## 기능

- acme-dns 호환 `/update` API: 원본 acme-dns와 응답이 같은지 자동 테스트로 확인합니다.
- 도메인별 계정과 키: 키는 bcrypt 해시로만 저장하고, 계정마다 허용 IP 대역(`allowfrom`)을 지정할 수 있습니다.
- 값 2개 유지: `a.com`과 `*.a.com`을 한 인증서로 발급할 때 필요한 TXT 2개를 함께 유지합니다.
- DNS 공급자 인터페이스: 기본 구현체는 Cloudflare이고, 테스트용으로 메모리 구현체가 있습니다.
- 매일 고아 TXT 정리: 챌린지 존에서 `<uuid>.<존>` 형태의 레코드만 다룹니다.
- 감사 로그: 등록, 갱신 성공·실패·거부, 정리 작업을 모두 기록합니다.
- 요청 제한: 계정별 갱신 빈도, IP별 인증 실패 횟수, 관리자 키 실패 횟수를 제한합니다.

### 아직 없는 것

- 관리 화면. 관리 API는 있습니다([API](docs/guide/api.md)). 화면은 아직 없습니다.
- 인증서 만료 모니터링과 알림.

## 빠른 시작 (로컬에서 써 보기)

Docker만 있으면 됩니다. 메모리 공급자를 쓰므로 실제 DNS에는 아무것도 기록하지 않습니다.

```bash
git clone <이 저장소> ssl-checker && cd ssl-checker
./scripts/smoke.sh
```

`smoke.sh`는 이미지를 빌드하고 compose 스택을 띄웁니다. 그다음 계정 등록 → TXT 갱신 → 잘못된 키 거부 순으로 확인하고, 끝나면 스택을 내립니다. 마지막 줄에 `smoke OK`가 나오면 정상입니다.

## 문서

| 문서 | 내용 |
| --- | --- |
| [설치와 배포](docs/guide/installation.md) | 준비물, 이미지 빌드, docker compose 배포, 첫 기동 확인 |
| [설정](docs/guide/configuration.md) | 환경변수 전체 목록, 네트워크·프록시 설정, 비밀값 관리 |
| [도메인 연결과 인증서 발급](docs/guide/onboarding.md) | 계정 등록, CNAME 등록, acme.sh 발급·설치·갱신, 서버 추가 |
| [API 레퍼런스](docs/guide/api.md) | `/update`, `/register`, `/health`의 요청·응답·오류 코드 |
| [운영](docs/guide/operations.md) | 업데이트, 백업, 키 교체, 계정 폐기, 감사 로그, 문제 해결 |

## 명령

이미지 하나에 하위 명령 네 개가 들어 있습니다.

| 명령 | 역할 |
| --- | --- |
| `ssl-checker migrate` | DB 마이그레이션을 적용하고 종료 |
| `ssl-checker serve --listen :8080` | `/update`, `/register`, `/health` API |
| `ssl-checker worker` | 시작할 때 한 번, 그 뒤 24시간마다 고아 TXT 정리 |
| `ssl-checker healthcheck [--url URL]` | 컨테이너 헬스체크용 `/health` 확인 |

## 개발

Go 1.27 이상과 Docker가 필요합니다.

```bash
make db-up && make test && make db-down   # 단위·통합 테스트 (Postgres 컨테이너 사용)
./scripts/smoke.sh                        # 이미지 + compose 스택 smoke 테스트
./scripts/compat.sh                       # 원본 acme-dns와 /update 응답 대조
```

DB가 필요한 테스트는 `TEST_DATABASE_URL`이 없으면 건너뜁니다. `make test`가 이 변수를 넣어 줍니다.

## 라이선스

[MIT](LICENSE)
