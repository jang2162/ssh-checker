# 도메인 연결과 인증서 발급

[English](onboarding.md) | **한국어**

`a.com`과 `*.a.com` 인증서를 nginx 서버에서 자동 발급·갱신하는 과정을 예로 듭니다. ssl-checker가 [설치](installation.ko.md)되어 있고, `/health`가 `{"db":"ok","provider":"ok"}`라고 가정합니다.

아래에서 `<앱>`은 클라이언트가 부르는 api 주소입니다. 호스트 포트로 열었다면 `http://<BIND_ADDR>:<API_PORT>`, Caddy 뒤라면 Caddy에 설정한 주소(예: `https://ssl-checker.example.com`)입니다. `<존>`은 `CHALLENGE_ZONE` 값입니다.

아래 API 호출은 [대시보드](dashboard.ko.md)에서도 할 수 있습니다. 도메인 상세의 `ACME 켜기`가 1단계, `CNAME 지금 확인`이 2단계의 확인에 해당하고, 켠 직후 나오는 창에 3단계 명령이 값이 채워진 채로 들어 있습니다. 에이전트에게 맡길 때는 `<앱>/llms.txt`를 알려 주세요.

## 전체 흐름

| 단계 | 누가 | 횟수 |
| --- | --- | --- |
| 1. 도메인 등록과 ACME 켜기 | 관리자 (`ADMIN_KEY` 보유) | 도메인당 1번 |
| 2. CNAME 등록 | 그 도메인의 DNS 관리자 | 도메인당 1번 |
| 3. 발급과 설치 | 각 서버 | 서버당 1번 |
| 4. 자동 갱신 | 각 서버의 acme.sh cron | 자동 |

## 1. 도메인 등록과 ACME 켜기

도메인 하나에 ACME 계정 하나를 만듭니다. 이 계정으로 `a.com`과 `*.a.com`을 함께 처리합니다. `*.a.com` 인증서가 `www.a.com`, `api.a.com` 같은 한 단계 아래 이름을 모두 덮습니다. 두 단계 아래(`*.dev.a.com`)가 필요하면 `dev.a.com`을 따로 등록합니다.

`ADMIN_KEY`가 셸 히스토리에 남지 않게 입력으로 받습니다.

```bash
read -rs ADMIN_KEY      # 붙여넣고 Enter. 화면에 표시되지 않는다
A='Authorization: Bearer '"$ADMIN_KEY"
J='Content-Type: application/json'

# 도메인 등록 (이미 있으면 409 domain_exists — GET <앱>/api/domains 에서 id를 찾는다)
curl -s -X POST <앱>/api/domains -H "$A" -H "$J" -d '{"fqdn":"a.com"}'
# 응답의 "id"로 ACME 켜기
curl -s -X POST <앱>/api/domains/<id>/acme -H "$A" -H "$J" \
  -d '{"allowfrom":["10.0.1.10/32","10.0.1.11/32"]}'
unset ADMIN_KEY A
```

- `allowfrom`: 이 계정으로 `/update`를 부를 수 있는 서버 IP나 CIDR 목록. 비우면 어디서든 부를 수 있습니다. 서버 IP를 알고 있다면 넣는 것을 권장합니다. 프록시 뒤나 rootless Docker라면 먼저 [출발지 IP를 확인](configuration.ko.md#네트워크와-프록시)합니다.

응답에는 `username`, `password`, `subdomain`, `fulldomain`, 등록할 CNAME 한 줄(`cname_record`), 그리고 3단계에서 서버가 실행할 명령(`setup`)이 들어 있습니다. 스크립트에는 비밀값이 없습니다.

`password`는 이때 한 번만 나오고 다시 볼 수 없습니다. 이 도메인의 인증서를 발급받을 수 있는 비밀이므로 시크릿 저장소에 바로 넣습니다. 티켓이나 채팅에 붙이지 않습니다. 잃어버리면 [키를 재발급](operations.ko.md#계정-폐기와-키-재발급)합니다. 서브도메인이 그대로라 CNAME은 다시 걸지 않아도 됩니다.

예전 방식인 `POST /register`(`{"domain":"a.com","allowfrom":[...]}`)도 그대로 동작합니다. 도메인이 없으면 만들고 ACME를 켭니다.

## 2. CNAME 등록

`a.com`의 DNS에 레코드 하나를 추가합니다. `a.com`의 DNS를 손대는 것은 이때 한 번뿐입니다.

```
_acme-challenge.a.com.  CNAME  <fulldomain>.
```

예: `_acme-challenge.a.com. CNAME 8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example.`

확인합니다. 등록 직후에는 리졸버 캐시 때문에 몇 분 걸릴 수 있습니다.

```bash
dig +short CNAME _acme-challenge.a.com @1.1.1.1
# 8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example.
```

ssl-checker도 공개 DNS로 확인합니다: `POST <앱>/api/domains/<id>/acme/check`의 `cname_status`가 `ok`면 됩니다(worker도 `CHECK_INTERVAL`마다 확인합니다).

`a.com`에 CAA 레코드가 있다면 `letsencrypt.org`가 허용 목록에 있어야 합니다.

```bash
dig +short CAA a.com
```

## 3. 발급과 설치

인증서를 쓸 서버에서 실행합니다. acme.sh는 **3.1.4 이상**을 씁니다. 그보다 낮은 버전은 ARI(갱신 시점 안내)를 지원하지 않습니다.

### 설치·세팅 스크립트로 한 번에

스크립트는 ssl-checker가 내려줍니다. 대시보드의 [이용 방법](dashboard.ko.md) 페이지나 ACME를 켤 때 나오는 창에서 명령을 복사합니다. 스크립트에는 비밀값이 없고, 실행하면 값을 물어봅니다. 인증서를 받을 서버에서 이 한 줄이면 됩니다.

```bash
curl -fsSL <앱>/acme/setup.sh -o setup.sh && bash setup.sh
```

스크립트는 이 순서로 진행하고, 문제가 있으면 발급 전에 이유를 알려 주고 멈춥니다.

1. **자격 증명과 허용 IP 확인.** ssl-checker 주소(내려받은 주소가 기본값), username, password(입력해도 보이지 않음)를 받아 확인합니다. subdomain은 계정에 묶인 값이라 서버가 알려 주므로 입력하지 않습니다. 이 서버의 IP가 허용 IP 목록(`allowfrom`)에 없으면 그 IP를 알려 주므로, 관리자가 [허용 목록에 추가](api.ko.md#post-acmeverify)한 뒤 다시 실행합니다.
2. **acme.sh 확인.** 3.1.4 이상이 설치되어 있는지 확인하고, 없거나 낮으면 설치합니다(만료 알림 메일 주소를 물어봅니다).
3. **웹서버 지정과 위치 확인.** 설치된 nginx, apache, caddy를 감지해 기본값으로 제안하고, 실행 파일과 설정 디렉터리를 확인합니다. **감지되지 않거나 직접 reload하려면 `manual`을 골라** reload 스크립트나 명령을 입력합니다(예: `/usr/local/bin/reload-web.sh`, `systemctl reload haproxy`). 경로로 준 스크립트는 실행할 수 있는지 확인합니다.
4. **도메인과 CNAME 확인.** 인증서를 받을 도메인(`*.도메인`도 함께 받음)을 입력하면, `_acme-challenge.<도메인>`의 CNAME이 ACME를 켤 때 안내한 주소를 가리키는지 공개 DNS(기본 1.1.1.1)로 조회해 확인합니다. 없거나 다르면 등록할 레코드(`_acme-challenge.a.com. CNAME <서브도메인>.<챌린지 존>.`)를 보여 주고, 등록한 뒤 Enter로 다시 확인합니다(DNS 전파에 몇 분 걸릴 수 있습니다). 터미널이 없으면 멈추므로 등록한 뒤 다시 실행합니다.
5. **인증서 저장 경로.** 저장 디렉터리(기본 `/etc/ssl/<도메인>`)와 설치 뒤 실행할 reload 명령을 정합니다. Caddy는 키 파일을 읽을 그룹도 묻습니다.
6. **발급과 설치.** 발급해 저장하고, 갱신될 때마다 reload하도록 등록합니다.
7. **웹서버 설정 확인.** 웹서버 설정이 이 인증서 경로를 이미 쓰는지 확인하고, 아니면 넣을 설정을 보여 줍니다.
8. **갱신 점검.** reload 명령이 `sudo`를 쓰는데 비밀번호가 필요하면 경고합니다([자동 갱신](#4-자동-갱신) 참고).

환경변수로 미리 넘기면 묻지 않습니다(터미널이 없는 자동화용). 기본값이 있는 값은 생략해도 됩니다.

| 값 | 환경변수 | 기본값 |
| --- | --- | --- |
| ssl-checker 주소 | `SSLCHK_URL` | 스크립트를 내려받은 주소 |
| username, password | `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD` | 없음 |
| subdomain | `ACMEDNS_SUBDOMAIN` | 서버가 알려 준 값. 서버 값이 우선합니다 |
| acme.sh 설치 시 메일 주소 | `ACME_EMAIL` | 없음(설치할 때만 필요) |
| 웹서버 | `WEB_SERVER` | 감지된 첫 웹서버. 없으면 `manual` (`nginx`, `apache`, `caddy`, `manual`) |
| 웹서버 설정 디렉터리 | `WEB_CONF_DIR` | 웹서버별 흔한 위치(`/etc/nginx` 등) |
| 웹서버 실행 파일 확인 건너뛰기 | `SKIP_WEB_CHECK=1` | 확인함 |
| 실행 파일을 더 찾아볼 위치 | `WEB_SEARCH_DIRS` | `/usr/sbin /usr/local/sbin /sbin /usr/local/bin /opt/homebrew/bin` |
| reload와 디렉터리 생성에 붙일 sudo | `SUDO_CMD` | root면 없음, 아니면 `sudo`. 비우면 sudo를 쓰지 않습니다 |
| 인증서를 받을 도메인 | `DOMAIN` | 없음. `*.` 없이 `example.com`처럼 입력합니다. 와일드카드는 항상 함께 발급되고, `*.example.com`을 입력해도 `example.com`으로 봅니다 |
| CNAME 확인용 공개 리졸버 | `DNS_RESOLVER` | `1.1.1.1` |
| CNAME 확인 건너뛰기 | `SKIP_CNAME_CHECK=1` | 확인함. 사내 DNS만 쓰는 등 공개 DNS로 볼 수 없을 때 |
| 인증서를 저장할 디렉터리 | `CERT_DIR` | `/etc/ssl/<도메인>` |
| 설치 뒤 실행할 reload 스크립트나 명령 | `RELOAD_CMD` | 웹서버별 reload. `manual`은 없음 |
| 키 파일을 읽을 그룹 (Caddy만) | `CERT_GROUP` | `caddy` |

이미 인증서가 있는 서버에서 다시 실행해도 안전합니다(`--issue`가 "아직 갱신할 때가 아님"으로 끝나도 설치까지 진행합니다). 키를 재발급한 뒤에는 `FORCE=1 bash setup.sh`로 실행해야 새 키가 저장됩니다. `curl ... | bash`로 실행해도 되며, 이때 입력은 터미널(`/dev/tty`)에서 받습니다.

스크립트는 `~/.acme.sh/acme.sh --issue --dns dns_acmedns`(와일드카드 포함)와 `--install-cert --reloadcmd`를 실행하는 것과 같습니다. 직접 하려면 `ACMEDNS_BASE_URL=<앱>/acme`와 `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD`, `ACMEDNS_SUBDOMAIN`을 환경변수로 주고 이 명령을 실행합니다.

### 먼저 스테이징으로 확인

처음 연결하는 도메인은 Let's Encrypt 스테이징으로 먼저 발급해 봅니다. 운영 CA의 발급 횟수 제한에 걸리지 않습니다. 세팅 스크립트를 실행하기 전에 합니다.

```bash
ACMEDNS_BASE_URL=<앱>/acme ACMEDNS_USERNAME=<username> ACMEDNS_SUBDOMAIN=<subdomain> \
  bash -c 'read -rsp "password: " ACMEDNS_PASSWORD; echo; export ACMEDNS_PASSWORD
  ~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d a.com -d "*.a.com"'
```

`Cert success.`가 나오면 스테이징 인증서를 지웁니다.

```bash
~/.acme.sh/acme.sh --remove -d a.com --ecc
rm -rf ~/.acme.sh/a.com_ecc
```

### 웹서버 설정

스크립트가 인증서를 `CERT_DIR`(기본 `/etc/ssl/<도메인>`)에 저장하고, 웹서버 설정이 그 경로를 아직 쓰지 않으면 넣을 설정을 출력합니다. 직접 넣을 때는 이렇게 합니다.

```nginx
# nginx
ssl_certificate     /etc/ssl/a.com/fullchain.pem;
ssl_certificate_key /etc/ssl/a.com/key.pem;
```

```apache
# Apache
SSLCertificateFile    /etc/ssl/a.com/fullchain.pem
SSLCertificateKeyFile /etc/ssl/a.com/key.pem
```

```caddy
# Caddy
example.com, *.example.com {
	tls /etc/ssl/a.com/fullchain.pem /etc/ssl/a.com/key.pem
}
```

HAProxy 같은 그 밖의 웹서버는 `manual`을 골라 reload 스크립트를 입력합니다. HAProxy는 인증서와 키를 한 파일로 합친 뒤 reload하는 스크립트를, Docker 컨테이너는 `docker kill -s HUP <컨테이너>`를 쓰면 됩니다.

## 4. 자동 갱신

acme.sh는 설치할 때 cron을 등록합니다. 갱신 시점이 되면 같은 자격 증명으로 `/acme/update`를 다시 부르고, 새 인증서를 설치한 뒤 `--reloadcmd`를 실행합니다. 따로 할 일은 없습니다.

- 자격 증명(`ACMEDNS_*`)은 acme.sh가 도메인 설정(`~/.acme.sh/a.com_ecc/a.com.conf`)에 저장합니다. 그래서 갱신 때는 환경변수가 필요 없습니다. 이 파일에는 비밀번호가 평문으로 들어 있으므로 서버 접근 권한을 관리합니다.
- 갱신 상태 확인: `~/.acme.sh/acme.sh --list`
- **reload에 `sudo`가 있으면 비밀번호 없이 되어야 합니다.** 자동 갱신(cron)은 sudo 비밀번호를 입력할 수 없습니다. 비밀번호가 필요하면 인증서 파일은 갱신돼도 웹서버가 reload되지 않아 옛 인증서로 계속 서비스됩니다. 스크립트가 끝에서 이를 점검해 경고합니다. 이미 설치한 서버는 이렇게 확인합니다.

  ```bash
  # 저장된 reload 명령 (sudo가 있는지 본다)
  sed -n "s/^Le_ReloadCmd='__ACME_BASE64__START_\(.*\)__ACME_BASE64__END_'/\1/p" ~/.acme.sh/a.com_ecc/a.com.conf | base64 -d; echo
  # sudo가 비밀번호 없이 되는지 (실패하면 갱신 때 reload가 막힌다)
  sudo -k; sudo -n true && echo "비밀번호 없이 가능" || echo "비밀번호 필요"
  ```

  고치는 방법은 셋 중 하나입니다.
  1. **root 계정으로 설치합니다.** `sudo -i`(또는 `su -`)로 root 셸이 된 뒤 `setup.sh`를 실행하세요. root에서는 스크립트가 reload 기본 명령에서 `sudo`를 뺍니다. acme.sh와 갱신 cron도 root 것으로 등록되어 비밀번호 문제가 없습니다. `sudo bash setup.sh`는 피하세요. 환경에 따라 acme.sh와 cron이 일반 계정 쪽에 설치될 수 있습니다.
  2. `visudo`로 reload 명령만 비밀번호 없이 허용합니다(`<계정> ALL=(root) NOPASSWD: /bin/systemctl reload nginx`). root보다 권한이 좁아 더 안전합니다.
  3. sudo 없이 되는 reload 스크립트를 씁니다(예: docker 그룹이면 `docker exec <컨테이너> caddy reload ...`).

  바꾼 명령은 `~/.acme.sh/acme.sh --install-cert -d a.com --ecc --reloadcmd '<새 명령>'`으로 다시 저장합니다. 계정을 바꿔 다시 설치하면 새로 발급하므로 Let's Encrypt의 같은 이름 발급 횟수 제한(주 5회)에 유의하세요.
- 설치 중 `sudo` 비밀번호를 한 번 물을 수 있습니다. 인증서 저장 디렉터리를 쓸 수 없을 때 만들기 위해서이고, 갱신에는 필요하지 않습니다(디렉터리 소유자가 현재 계정이 됩니다).
- 강제 갱신 테스트: `~/.acme.sh/acme.sh --renew -d a.com --ecc --force`

## 같은 도메인에 서버 추가

계정과 CNAME은 그대로 두고, 새 서버에서 3단계만 다시 실행합니다. 새 서버 IP가 `allowfrom`에 없으면 `/update`가 거부됩니다. `PATCH <앱>/api/domains/<id>/acme`에 `{"allowfrom":[...]}`로 목록 전체를 다시 보내 고칩니다. 계정과 CNAME은 그대로입니다.

## 문제가 생기면

[운영 → 문제 해결](operations.ko.md#문제-해결)을 봅니다.
