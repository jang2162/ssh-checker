# 운영

아래 명령은 [설치](installation.md)대로 `deploy/.env`와 프로젝트 이름 `ssl-checker`로 띄웠다고 가정합니다. 편의를 위해 별칭을 둡니다. 호스트 포트로 열었다면 `compose.caddy.yaml`을 `compose.ports.yaml`로 바꿉니다. Komodo로 배포했다면 Komodo UI에서 같은 작업을 하거나, 서버의 Stack 디렉터리에서 실행합니다.

```bash
alias dc='docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.caddy.yaml --env-file deploy/.env'
alias psql-sslchk='dc exec db psql -U sslchk -d sslchk'
```

## 업데이트

```bash
git pull
dc up -d --build
```

compose가 새 소스로 이미지를 다시 빌드합니다. `migrate`가 먼저 새 마이그레이션을 적용하고, 성공해야 api와 worker가 새 버전으로 시작합니다. api와 worker가 분리돼 있어, api를 재시작하는 동안에도 이미 발급된 인증서에는 영향이 없습니다. 갱신 중이던 서버는 acme.sh가 다음 cron 실행 때 재시도합니다.

## 백업과 복원

DB에는 도메인, 계정(키 해시), TXT 값, 감사 로그가 있습니다. 하루 한 번 백업해 다른 저장소에 보관합니다.

```bash
dc exec -T db pg_dump -U sslchk -d sslchk --format=custom > sslchk-$(date +%F).dump
```

복원:

```bash
dc stop api worker
dc exec -T db pg_restore -U sslchk -d sslchk --clean --if-exists < sslchk-2026-10-03.dump
dc start api worker
```

DB를 잃으면 모든 계정이 사라집니다. 그러면 도메인마다 재등록하고 CNAME과 서버 자격 증명을 다시 설정해야 합니다.

## 비밀값 교체

| 대상 | 절차 |
| --- | --- |
| `ADMIN_KEY` | `deploy/.env` 값 변경 → `dc up -d api` |
| `CF_API_TOKEN` | Cloudflare에서 새 토큰 발급 → `deploy/.env` 값 변경 → `dc up -d api worker` → `/health`가 `provider: ok`인지 확인 → 옛 토큰 삭제 |
| 계정 키 | 아래 [계정 폐기와 키 재발급](#계정-폐기와-키-재발급) |

## 계정 폐기와 키 재발급

서버를 폐기했거나 담당자가 바뀌었거나 키가 새었을 때 계정을 폐기합니다. 폐기한 계정의 키로 갱신을 시도하면 `401`이 납니다. 지금은 관리 API가 없으므로 SQL로 처리합니다.

```bash
psql-sslchk -c "SELECT d.fqdn, a.subdomain, a.created_at FROM acme_accounts a JOIN domains d ON d.id = a.domain_id WHERE a.revoked_at IS NULL ORDER BY d.fqdn;"
psql-sslchk -c "UPDATE acme_accounts SET revoked_at = now() WHERE subdomain = '<subdomain>' AND revoked_at IS NULL;"
dc restart worker     # 정리 작업을 바로 돌려 챌린지 TXT를 지운다
```

키를 다시 받으려면 폐기한 뒤 같은 도메인을 [다시 등록](onboarding.md#1-계정-등록)합니다. 재등록하면 `subdomain`이 바뀝니다. 따라서 다음을 모두 다시 해야 합니다.

1. `_acme-challenge.<도메인>` CNAME을 새 `fulldomain`으로 바꿉니다.
2. 각 서버에서 새 자격 증명으로 `--issue`를 다시 실행합니다. 옛 자격 증명은 `~/.acme.sh/<도메인>_ecc/<도메인>.conf`에 남아 있으므로 새 값이 덮어씁니다.

도메인 자체를 더 쓰지 않는다면 폐기한 뒤 `_acme-challenge` CNAME도 지웁니다.

## 정리 작업

worker는 시작할 때 한 번, 그 뒤 24시간마다 챌린지 존을 DB와 대조합니다.

- 활성 계정이 없는 `<uuid>.<존>` TXT를 지웁니다(폐기한 계정, 모르는 이름).
- 활성 계정의 TXT가 DB와 다르면 DB 값으로 다시 맞춥니다.
- 이름이 소문자 UUID가 아닌 레코드는 건드리지 않습니다. 그래서 챌린지 존에 다른 레코드가 있어도 안전합니다. 다만 챌린지 존은 이 용도로만 쓰는 것을 권장합니다.
- 진행 중인 `/update`와 겹쳐도 계정 단위 잠금을 거치므로 발급 중인 값을 지우지 않습니다.

결과는 worker 로그(`dc logs worker`)와 감사 로그의 `txt_cleanup` 행으로 확인합니다.

## 감사 로그

`audit_logs` 테이블에 모든 등록, 갱신, 거부, 정리가 남습니다.

```bash
psql-sslchk -c "SELECT at, actor, action, target, source_ip, detail FROM audit_logs ORDER BY at DESC LIMIT 30;"
```

| `action` | 의미 | `detail` |
| --- | --- | --- |
| `register` | 계정 등록 (`actor`=`admin`) | `subdomain=<uuid>` |
| `update` | TXT 갱신 성공 | |
| `update_denied` | `/update` 거부 | `invalid_credentials`, `ip_unauthorized`, `subdomain_mismatch`, `bad_txt`, `rate_limited` |
| `update_failed` | DNS 공급자나 DB 오류 | `transient: …` 또는 `config: …` |
| `txt_cleanup` | worker 정리 (`actor`=`worker`) | `deleted`, `repaired` |

관리자 키 인증 실패와 잠금은 감사 로그가 아니라 api 로그(`dc logs api`)에 `admin auth failed`, `admin auth locked out`로 남습니다. 키 값은 기록하지 않습니다.

감사 로그는 자동으로 지우지 않습니다. 필요하면 보존 기간을 정해 주기적으로 지웁니다.

```bash
psql-sslchk -c "DELETE FROM audit_logs WHERE at < now() - interval '1 year';"
```

## 상태 확인

```bash
curl -s <앱>/health                             # {"db":"ok","provider":"ok"}
dc ps                                           # api가 healthy인지
dc logs --since 24h api worker                  # JSON 로그
```

같은 계정에서 `update_failed`가 이어지면 다음 갱신이 실패할 수 있다는 신호입니다. 알림 기능은 아직 없으므로 감사 로그를 정기적으로 확인합니다.

```bash
psql-sslchk -c "SELECT target, count(*), max(at) FROM audit_logs WHERE action = 'update_failed' AND at > now() - interval '7 days' GROUP BY target;"
```

## 문제 해결

| 증상 | 확인할 것 |
| --- | --- |
| compose가 `required variable … is missing a value`로 멈춤 | `deploy/.env`에 그 변수가 비어 있습니다. [설정](configuration.md) 참고 |
| `api`가 시작 직후 종료 | `dc logs api`. 설정 오류면 문제 있는 변수 이름이 나옵니다(예: `ADMIN_KEY must be at least 32 bytes`). |
| `/health`가 `provider: config` | 토큰 권한(`Zone > DNS > Edit`), `CF_ZONE_ID`, 토큰의 IP 필터 |
| `/health`가 `provider: transient` | Cloudflare 응답 지연·요청 제한. 1분 뒤 다시 확인 |
| acme.sh가 `/update`를 실패로 봄 (401) | 감사 로그의 `update_denied` 사유. `ip_unauthorized`면 `source_ip`와 계정의 `allowfrom`을 비교합니다. |
| 401인데 감사 로그에 해당 계정 행이 없음 | `X-Api-User`가 틀렸거나 다른 계정입니다. `actor`에 보낸 값이 그대로 남습니다. |
| 429 `too_many_attempts` | 이 IP에서 인증 실패가 쌓였습니다. 잘못된 키를 쓰는 서버를 고치고 잠시 기다립니다. |
| `update_failed` + `config` | 토큰 권한, Zone ID, 토큰 IP 필터 |
| `update_failed` + `transient` | Cloudflare 장애·요청 제한. acme.sh가 다음 실행 때 재시도합니다. |
| CA 검증 실패 (TXT를 찾지 못함) | CNAME 대상 오타, 챌린지 존의 네임서버 위임, `dig TXT <fulldomain> @1.1.1.1`로 값이 보이는지 |
| CA가 CAA 때문에 거부 | 도메인의 CAA 레코드에 `letsencrypt.org` 추가 |
| 발급은 됐는데 웹서버가 옛 인증서를 씀 | `--install-cert`의 `--reloadcmd`가 실행 권한이 있는지, 웹서버 설정의 인증서 경로 |

## 제거

```bash
dc down          # 컨테이너만 내림. 데이터는 pgdata 볼륨에 남는다
dc down -v       # 데이터까지 삭제
```

제거하기 전에 각 도메인의 `_acme-challenge` CNAME을 지우거나, 다른 DNS-01 방식으로 옮겨야 다음 갱신이 실패하지 않습니다.
