# 도메인 연결과 인증서 발급

`a.com`과 `*.a.com` 인증서를 nginx 서버에서 자동 발급·갱신하는 과정을 예로 듭니다. ssl-checker가 [설치](installation.md)되어 있고, `/health`가 `{"db":"ok","provider":"ok"}`라고 가정합니다.

아래에서 `<앱>`은 클라이언트가 부르는 api 주소입니다. 호스트 포트로 열었다면 `http://<BIND_ADDR>:<API_PORT>`, Caddy 뒤라면 Caddy에 설정한 주소(예: `https://ssl-checker.example.com`)입니다. `<존>`은 `CHALLENGE_ZONE` 값입니다.

## 전체 흐름

| 단계 | 누가 | 횟수 |
| --- | --- | --- |
| 1. 계정 등록 | 관리자 (`ADMIN_KEY` 보유) | 도메인당 1번 |
| 2. CNAME 등록 | 그 도메인의 DNS 관리자 | 도메인당 1번 |
| 3. 발급과 설치 | 각 서버 | 서버당 1번 |
| 4. 자동 갱신 | 각 서버의 acme.sh cron | 자동 |

## 1. 계정 등록

도메인 하나에 계정 하나를 만듭니다. 이 계정으로 `a.com`과 `*.a.com`을 함께 처리합니다. `*.a.com` 인증서가 `www.a.com`, `api.a.com` 같은 한 단계 아래 이름을 모두 덮습니다. 두 단계 아래(`*.dev.a.com`)가 필요하면 `dev.a.com`을 따로 등록합니다.

`ADMIN_KEY`가 셸 히스토리에 남지 않게 입력으로 받습니다.

```bash
read -rs ADMIN_KEY      # 붙여넣고 Enter. 화면에 표시되지 않는다
curl -s -X POST <앱>/register \
  -H "Authorization: Bearer $ADMIN_KEY" \
  -d '{"domain":"a.com","allowfrom":["10.0.1.10/32","10.0.1.11/32"]}'
unset ADMIN_KEY
```

- `domain`: 인증서를 받을 도메인. 와일드카드(`*.a.com`)가 아니라 `a.com`으로 등록합니다.
- `allowfrom`: 이 계정으로 `/update`를 부를 수 있는 서버 IP나 CIDR 목록. 비우면 어디서든 부를 수 있습니다. 서버 IP를 알고 있다면 넣는 것을 권장합니다. 프록시 뒤나 rootless Docker라면 먼저 [출발지 IP를 확인](configuration.md#네트워크와-프록시)합니다.

응답 예:

```json
{
  "username": "c36f50e8-4632-44f0-83fe-e070fef28a10",
  "password": "htg2sPJ8C0…(40자)",
  "fulldomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example",
  "subdomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a",
  "allowfrom": ["10.0.1.10/32", "10.0.1.11/32"]
}
```

`password`는 이때 한 번만 나오고 다시 볼 수 없습니다. 이 도메인의 인증서를 발급받을 수 있는 비밀이므로 시크릿 저장소에 바로 넣습니다. 티켓이나 채팅에 붙이지 않습니다. 잃어버리면 [재등록](operations.md#계정-폐기와-키-재발급)합니다.

이미 활성 계정이 있는 도메인을 다시 등록하면 `409 account_exists`가 납니다.

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

`a.com`에 CAA 레코드가 있다면 `letsencrypt.org`가 허용 목록에 있어야 합니다.

```bash
dig +short CAA a.com
```

## 3. 발급과 설치

인증서를 쓸 서버에서 실행합니다. acme.sh는 **3.1.4 이상**을 씁니다. 그보다 낮은 버전은 ARI(갱신 시점 안내)를 지원하지 않습니다.

### acme.sh 설치

```bash
curl -fsSL https://get.acme.sh | sh -s email=ops@example.com
~/.acme.sh/acme.sh --version                         # 3.1.4 이상
~/.acme.sh/acme.sh --set-default-ca --server letsencrypt
```

### 자격 증명

비밀번호는 입력으로 받아 셸 히스토리에 남기지 않습니다.

```bash
export ACMEDNS_BASE_URL="<앱>"                        # 예: https://ssl-checker.example.com
export ACMEDNS_USERNAME="<username>"
read -rs ACMEDNS_PASSWORD && export ACMEDNS_PASSWORD
export ACMEDNS_SUBDOMAIN="<subdomain>"
```

### 먼저 스테이징으로 확인

처음 연결하는 도메인은 Let's Encrypt 스테이징으로 먼저 발급해 봅니다. 운영 CA의 발급 횟수 제한에 걸리지 않습니다.

```bash
~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d a.com -d '*.a.com'
```

`Cert success.`가 나오면 스테이징 인증서를 지웁니다.

```bash
~/.acme.sh/acme.sh --remove -d a.com --ecc
rm -rf ~/.acme.sh/a.com_ecc
```

### 운영 발급

```bash
~/.acme.sh/acme.sh --issue --dns dns_acmedns -d a.com -d '*.a.com'
```

### 웹서버에 설치

인증서 파일을 웹서버가 읽는 곳에 복사하고, 갱신될 때마다 다시 읽게 합니다.

```bash
sudo mkdir -p /etc/ssl/a.com
~/.acme.sh/acme.sh --install-cert -d a.com --ecc \
  --key-file       /etc/ssl/a.com/key.pem \
  --fullchain-file /etc/ssl/a.com/fullchain.pem \
  --reloadcmd      "sudo systemctl reload nginx"
```

웹서버별 reload 명령:

| 웹서버 | `--reloadcmd` |
| --- | --- |
| nginx | `systemctl reload nginx` |
| Apache | `systemctl reload apache2` (RHEL 계열은 `httpd`) |
| HAProxy | 인증서와 키를 한 파일로 합친 뒤 `systemctl reload haproxy` |
| Docker 컨테이너 | `docker kill -s HUP <컨테이너>` 또는 `docker restart <컨테이너>` |

nginx 설정 예:

```nginx
ssl_certificate     /etc/ssl/a.com/fullchain.pem;
ssl_certificate_key /etc/ssl/a.com/key.pem;
```

## 4. 자동 갱신

acme.sh는 설치할 때 cron을 등록합니다. 갱신 시점이 되면 같은 자격 증명으로 `/update`를 다시 부르고, 새 인증서를 설치한 뒤 `--reloadcmd`를 실행합니다. 따로 할 일은 없습니다.

- 자격 증명(`ACMEDNS_*`)은 acme.sh가 도메인 설정(`~/.acme.sh/a.com_ecc/a.com.conf`)에 저장합니다. 그래서 갱신 때는 환경변수가 필요 없습니다. 이 파일에는 비밀번호가 평문으로 들어 있으므로 서버 접근 권한을 관리합니다.
- 갱신 상태 확인: `~/.acme.sh/acme.sh --list`
- 강제 갱신 테스트: `~/.acme.sh/acme.sh --renew -d a.com --ecc --force`

## 같은 도메인에 서버 추가

계정과 CNAME은 그대로 두고, 새 서버에서 3단계만 다시 실행합니다. 새 서버 IP가 `allowfrom`에 없으면 `/update`가 거부됩니다. 지금은 `allowfrom`을 수정하는 API가 없으므로 다음 둘 중 하나로 처리합니다.

- 처음 등록할 때 서버들이 있는 대역(예: `10.0.1.0/24`)을 넣어 둡니다.
- 계정을 폐기하고 새 IP를 넣어 재등록합니다. 재등록하면 `subdomain`이 바뀌므로 CNAME도 다시 등록해야 합니다.

## 문제가 생기면

[운영 → 문제 해결](operations.md#문제-해결)을 봅니다.
