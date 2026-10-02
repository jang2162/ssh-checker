# 게이트 1: Let's Encrypt 스테이징 호환 테스트

통과 기준: Let's Encrypt **스테이징**에서 `dns_acmedns`로 테스트 도메인과 그 와일드카드를 한 인증서로 발급한다. 실서버 적용은 이 게이트를 통과한 뒤에만 한다.

## 준비물

- 챌린지 전용 존(예: `acme-chk.net`, 이름은 운영 전에 확정). Cloudflare에 존을 추가하고 등록기관에서 네임서버를 Cloudflare로 위임해 둔다.
- 그 존의 `Zone > DNS > Edit` 권한만 가진 API 토큰. 클라이언트 IP 필터에 앱 서버 IP를 넣는다.
- DNS를 직접 바꿀 수 있는 테스트 도메인(예: `test.a.com`). 운영 도메인을 쓰지 않는다.
- acme.sh 3.1.4 이상을 설치할 리눅스 머신 1대(앱 API에 사내망으로 닿아야 한다).

## 절차

1. 앱 배포: `DNS_PROVIDER=cloudflare`, `CHALLENGE_ZONE=<존>`, `CF_API_TOKEN=<토큰>`(필요하면 `CF_ZONE_ID`)으로 Stack을 배포한다. `curl http://<앱>:8080/health`가 `{"db":"ok","provider":"ok"}`인지 확인한다.
2. 계정 등록 (ADMIN_KEY가 셸 히스토리에 남지 않게 입력으로 받는다):
   ```bash
   read -rs ADMIN_KEY    # 붙여넣고 Enter. 화면에 표시되지 않는다
   curl -s -X POST http://<앱>:8080/register \
     -H "Authorization: Bearer $ADMIN_KEY" \
     -d '{"domain":"test.a.com","allowfrom":["<테스트 머신 IP>/32"]}'
   unset ADMIN_KEY
   ```
   응답에 일회용 `password`가 들어 있다. 다시 볼 수 없고 비밀이므로 시크릿 저장소나 바로 다음 단계에만 넣고, 티켓·채팅에는 붙이지 않는다. `username`, `subdomain`, `fulldomain`도 기록한다.
   `/register`와 `/update`는 사내망/VPN 안의 평문 HTTP다. 테스트 계정에는 허용하지만, 운영은 설계대로 내부 전용으로만 둔다.
3. CNAME 등록: 테스트 도메인 DNS에 `_acme-challenge.test.a.com. CNAME <fulldomain>.`을 넣고 확인한다.
   ```bash
   dig +short CNAME _acme-challenge.test.a.com @1.1.1.1   # <fulldomain>. 이 나와야 한다
   ```
4. 발급 (테스트 머신):
   ```bash
   curl -fsSL https://get.acme.sh | sh -s email=ops@company.example
   ~/.acme.sh/acme.sh --version          # 3.1.4 이상
   export ACMEDNS_BASE_URL="http://<앱>:8080"
   export ACMEDNS_USERNAME="<username>"
   read -rs ACMEDNS_PASSWORD && export ACMEDNS_PASSWORD   # 비밀번호는 입력으로 받는다
   export ACMEDNS_SUBDOMAIN="<subdomain>"
   ~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d test.a.com -d '*.test.a.com'
   ```
5. 확인:
   - acme.sh 출력에 `Cert success.`가 있다.
   - `openssl x509 -in ~/.acme.sh/test.a.com_ecc/test.a.com.cer -noout -issuer -ext subjectAltName`의 발급자가 스테이징 CA이고, SAN에 `test.a.com`과 `*.test.a.com`이 모두 있다.
   - DB의 `audit_logs`에 `action='update'` 행이 2개 이상 있고 `update_failed`가 없다.
   - Cloudflare 존의 `<subdomain>.<존>` TXT가 2개다.
6. 기록: 날짜, acme.sh 버전, 앱 버전, 결과를 설계 문서의 「일정과 단계별 산출물」 옆에 남긴다.

## 정리

```bash
~/.acme.sh/acme.sh --remove -d test.a.com --ecc
sed -i '/^SAVED_ACMEDNS_/d' ~/.acme.sh/account.conf   # acme.sh가 저장한 자격 증명 삭제
unset ACMEDNS_BASE_URL ACMEDNS_USERNAME ACMEDNS_PASSWORD ACMEDNS_SUBDOMAIN
```

테스트 계정은 게이트가 끝나면 반드시 폐기한다. 폐기는 2단계의 관리 API(`DELETE /api/domains/{id}`)가 생기면 그것으로 한다. 그 전에는 `UPDATE acme_accounts SET revoked_at = now() WHERE subdomain = '<subdomain>'`로 폐기하고, 다음 worker 정리 때 TXT가 지워지는지 확인한다.

## 실패 시

| 증상 | 확인할 것 |
| --- | --- |
| acme.sh가 `/update` 응답을 실패로 봄 | `audit_logs`의 `update_denied` 사유(`invalid_credentials`, `ip_unauthorized`, `subdomain_mismatch`) |
| `update_failed` + `config` | 토큰 권한, 존 ID, 토큰 IP 필터 |
| `update_failed` + `transient` | Cloudflare 장애·요청 제한. 잠시 후 재시도 |
| CA 검증 실패(TXT 못 찾음) | CNAME 대상 오타, 챌린지 존 네임서버 위임 |
