# Configuration

**English** | [한국어](configuration.ko.md)

ssl-checker takes all of its configuration from environment variables. `deploy/compose.yaml` passes each service only the variables it needs. That is why `ADMIN_KEY` goes only to api, and migrate receives only the database address.

## Environment variables

If a variable marked "Required" is empty, compose refuses to start. "Conditional" variables are needed only with certain settings.

| Variable | Description | Used by | Default / example | Type |
| --- | --- | --- | --- | --- |
| `BIND_ADDR` | The host IP to open the api port on. Use the address of an internal interface reachable only by the servers that receive certificates. If empty, the port would be opened on every interface, so compose blocks it. | api | `10.0.0.5` | Conditional (required with `compose.ports.yaml`) |
| `API_PORT` | The host port to open the api on | api | `8080` | Optional (only with `compose.ports.yaml`) |
| `CADDY_NETWORK` | The name of the external Docker network the Caddy container is attached to. Only api joins this network. | api | `caddy` | Optional (only with `compose.caddy.yaml`) |
| `TRUSTED_PROXIES` | Reverse proxy addresses whose `X-Forwarded-For` is trusted. Comma-separated CIDRs or IPs. Be sure to read [Network and proxies](#network-and-proxies) below. | api | empty | Optional |
| `DNS_PROVIDER` | The provider that writes the TXT records. `cloudflare` or `memory`. `memory` is for testing and does not write to real DNS. | api, worker | `cloudflare` | Required |
| `CHALLENGE_ZONE` | The name for the challenge TXT records. Records are created as `<uuid>.<CHALLENGE_ZONE>`. Use a dedicated zone name or a sub-name of an existing zone (e.g. `acme.example.com`). Every domain's CNAME points here, so settle on it up front. Case and a trailing dot are ignored. | api, worker | `acme.example.com` | Required |
| `CF_API_TOKEN` | A Cloudflare token with only the `Zone > DNS > Edit` permission on the dedicated zone | api, worker | | Conditional (required with `cloudflare`) |
| `CF_ZONE_ID` | The Zone ID of the Cloudflare zone that holds the challenge TXT records. If empty, `CHALLENGE_ZONE` is treated as the zone name and looked up. **Required when `CHALLENGE_ZONE` is a sub-name**; since zone-scoped tokens may lack permission to look up zones, always setting it is recommended. | api, worker | | Conditional |
| `TXT_TTL` | TTL of the challenge TXT records (seconds, 1–86400). Cloudflare raises values below 60 to 60. | api, worker | `60` | Optional |
| `CHECK_INTERVAL` | Interval for certificate checks and ACME CNAME checks. Go duration format, minimum `5m`. api only displays it in `/api/config`. | api, worker | `6h` | Optional |
| `DNS_RESOLVER` | Resolver `host:port` used for ACME CNAME checks. A public resolver is used instead of internal DNS so that the check matches the public DNS that Let's Encrypt sees. An IP without a port is treated as port 53. | api, worker | `1.1.1.1:53` | Optional |
| `PUBLIC_URL` | The external address at which clients reach the api. Format `http(s)://host[:port]`; a path, query or fragment is not allowed. Any other format prevents api from starting. Used by the UI, `/llms.txt`, `ACMEDNS_BASE_URL` in the credentials instructions, and as the default address in `/acme/setup.sh`. If empty, it is built from the request (including `X-Forwarded-Proto`/`X-Forwarded-Host` from trusted proxies). | api | `https://ssl-checker.example.com` | Optional |
| `UI_LANGUAGE` | Language of everything the server shows users: the dashboard, the `/acme/setup.sh` script and its messages, `/llms.txt`, and the status code descriptions (`/api/codes`). `en` or `ko`. Any other value prevents api from starting. It is fixed when the container starts; change it by redeploying. Documentation is not affected (see the `*.ko.md` files for Korean). | api | `en` (default), `ko` | Optional |
| `ADMIN_KEY` | The Bearer key for `/register`. api will not start unless it is at least 32 bytes. It is also the Bearer key for the admin API and the UI login key. Changing it ends all login sessions. | api | `openssl rand -hex 32` | Required |
| `POSTGRES_PASSWORD` | Database password. compose builds `DATABASE_URL` from this value, so use only characters that can appear in a URL as-is. | all | `openssl rand -hex 24` | Required |

Some variables are built internally by compose. You do not need to set them yourself.

| Variable | Description |
| --- | --- |
| `DATABASE_URL` | `postgres://sslchk:${POSTGRES_PASSWORD}@db:5432/sslchk`. When running the binary directly without compose, set this value yourself. |

### Running the binary directly

When running without compose, set the following variables for each role.

| Command | Required variables |
| --- | --- |
| `ssl-checker migrate` | `DATABASE_URL` |
| `ssl-checker serve --listen :8080` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, `ADMIN_KEY`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`, `TRUSTED_PROXIES`, `CHECK_INTERVAL`, `DNS_RESOLVER`, `PUBLIC_URL`, `UI_LANGUAGE`) |
| `ssl-checker worker` | `DATABASE_URL`, `DNS_PROVIDER`, `CHALLENGE_ZONE`, (`CF_API_TOKEN`, `CF_ZONE_ID`, `TXT_TTL`, `CHECK_INTERVAL`, `DNS_RESOLVER`) |
| `ssl-checker healthcheck --url http://127.0.0.1:8080/health` | none |

If the configuration is invalid, the process prints the names of all problematic variables and exits.

## Ways to expose the api

`compose.yaml` alone does not expose the api to the outside. Pick one of the two options and add one more compose file.

| Option | File to add | Required variables | Address clients call |
| --- | --- | --- | --- |
| Expose directly on a host port | `compose.ports.yaml` | `BIND_ADDR`, (`API_PORT`) | `http://<BIND_ADDR>:<API_PORT>` |
| Behind Caddy (no published port) | `compose.caddy.yaml` | `CADDY_NETWORK`, `TRUSTED_PROXIES` | The address configured in Caddy (e.g. `https://ssl-checker.example.com`) |

Either way, the calls arriving at this address are as follows. Servers that receive certificates call `/acme/*` (the API and scripts), administrators and visitors call the UI and `/api/*`, and monitoring calls `/health`. Because all paths for certificate-receiving servers are grouped under the single `/acme/` prefix, a proxy can expose just this prefix to the outside and keep everything else on the internal network. The container health check runs inside the container, so it needs no port.

### Running behind Caddy

This assumes Caddy runs as a container on the same server and that there is a Docker network (e.g. `caddy`) shared with other services.

1. **compose files**: `compose.yaml` + `compose.caddy.yaml`. api opens no host port and joins `CADDY_NETWORK` under the alias `ssl-checker-api`. db, migrate and worker do not join this network.
2. **Caddyfile**: forward with `reverse_proxy ssl-checker-api:8080`, as in [`deploy/Caddyfile.example`](../../deploy/Caddyfile.example). The certificate authority never calls this API. If the servers that receive certificates are outside, open only the single `/acme/*` line and let everything else (admin login, writes, audit log, the legacy `/update`) through only from internal ranges using `remote_ip`. Exposing read-only dashboard views to the outside is optional and is included in the example as a comment. **If Caddy is open to the internet, always keep the internal restriction.**
3. **Put Caddy's address in `TRUSTED_PROXIES`.** If you leave it empty, every request appears to come from Caddy. Then `allowfrom` does not work, and the authentication failure limit is shared by all servers. Caddy sends the real client IP in `X-Forwarded-For`, and ssl-checker uses that value only for requests coming from `TRUSTED_PROXIES`.
   - **Recommended: pin the Caddy container's IP and put only that one IP in.** Set `ipv4_address` for the network in Caddy's compose file.
   - If you do not pin it, put in the Caddy network's subnet. Check the subnet with `docker network inspect caddy --format '{{(index .IPAM.Config 0).Subnet}}'`. In this case, however, **other containers on the same network can also forge their IP through `X-Forwarded-For`.** Use this only when that network contains nothing but trusted containers.
4. **Use the HTTPS address.** The address client servers use is now Caddy's address (e.g. `https://ssl-checker.example.com`), and the setup script sets `ACMEDNS_BASE_URL` to `/acme` under that address.
   - For a public domain, Caddy obtains the certificate automatically.
   - If you use a private certificate such as `tls internal`, each server must trust Caddy's root certificate for acme.sh (curl) to connect.
5. **Verify**: send one `/acme/update` with a deliberately wrong key, then look at `source_ip` in the audit log. It must show **the IP of the calling server, not the Caddy container's IP**.

If there is another load balancer or Cloudflare proxy in front of Caddy, you also need to configure Caddy's `trusted_proxies` so that the original client IP is passed through.

## Network and proxies

ssl-checker uses the client IP for two decisions: the account's `allowfrom` (allowed IP ranges) and the per-IP authentication failure limit. The client IP is determined as follows.

- If the directly connected address is not in `TRUSTED_PROXIES`, that address is the client IP. `X-Forwarded-For` is ignored.
- If it is in `TRUSTED_PROXIES`, `X-Forwarded-For` is read from right to left and the first untrusted address is used as the client IP.

Three rules follow from this.

1. **Put only proxy addresses in `TRUSTED_PROXIES`.** If you include the range where clients live (e.g. all of `10.0.0.0/8`), clients in that range can set their own IP at will through `X-Forwarded-For` and bypass `allowfrom`.
2. **Leave it empty if there is no proxy in front.** Conversely, if you run behind a proxy and leave it empty, every request appears to come from the proxy IP. Then `allowfrom` becomes meaningless. The authentication failure limit is also shared by all clients, so one server's wrong key can block renewals on other servers.
3. **With rootless Docker or the userland proxy, check the source IP first.** In these environments, requests arriving on a published port can appear to come from the bridge gateway. Before setting a `/32` `allowfrom`, send one `/update` with a deliberately wrong key and see which address is recorded in `source_ip` in the audit log. How to query the audit log is described in [Operations](operations.md#audit-log).

The API does not need to be exposed to the internet. The certificate authority (Let's Encrypt) only queries DNS and never calls ssl-checker directly. Keep it on an internal network or VPN reachable only by the servers that receive certificates, and make sure plain-text HTTP never leaves the internal network.

## Secrets

| Secret | If it leaks | How to rotate |
| --- | --- | --- |
| `ADMIN_KEY` | Through the admin API, an attacker can clear `allowfrom` (`PATCH …/acme`) and reissue keys (`POST …/acme/rotate`), obtaining usable keys for every domain whose CNAME is already in place. That means they can get `a.com`/`*.a.com` certificates issued. Changing the value does not invalidate keys the attacker has already obtained. | Change the value and redeploy (this also ends all login sessions) → review the audit log → reissue keys for affected domains. [When ADMIN_KEY leaks](operations.md#when-admin_key-leaks) |
| `CF_API_TOKEN` | The TXT records in the challenge zone can be changed. Service domains remain safe. | Issue a new token in Cloudflare → replace the value → redeploy → delete the old token |
| Account key (`password` in the `/register` response) | Certificates for that domain can be issued. | Reissue the key (the CNAME stays the same) ([Operations](operations.md#revoking-accounts-and-rotating-keys)) |
| `POSTGRES_PASSWORD` | The database can be accessed. Since no port is exposed, this applies only from within the same Docker network. | Rotate together with changing the database password |

- Keep `deploy/.env` at permission 600 and never commit it to the repository. It is listed in `.gitignore`.
- ssl-checker never writes secrets to logs or responses. Account keys are stored only as bcrypt hashes. The plain-text key appears only once, in the registration response.

## Rate limits

The values are fixed in code.

| Target | Keyed by | Limit | When exceeded |
| --- | --- | --- | --- |
| Successful `/update` requests | account | 10 in a row, then 1 every 6 seconds | 429 `rate_limited` |
| `/update` authentication failures | client IP | 20, then 1 every 3 seconds | 429 `too_many_attempts` |
| `/register` admin key failures | client IP | 5, then 1 every 12 seconds | 429 `too_many_attempts` |

acme.sh calls `/update` once per domain name when issuing a certificate, so getting `a.com` and `*.a.com` together makes 2 calls. Normal use does not hit these limits.
