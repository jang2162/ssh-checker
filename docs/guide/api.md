# API reference

**English** | [한국어](api.ko.md)

All request and response bodies are JSON. Errors are returned as `{"error": "<code>"}`, like the original acme-dns. Request bodies are accepted up to 4KB.

| Method | Path | Caller | Authentication |
| --- | --- | --- | --- |
| `POST` | `/acme/update` (formerly `/update`) | acme.sh on each server (`dns_acmedns`) | `X-Api-User`, `X-Api-Key` |
| `POST` | `/acme/verify` (formerly `/verify`) | Setup script (pre-flight check) | `X-Api-User`, `X-Api-Key` |
| `GET` | `/acme/setup.sh` | Server that will receive the certificate | None |
| `POST` | `/register` | Administrator | `Authorization: Bearer <ADMIN_KEY>` |
| `GET` | `/health` | Monitoring, container health check | None |

Every path called by servers that receive certificates lives under a single prefix, `/acme/*` (the APIs `update` and `verify`, and the script `setup.sh`). You only need to expose this prefix through the reverse proxy; the rest (`/api/*`, the UI, `/register`) can stay on the internal network. If you give acme.sh `ACMEDNS_BASE_URL=<address>/acme`, it calls `/acme/update` (this is what the setup script configures). The root paths `/update` and `/verify` remain with the same behavior for existing configurations.

## POST /acme/update

Updates the challenge TXT value. The request and response format is the same as acme-dns's `/update`. Verified with acme.sh `dns_acmedns`. The acme-dns integrations of lego and certbot use the same format, but they may try to `/register` an account themselves. In that case they must be configured to use pre-registered credentials, and this has not been verified yet.

```http
POST /acme/update
X-Api-User: c36f50e8-4632-44f0-83fe-e070fef28a10
X-Api-Key: <password received at registration>
Content-Type: application/json

{"subdomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a", "txt": "<43-character validation value>"}
```

On success it returns `200` and `{"txt": "<stored value>"}`.

Processing rules:

- `txt` is the ACME DNS-01 validation value. It is exactly 43 characters and uses only `[A-Za-z0-9_-]`.
- The **2 most recent values** are kept per subdomain. When a 3rd value arrives, it replaces the oldest one. If an existing value arrives again, nothing changes. acme.sh never calls TXT deletion, so this rule also serves as cleanup.
- The value is stored in the DB only if writing it to the DNS provider succeeded. If the provider fails, nothing changes and a 5xx is returned, so acme.sh treats it as a failure and retries.
- Requests for the same account are processed in order. Even if values for `a.com` and `*.a.com` arrive at the same time, both are kept.

| Status | `error` | Cause |
| --- | --- | --- |
| 401 | `forbidden` | Username/key mismatch, revoked account, IP outside `allowfrom`, body `subdomain` differs from the account, malformed body JSON. Like the original acme-dns, the reasons are not distinguished. The reason is recorded in the audit log. |
| 400 | `bad_txt` | Malformed `txt` |
| 429 | `rate_limited` | Per-account update limit exceeded (10 in a row, then 1 every 6 seconds) |
| 429 | `too_many_attempts` | Authentication failure limit for this IP exceeded (20, then 1 every 3 seconds). This response is not recorded in the audit log. |
| 500 | `dns_provider_error` | Writing to the DNS provider failed. The audit log `detail` records `transient` (retry) or `config` (check the configuration). |
| 500 | `db_error` | DB error |

## POST /register

Creates the validation account for a domain. Administrators only.

```http
POST /register
Authorization: Bearer <ADMIN_KEY>
Content-Type: application/json

{"domain": "a.com", "allowfrom": ["10.0.1.0/24", "10.0.2.15"]}
```

| Field | Required | Description |
| --- | --- | --- |
| `domain` | Yes | The domain to get a certificate for. Normalized to lowercase, and a trailing dot is removed. Wildcards are not accepted. |
| `allowfrom` | No | List of CIDRs or IPs allowed to call `/update`. IPs are converted to `/32` (`/128` for IPv6). If empty, all IPs are allowed. |

On success it returns `201`. The response carries `Cache-Control: no-store`.

```json
{
  "username": "c36f50e8-4632-44f0-83fe-e070fef28a10",
  "password": "<40 characters, shown only this once>",
  "fulldomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example",
  "subdomain": "8e5700ea-a4bf-41c7-8a77-e990661dcc6a",
  "allowfrom": ["10.0.1.0/24", "10.0.2.15/32"]
}
```

- `username` and `subdomain` are random UUIDs. They are not derived from the domain name.
- `fulldomain` is the target that the `_acme-challenge.<domain>` CNAME points to.

| Status | `error` | Cause |
| --- | --- | --- |
| 400 | `malformed_json_payload` | Malformed body JSON |
| 400 | `invalid_allowfrom_cidr` | Malformed `allowfrom` entry |
| 400 | `invalid_domain` | Malformed domain, wildcard |
| 401 | `unauthorized` | `ADMIN_KEY` mismatch |
| 409 | `account_exists` | The domain already has an active account |
| 429 | `too_many_attempts` | Admin key failure limit for this IP exceeded (5, then 1 every 12 seconds) |
| 500 | `db_error` | DB error |

The original acme-dns `/register` is open without authentication and takes a different body. In ssl-checker it is administrator-only and the `domain` field is required.

## GET /health

Checks the DB connection and the DNS provider status.

```json
{"db": "ok", "provider": "ok"}
```

| Field | Values |
| --- | --- |
| `db` | `ok`, `error` |
| `provider` | `ok`, `transient` (slow response, rate limiting), `config` (token or zone misconfiguration) |

Returns `200` if both are `ok`, otherwise `503`. The provider check calls an external API, so the result is reused for 1 minute, and each check waits at most 3 seconds.

## Read API (`/api`)

View certificate check results and ACME status as JSON. Anyone can call it without authentication. Times are RFC 3339 UTC. Keys and key hashes never appear in any response.

| Request | Content |
| --- | --- |
| `GET /api/summary` | Number of domains, counts per status, time of the last check |
| `GET /api/domains` | Domain list. Can be filtered by status, e.g. `?status=critical` |
| `GET /api/domains/{id}` | Latest check results per target and per connection address, and the ACME status |
| `GET /api/targets/{id}/results?limit=50` | Check history of a target, newest first. `limit` up to 500 |
| `GET /api/config` | `challenge_zone`, `check_interval` (Go duration string, e.g. `"6h0m0s"`), `public_url` |
| `GET /api/codes` | Table of verdict codes `{"codes":[{"code","status","meaning"}]}`. Sorted from the worst status. The UI and `/llms.txt` use the same table |

Check results that did not obtain a certificate (connection failure, etc.) have no `not_before` or `not_after` fields.

Statuses are ordered from worst to best as `critical` > `warning` > `info` > `ok` > `unknown`. A domain's status is the worst of its targets' and its ACME status.

| Code | Status | Meaning |
| --- | --- | --- |
| `CONNECT_FAILED` | critical | Connection or TLS handshake failed (10 seconds). Very old servers that the Go TLS client rejects (RSA key exchange only, RSA keys under 1024 bits, SHA-1 signatures, etc.) also show up as `CONNECT_FAILED`, so check the `error` field. |
| `CERT_EXPIRED` | critical | Certificate expired |
| `CERT_EXPIRES_SOON` | critical | Less than `min(7 days, 1/3 of lifetime)` until expiry. 7 days for a 90-day certificate, 2 days for a 6-day short-lived certificate |
| `CHAIN_INVALID` | critical | Chain verification failed (including self-signed) |
| `HOSTNAME_MISMATCH` | critical | The certificate does not cover the connection name |
| `RENEWAL_OVERDUE` | warning | Remaining validity is less than 1/3 of the lifetime. Under normal operation it would already have been renewed |
| `SERIAL_MISMATCH` | warning | Servers of the same target present different certificates (partially missed deployment) |
| `TLS_OUTDATED` | warning | Negotiated below TLS 1.2 |
| `CNAME_MISSING` / `CNAME_MISMATCH` | warning | ACME is enabled but the `_acme-challenge` CNAME is missing or points elsewhere |
| `TXT_UPDATE_FAILING` | warning | `/update` failed 3 or more times in a row |
| `WEAK_KEY` | info | RSA under 2048 bits, ECDSA under 256 bits |
| `NOT_CHECKED` | unknown | No check result yet |

Errors are `{"error":"<code>"}`: 400 `invalid_status`, `invalid_limit`, 404 `not_found`, 500 `db_error`.

```bash
curl -s '<app>/api/domains?status=critical'
```

## Admin API (`/api`, administrator)

Manages domains and monitoring targets and turns ACME on and off. Administrators only.

### Authentication

| Method | Used by |
| --- | --- |
| `Authorization: Bearer <ADMIN_KEY>` | Scripts, agents |
| Session cookie `sslchk_session` (issued by `POST /api/session`) | Dashboard UI |

- Non-administrators get 401 `unauthorized`. Wrong keys (login or Bearer) are accepted up to 5 times per IP, and after that only 1 every 12 seconds (429 `too_many_attempts`). A locked IP has wrong-key attempts and Bearer requests blocked, but a cookie obtained by logging in still passes.
- Non-GET requests must have `Content-Type: application/json` (even without a body). Otherwise the response is 415 `unsupported_media_type`. Unknown fields or a body over 64KiB give 400 `malformed_json`.
- `allowfrom` holds at most 64 entries; more gives 400 `invalid_allowfrom`.
- Every write is recorded in the audit log (`GET /api/audit`).

### Session

| Request | Body | Response |
| --- | --- | --- |
| `POST /api/session` | `{"key":"<ADMIN_KEY>"}` | 200 `{"admin":true}` + cookie. 401 if wrong |
| `GET /api/session` | | `{"admin":true\|false}` (looks only at the session cookie; a Bearer key is not reported) |
| `DELETE /api/session` | | 204, cookie deleted |

The cookie is valid for 12 hours. Sessions are not stored on the server, so **a cookie that has already been copied stays valid until it expires, even after logout.** To end all sessions, change `ADMIN_KEY`. The cookie gets `HttpOnly` and `SameSite=Strict`, and also `Secure` unless the public address is `http://`. Using the UI login over HTTPS (behind Caddy) is recommended.

### Domains and targets

| Request | Body | Response |
| --- | --- | --- |
| `POST /api/domains` | `{"fqdn":"a.com","memo":""}` | 201 domain detail. 409 `domain_exists`, 400 `invalid_domain`, `invalid_memo` |
| `PATCH /api/domains/{id}` | `{"memo":"..."}` | 200 domain detail |
| `DELETE /api/domains/{id}` | | 204. If ACME is on, it is turned off first (challenge TXT deleted), then the domain is deleted together with its targets and records |
| `POST /api/domains/{id}/targets` | `{"hostname":"www.a.com","port":443,"sni":"","ips":["10.0.1.10"],"memo":""}` | 201 target (including the result of an immediate check). 409 `target_exists`, 400 `invalid_target` |
| `PATCH /api/targets/{id}` | Only the fields above that you want to change | 200 target. Changing hostname, port, sni or ips triggers an immediate re-check |
| `DELETE /api/targets/{id}` | | 204 |
| `POST /api/targets/{id}/check` | | 200 `{"results":[...]}` results of this check |

- `hostname`: The name to check. Internal names (single label) and IPs are also allowed. Wildcards are not.
- `ips`: List of IPs to connect to directly, one per server (up to 16). If empty, a single connection is made to the hostname.
- `memo`: Up to 500 characters.

### ACME

| Request | Body | Response |
| --- | --- | --- |
| `POST /api/domains/{id}/acme` | `{"allowfrom":["10.0.1.0/24"]}` | 201 **one-time credentials**. 409 `acme_enabled`, 400 `invalid_allowfrom` |
| `PATCH /api/domains/{id}/acme` | `{"allowfrom":[...]}` | 200 ACME status. With an empty list, `/update` can be called from anywhere |
| `POST /api/domains/{id}/acme/rotate` | | 200 one-time credentials. The username and subdomain stay the same; only the key changes. The old key is invalidated immediately |
| `POST /api/domains/{id}/acme/check` | | 200 ACME status (result of checking the CNAME right now) |
| `DELETE /api/domains/{id}/acme` | | 204. Account revoked, challenge TXT deleted. If you turn it on again, the subdomain changes and the CNAME must be set up again |

Calling update, rotate, check or disable on a domain with ACME turned off gives 409 `acme_disabled`. If the DNS provider fails while disabling ACME or deleting a domain, nothing is deleted and 500 `dns_provider_error` is returned.

One-time credentials response (`Cache-Control: no-store`). **The password appears only in this response.**

```json
{
  "username": "<uuid>", "password": "<40 characters>", "subdomain": "<uuid>",
  "fulldomain": "<uuid>.acme.example.com",
  "cname_record": "_acme-challenge.a.com. CNAME <uuid>.acme.example.com.",
  "allowfrom": ["10.0.1.0/24"],
  "setup": {
    "setup": {"id": "setup", "label": "세팅", "url": "https://.../acme/setup.sh", "cmd": "curl -fsSL https://.../acme/setup.sh -o setup.sh && bash setup.sh", "script": "#!/usr/bin/env bash\n..."},
    "servers": [{"id": "nginx", "label": "nginx", "config": "ssl_certificate ...;"}]
  },
  "rotated": false
}
```

`setup` contains the download URL, run command (`cmd`) and content (`script`) of the single script (`setup`), plus a configuration example (`config`) per web server (`nginx`, `apache`, `caddy`). The one script does everything from checking the acme.sh installation to configuring the web server. **The script contains no secrets such as the `password`.** When it runs on the server, you enter `username` and `password` or pass them as environment variables (the `subdomain` is supplied by `/acme/verify`). Addresses are based on `PUBLIC_URL` (or the request address if unset). The key rotation (`rotate`) response has `rotated: true`, and even if a certificate already exists you must re-run the setup script on the server with `FORCE=1` so that the new key is saved to the domain configuration (e.g. `FORCE=1 bash setup.sh`).

### Install and setup script

- `GET /acme/setup.sh`: The script file (`text/plain`). It can be downloaded without logging in and contains no secrets. A single line, `curl -fsSL <address>/acme/setup.sh -o setup.sh && bash setup.sh`, runs the setup. `curl ... | bash` also works; in that case input is read from `/dev/tty`.
- `GET /api/setup`: The content and run command of the script above, plus web server configuration examples (JSON).

The script proceeds in this order, and if something is wrong it explains why and stops before issuing.

1. Prompts for the credentials and uses `POST /acme/verify` to check **the credentials and whether this server's IP is in the allow list (`allowfrom`)**.
2. Checks that acme.sh is 3.1.4 or later, and installs it if it is missing or older (asks for an email address for expiry notices).
3. Detects the web server and offers it as the default (`nginx`, `apache`, `caddy`), and checks its executable and configuration directory. If none is detected or you want to reload it yourself, choose `manual` and enter a **reload script or command** (`RELOAD_CMD`). A script given as a path is checked to be executable.
4. Prompts for the domain (enter it without `*.`, like `a.com`; the wildcard `*.a.com` is always issued along with it, and entering `*.a.com` is treated as `a.com`) and checks via public DNS (`DNS_RESOLVER`, default `1.1.1.1`) that **the CNAME of `_acme-challenge.<domain>` points to the `fulldomain` returned by `/acme/verify`** (using `dig`, `host`, `nslookup`, or DNS over HTTPS if none are available). If it is missing or different, it shows the record to create; on a terminal it re-checks after you create it, otherwise it stops. Skip with `SKIP_CNAME_CHECK=1`.
5. Decides the certificate directory (`CERT_DIR`, default `/etc/ssl/<domain>`) and the reload command, then issues and installs the certificate. If the directory can be created or written without sudo, sudo is not used.
6. Checks whether the web server configuration already uses this certificate, and if not, prints the configuration to add.
7. Warns if the reload command uses `sudo` and this account needs a password for sudo, because automatic renewal (cron) cannot enter a password, so the reload would fail.

Values passed in advance as environment variables are not prompted for: `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD`, `ACMEDNS_SUBDOMAIN`, `SSLCHK_URL`, `ACME_EMAIL`, `WEB_SERVER`, `DOMAIN`, `CERT_DIR`, `CERT_GROUP` (Caddy), `RELOAD_CMD`. The web server configuration location is set with `WEB_CONF_DIR`, additional places to look for the executable with `WEB_SEARCH_DIRS`, and the executable check is skipped with `SKIP_WEB_CHECK=1`. Non-root accounts use `sudo` for the default reload command and for creating directories; root does not (decided by `id -u`, overridable with `SUDO_CMD`; empty means no sudo). On servers where reload needs sudo, run the script from a root shell (`sudo -i`), or allow only the reload command without a password (sudoers NOPASSWD).

## POST /acme/verify

A pre-flight check used before setup. It performs the same authentication and allowed-IP check as `/acme/update` but does not touch the TXT.

| Response | Meaning |
| --- | --- |
| 200 `{"ok":true,"source_ip":"<caller IP>","subdomain":"...","fulldomain":"..."}` | The credentials are correct and the IP is allowed. `subdomain` is acme.sh's `ACMEDNS_SUBDOMAIN`, and `fulldomain` is the address the domain's `_acme-challenge` CNAME must point to |
| 401 `forbidden` | The username or password is wrong |
| 403 `ip_not_allowed` + `source_ip` | The credentials are correct but this IP is not in the allow list |
| 429 `too_many_attempts` | Too many authentication failures from this IP |

Rejections use the same per-IP failure counter as `/acme/update`. They are recorded in the audit log as `verify_denied`.

### Audit log

`GET /api/audit?limit=100&before_id=<id>` (administrator, up to 500) → `{"entries":[{"id","at","actor","action","target","source_ip","detail"}]}`, newest first. For the next page, pass the `id` of the last entry as `before_id`. If `before_id` is not a number, the response is 400 `invalid_before_id`.

```bash
read -rs ADMIN_KEY
curl -s -X POST '<app>/api/domains' -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"fqdn":"a.com"}'
```

## UI and `/llms.txt`

| Request | Content |
| --- | --- |
| `GET /`, `/domains/<id>`, `/audit` | Dashboard (SPA). A binary built without the UI returns 503 and a notice page |
| `GET /llms.txt` | Procedure for an agent to follow when configuring ACME (`text/plain`). The server address and `CHALLENGE_ZONE` are filled in, and no authentication is required |

The UI does not intercept `/api/`, `/update`, `/register`, `/health` or `/llms.txt`. Usage is described in [Dashboard](dashboard.md).
