# ssl-checker

**English** | [한국어](README.ko.md)

ssl-checker is a server that periodically checks the TLS certificates of the domains you register (expiry date, issuer, whether every server serves the same certificate) and shows the results on a web dashboard. As an add-on, it provides DNS-01 challenge delegation so that acme.sh can automatically issue and renew Let's Encrypt certificates even for domains whose DNS has no API. Wildcard certificates work too. ACME can be turned on or off per registered domain.

It exposes the same API as [acme-dns](https://github.com/joohoi/acme-dns), so each server uses the `dns_acmedns` plugin that already ships with acme.sh. Nothing extra needs to be installed on the client side. Unlike acme-dns, it does not run its own DNS server: it writes the challenge TXT records through a DNS provider's API, such as Cloudflare. That means you do not have to open port 53 or delegate NS records.

## How ACME delegation works

```
 server (acme.sh) ──(1) order──▶ Let's Encrypt
      │                              │
      │(2) /acme/update (TXT value)  │(4) look up _acme-challenge.a.com
      ▼                              ▼
 ssl-checker ──(3) write TXT──▶ <uuid>.acme-chk.net   ◀──CNAME── _acme-challenge.a.com
              (Cloudflare API)    (challenge-only zone)          (a.com DNS, registered once)
```

1. acme.sh on each server places an order with Let's Encrypt and receives a validation token.
2. acme.sh sends the TXT value it computed to ssl-checker's `/acme/update`.
3. ssl-checker writes the TXT record at `<uuid>.<zone>` in the challenge-only zone.
4. Let's Encrypt looks up `_acme-challenge.a.com`, follows the CNAME, and checks the TXT record.

You register the `_acme-challenge` CNAME **only once** per domain. After that, a.com's DNS is never touched again. Certificates and private keys are created and kept by acme.sh on each server; ssl-checker never handles private keys.

## Features

- Certificate monitoring: register check targets per domain (hostname, port, SNI, list of server IPs) and ssl-checker periodically checks the expiry date, issuer, chain, hostname and TLS version. It also compares whether every server has the same certificate installed.
- Web dashboard: anyone can view the status; registering, editing and ACME settings are reserved for an administrator logged in with `ADMIN_KEY` ([Dashboard](docs/guide/dashboard.md)).
- `/llms.txt`: a step-by-step procedure for agents to follow when setting up ACME, pre-filled with the server address and the challenge zone.
- acme-dns-compatible `/update` API (also accepted at `/acme/update`): automated tests verify that its responses match the original acme-dns.
- Per-domain accounts and keys: keys are stored only as bcrypt hashes, and each account can be restricted to allowed IP ranges (`allowfrom`).
- Two values kept at once: keeps both TXT records needed when `a.com` and `*.a.com` are issued on a single certificate.
- DNS provider interface: Cloudflare is the default implementation, plus an in-memory implementation for testing.
- Daily cleanup of orphaned TXT records: only records of the form `<uuid>.<zone>` in the challenge zone are touched.
- Audit log: registrations, successful, failed and rejected updates, and cleanup jobs are all recorded.
- Rate limiting: limits update frequency per account, authentication failures per IP, and admin key failures.

### Not yet available

- Notifications (Slack, email, etc.). Check the status on the dashboard or through the API.
- Per-user accounts and roles. Everything is managed with a single admin key.

## Quick start (try it locally)

All you need is Docker. It uses the in-memory provider, so nothing is written to real DNS.

```bash
git clone <this repository> ssl-checker && cd ssl-checker
./scripts/smoke.sh
```

`smoke.sh` builds the image and brings up the compose stack. It then checks, in order: account registration → TXT update → rejection of a wrong key, and tears the stack down at the end. If the last line says `smoke OK`, everything works.

There is no published container image. For a real deployment, compose builds the image from this repository's `Dockerfile`; see [Installation and deployment](docs/guide/installation.md).

## Documentation

| Document | Contents |
| --- | --- |
| [Installation and deployment](docs/guide/installation.md) | Prerequisites, image build, docker compose deployment, first-start checks |
| [Configuration](docs/guide/configuration.md) | Full list of environment variables, network and proxy setup, secret management |
| [Connecting domains and issuing certificates](docs/guide/onboarding.md) | Account registration, CNAME registration, acme.sh issuance, installation and renewal, adding servers |
| [Dashboard](docs/guide/dashboard.md) | Using the UI, admin login, registering targets, enabling ACME, `/llms.txt`, UI development |
| [API reference](docs/guide/api.md) | Requests, responses and error codes for `/acme/*` (update, verify, setup.sh), `/register`, `/health` and `/api` |
| [Operations](docs/guide/operations.md) | Updates, backups, key rotation, revoking accounts, audit log, troubleshooting |

## Commands

A single image contains four subcommands.

| Command | Role |
| --- | --- |
| `ssl-checker migrate` | Applies database migrations and exits |
| `ssl-checker serve --listen :8080` | `/acme/*` (update, verify, setup.sh), `/update`, `/register`, `/health`, `/api`, the dashboard, `/llms.txt` |
| `ssl-checker worker` | Cleans up orphaned TXT records once at startup and every 24 hours after that. Runs TLS checks and CNAME checks every `CHECK_INTERVAL` |
| `ssl-checker healthcheck [--url URL]` | Checks `/health`, for use as the container health check |

## Development

You need Go 1.27 or later and Docker. To work on the UI (`web/`), you also need Node 24.

```bash
make db-up && make test && make db-down   # unit and integration tests (uses a Postgres container)
make web-test && make web-build           # UI logic tests, type checking and build
make script-test-linux                    # setup.sh tests on Linux bash 5 (requires Docker)
./scripts/smoke.sh                        # smoke test of the image + compose stack
./scripts/compat.sh                       # compare /update responses with the original acme-dns
```

Tests that need a database are skipped when `TEST_DATABASE_URL` is not set. `make test` sets this variable for you.

## License

[MIT](LICENSE)
