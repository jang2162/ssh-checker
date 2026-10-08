# Operations

**English** | [한국어](operations.ko.md)

The commands below assume you started the stack as described in [Installation](installation.md), with `deploy/.env` and the project name `ssl-checker`. For convenience, define these aliases. If you exposed the api on a host port, replace `compose.caddy.yaml` with `compose.ports.yaml`. If you deployed with Komodo, do the same work in the Komodo UI, or run the commands in the Stack directory on the server.

```bash
alias dc='docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.caddy.yaml --env-file deploy/.env'
alias psql-sslchk='dc exec db psql -U sslchk -d sslchk'
```

## Updating

```bash
git pull
dc up -d --build
```

compose rebuilds the image from the new source. `migrate` applies new migrations first, and api and worker start on the new version only if it succeeds. Because api and worker are separate, certificates that have already been issued are not affected while the api restarts. A server that was in the middle of a renewal is retried by acme.sh on its next cron run.

## Backup and restore

The DB holds the domains, accounts (key hashes), TXT values and the audit log. Back it up once a day and keep the backup in a different storage location.

```bash
dc exec -T db pg_dump -U sslchk -d sslchk --format=custom > sslchk-$(date +%F).dump
```

Restore:

```bash
dc stop api worker
dc exec -T db pg_restore -U sslchk -d sslchk --clean --if-exists < sslchk-2026-10-03.dump
dc start api worker
```

If you lose the DB, every account is gone. You then have to re-register each domain and set up the CNAME and the server credentials again.

## Rotating secrets

| Secret | Procedure |
| --- | --- |
| `ADMIN_KEY` | Change the value in `deploy/.env` → `dc up -d api` |
| `CF_API_TOKEN` | Create a new token in Cloudflare → change the value in `deploy/.env` → `dc up -d api worker` → confirm that `/health` shows `provider: ok` → delete the old token |
| Account keys | See [Revoking accounts and rotating keys](#revoking-accounts-and-rotating-keys) below |

### When ADMIN_KEY leaks

With `ADMIN_KEY`, anyone can clear `allowfrom` and rotate keys, which gives them a working key for every domain whose CNAME is already in place. Changing the value does not invalidate keys the attacker has already obtained, so clean up in this order:

1. Set `ADMIN_KEY` in `deploy/.env` to a new value and redeploy with `dc up -d api`. This also ends every login session.
2. Check `GET <app>/api/audit` for `acme_rotate`, `acme_update`, `acme_enable` or `login` entries that you did not make.
3. For affected domains, [rotate the key](#revoking-accounts-and-rotating-keys) and restore `allowfrom` to its original value (`PATCH <app>/api/domains/<id>/acme`).

## Revoking accounts and rotating keys

If a key has leaked or been lost, **rotate** it. The username and subdomain stay the same, so you do not need to set up the CNAME again. The old key becomes invalid immediately.

```bash
curl -s -X POST <app>/api/domains/<id>/acme/rotate -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json'
```

Update the acme.sh configuration on each server with the new `password` from the response: run the setup script again on the server, as in `FORCE=1 bash setup.sh`, and enter the new password; `--issue --force` then saves the new value in the domain's configuration file.

If the domain no longer uses ACME, **disable** it. This revokes the account and deletes the challenge TXT. If you enable it again, the subdomain changes and you have to set up the CNAME again.

```bash
curl -s -X DELETE <app>/api/domains/<id>/acme -H "Authorization: Bearer $ADMIN_KEY" -H 'Content-Type: application/json'
```

If the DNS provider fails, nothing is changed and the server returns 500 `dns_provider_error`. Call it again after the provider has recovered.

## Cleanup job

The worker reconciles the challenge zone against the DB once at startup and every 24 hours after that.

- It deletes `<uuid>.<zone>` TXT records that have no active account (revoked accounts, unknown names).
- If an active account's TXT differs from the DB, it resets the record to the DB value.
- It does not touch records whose name is not a lowercase UUID, so other records in the challenge zone are safe. Still, we recommend using the challenge zone for this purpose only.
- Even when it overlaps with an in-progress `/update`, it goes through the per-account lock, so it never deletes a value that is being issued.

Check the results in the worker log (`dc logs worker`) and in the `txt_cleanup` rows of the audit log.

When the worker restarts, it also runs a full TLS check and CNAME check immediately on startup.

### Network requirements

For the TLS checks and CNAME checks, the worker must be able to open these outbound connections:

- TCP to every monitored target, including any internal IPs listed on targets.
- UDP/53 to `DNS_RESOLVER`.

## Audit log

The `audit_logs` table records every registration, update, denial and cleanup.

```bash
psql-sslchk -c "SELECT at, actor, action, target, source_ip, detail FROM audit_logs ORDER BY at DESC LIMIT 30;"
```

| `action` | Meaning | `detail` |
| --- | --- | --- |
| `register` | Account registered (`actor`=`admin`) | `subdomain=<uuid>` |
| `update` | TXT update succeeded | |
| `update_denied` | `/acme/update` (formerly `/update`) denied | `invalid_credentials`, `ip_unauthorized`, `subdomain_mismatch`, `bad_txt`, `rate_limited` |
| `verify_denied` | `/acme/verify` (the pre-flight check in `setup.sh`) denied | `invalid_credentials`, `ip_unauthorized` |
| `update_failed` | DNS provider or DB error | `transient: …` or `config: …` |
| `txt_cleanup` | Worker cleanup (`actor`=`worker`) | `deleted`, `repaired` |
| `login`, `login_denied` | UI login succeeded / failed (`actor`=`admin` / `anonymous`) | |
| `domain_create`, `domain_update`, `domain_delete` | Domain changed through the admin API | |
| `target_create`, `target_update`, `target_delete`, `target_check` | Monitoring target changed or checked through the admin API | |
| `acme_enable`, `acme_update`, `acme_rotate`, `acme_check`, `acme_disable` | ACME changed through the admin API | |

Actions taken through the admin API are recorded as `admin`, and failed logins as `anonymous`. You can also view the log with `GET <app>/api/audit` (admin).

Failed admin key authentication is logged in the api log (`dc logs api`) as `admin auth failed`. A lockout on `/register` is logged as `admin auth locked out`, but a lockout on `/api` is not logged. Failed UI logins are also recorded in the audit log as `login_denied`. Key values are never recorded.

The audit log is not deleted automatically. If needed, choose a retention period and delete old rows periodically.

```bash
psql-sslchk -c "DELETE FROM audit_logs WHERE at < now() - interval '1 year';"
```

## Health checks

```bash
curl -s <app>/health                            # {"db":"ok","provider":"ok"}
dc ps                                           # is api healthy?
dc logs --since 24h api worker                  # JSON logs
```

Repeated `update_failed` entries for the same account are a sign that the next renewal may fail. There is no alerting feature yet, so check the audit log regularly.

```bash
psql-sslchk -c "SELECT target, count(*), max(at) FROM audit_logs WHERE action = 'update_failed' AND at > now() - interval '7 days' GROUP BY target;"
```

## Troubleshooting

| Symptom | What to check |
| --- | --- |
| compose stops with `required variable … is missing a value` | That variable is empty in `deploy/.env`. See [Configuration](configuration.md) |
| `api` exits right after starting | `dc logs api`. For configuration errors, the name of the offending variable is shown (e.g. `ADMIN_KEY must be at least 32 bytes`). |
| `/health` shows `provider: config` | Token permissions (`Zone > DNS > Edit`), `CF_ZONE_ID`, the token's IP filter |
| `/health` shows `provider: transient` | Slow Cloudflare responses or rate limiting. Check again after a minute |
| acme.sh treats `/acme/update` as failed (401) | The `update_denied` reason in the audit log. For `ip_unauthorized`, compare `source_ip` with the account's `allowfrom`. |
| 401 but no row for that account in the audit log | `X-Api-User` is wrong or belongs to a different account. The value that was sent is recorded as-is in `actor`. |
| 429 `too_many_attempts` | Authentication failures have piled up from this IP. Fix the server that is using the wrong key and wait a while. |
| `update_failed` + `config` | Token permissions, Zone ID, token IP filter |
| `update_failed` + `transient` | Cloudflare outage or rate limiting. acme.sh retries on its next run. |
| CA validation fails (TXT not found) | A typo in the CNAME target, the name server delegation of the challenge zone, and whether `dig TXT <fulldomain> @1.1.1.1` shows the value |
| CA refuses because of CAA | Add `letsencrypt.org` to the domain's CAA record |
| Dashboard shows the stale badge | The worker has stopped, or no check has run for more than twice the check interval (`CHECK_INTERVAL`). Check `dc ps worker` and `dc logs worker`. |
| `<app>/` returns a 503 page saying the UI has not been built | The binary was built with `go build` without the UI, not the Docker image. Rebuild after `make web-build`, or use the image. The API works normally. |
| The certificate was issued but the web server still uses the old one | Whether the `--reloadcmd` of `--install-cert` has permission to run, and the certificate path in the web server configuration. If reload uses `sudo` and sudo requires a password, reload fails at renewal (cron) time ([Check and fix](onboarding.md#4-automatic-renewal)) |

## Uninstalling

```bash
dc down          # stops the containers only; data stays in the pgdata volume
dc down -v       # also deletes the data
```

Before uninstalling, delete each domain's `_acme-challenge` CNAME or move it to another DNS-01 method; otherwise the next renewal fails.
