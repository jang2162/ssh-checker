# Installation and deployment

**English** | [한국어](installation.ko.md)

This document covers getting ssl-checker up and running with Docker Compose. How to connect a domain and issue certificates is described in [Connecting domains and issuing certificates](onboarding.md).

## 1. Prerequisites

| Item | Description |
| --- | --- |
| One server | Linux with Docker and Docker Compose v2 installed. The servers that will receive certificates must be able to reach this server's api (via a host port or through Caddy). |
| A name for the challenge TXT records | One of the two options below. Examples: a dedicated domain `acme-chk.example`, or a sub-name of an existing zone `acme.example.com` |
| Cloudflare account | The zone that holds the challenge TXT records must be on Cloudflare. The free plan is enough. |
| Cloudflare API token | A token that has **only** the `Zone > DNS > Edit` permission on that zone. Do not share the token with other services (Caddy, etc.). |
| Domains that will receive certificates | You only need to be able to register an `_acme-challenge` CNAME once. A DNS API for those domains is not required. |

### Choosing the name for the challenge TXT records

Records are created as `<uuid>.<CHALLENGE_ZONE>`, and each domain's `_acme-challenge` CNAME points to that name. Changing it later means re-registering the CNAME for every domain, so settle on it up front.

| Option | `CHALLENGE_ZONE` example | Pros | Caveats |
| --- | --- | --- | --- |
| **Dedicated domain (recommended)** | `acme-chk.example` | The token's permission is scoped to this zone only, so if the token leaks the damage stops at the challenge TXT records. | You have to buy a new domain and add it as a Cloudflare zone. |
| Sub-name of an existing zone | `acme.example.com` (zone is `example.com`) | No new domain needed. Records are only created under `acme.`, so the zone does not get cluttered. | The token can edit the entire `example.com` zone, so if the token leaks your service records can be changed too. **`CF_ZONE_ID` is required.** |

Cloudflare tokens can only be scoped per zone, not per record. That is why a dedicated domain is better for limiting the blast radius. Splitting off a separate subzone is only available on the Cloudflare Enterprise plan. Either way, ssl-checker only creates and deletes names of the form `<lowercase UUID>.<CHALLENGE_ZONE>` and never touches other records in the zone.

## 2. Preparing Cloudflare

1. **If you use a dedicated domain**, add the zone with **Add a domain** on the dashboard home screen.
   - Skip importing DNS records and choose the Free plan.
   - At your registrar, change the nameservers to the two values Cloudflare gives you. Wait until the zone status becomes **Active**.
   - You do not need to create any records. If you use a sub-name of an existing zone, skip this step.
2. Create a token under **My Profile → API Tokens → Create Token → Create Custom Token**.
   - Permissions: `Zone` / `DNS` / `Edit`
   - Zone Resources: `Include` / `Specific zone` / the one zone that will hold the challenge TXT records
   - Client IP Address Filtering: the public egress IP of the ssl-checker server (recommended)
3. Copy the **Zone ID** from the API section on the right side of that zone's **Overview** page and put it in `CF_ZONE_ID`. A zone-scoped token may not be able to list zones, so always setting it is recommended.

## 3. Getting the repository

No official image is published. compose builds the image directly from the repository's `Dockerfile`, so all you need is the repository.

```bash
git clone <this repository> ssl-checker && cd ssl-checker
```

The image is distroless. It has no shell and no curl; the container health check uses the `ssl-checker healthcheck` subcommand.

## 4. Writing the environment file

```bash
cp deploy/production.env.example deploy/.env
chmod 600 deploy/.env
```

Open `deploy/.env` and fill in the values. At a minimum, set the values below. If they are empty, compose refuses to start.

| Variable | Value |
| --- | --- |
| `BIND_ADDR` | When exposing via a host port: the host IP to open the API on. An internal address reachable only by the servers that receive certificates |
| `CADDY_NETWORK`, `TRUSTED_PROXIES` | When running behind Caddy: the name of Caddy's Docker network and Caddy's address |
| `DNS_PROVIDER` | `cloudflare` |
| `CHALLENGE_ZONE` | The name for the challenge TXT records. A dedicated zone name, or a sub-name of an existing zone (e.g. `acme.example.com`) |
| `CF_API_TOKEN`, `CF_ZONE_ID` | The token and Zone ID from step 2 |
| `ADMIN_KEY` | `openssl rand -hex 32` |
| `POSTGRES_PASSWORD` | `openssl rand -hex 24` |

What each variable means, and what to watch out for behind a proxy, is described in [Configuration](configuration.md).

## 5. Starting up

Add one more compose file depending on how you expose the api. See [Configuration → Ways to expose the api](configuration.md#ways-to-expose-the-api) for details.

```bash
# Behind Caddy (no host port opened)
docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.caddy.yaml --env-file deploy/.env up -d

# Exposed directly on a host port
docker compose -p ssl-checker -f deploy/compose.yaml -f deploy/compose.ports.yaml --env-file deploy/.env up -d
```

The first run builds the image, so it takes a few minutes. Pass the same `-f` list to later commands such as `ps` and `logs`. Four services come up.

| Service | Role |
| --- | --- |
| `db` | PostgreSQL 17. Data lives in the `pgdata` volume. No port is exposed externally. |
| `migrate` | Builds the image (`ssl-checker:local`), applies the DB migrations, and exits. api and worker start only if it succeeds. |
| `api` | `/acme/*` (the API and scripts for servers that receive certificates), `/api/*`, the UI, `/register`, `/health`. The legacy `/update` is still available. It is exposed on `BIND_ADDR:API_PORT`, or reachable as `ssl-checker-api:8080` on the Caddy network. |
| `worker` | Runs TLS checks and ACME CNAME checks every `CHECK_INTERVAL`, and once a day cleans up orphaned TXT records and deletes check records older than 90 days. Run only one instance to avoid duplicate work. |

If `migrate` is `exited (0)` and `api` is `healthy`, everything is fine.

## 6. First-start check

Call `/health` from a server that will receive certificates.

```bash
curl http://<BIND_ADDR>:<API_PORT>/health          # when exposed on a host port
curl https://ssl-checker.example.com/health        # when behind Caddy (the address configured in Caddy)
# {"db":"ok","provider":"ok"}
```

| Response | Meaning |
| --- | --- |
| `provider":"ok"` | The Cloudflare token and zone access are working |
| `provider":"config"` | Check the token permissions, the Zone ID, and the token's IP filter. |
| `provider":"transient"` | Slow Cloudflare response or rate limiting. Check again in a minute. |
| `db":"error"` | Check the `db` container and `POSTGRES_PASSWORD`. |
| No connection | With a host port, check `BIND_ADDR`, the firewall, and the network between servers. Behind Caddy, check `reverse_proxy ssl-checker-api:8080` in the Caddyfile, the allowed `remote_ip` ranges, and whether api is attached to `CADDY_NETWORK` (`docker network inspect <network>`). |

Do not start connecting domains until `provider` is `ok`.

Also open `<app>/` in a browser and confirm that the [Dashboard](dashboard.md) appears. The UI (Node) is built together with the image, so the first build takes a little longer while it downloads npm packages.

## Deploying with Komodo (repository mode)

With the Git repo mode of a Komodo Stack, Komodo takes care of steps 3–5 above. On every deploy it pulls this repository and brings it up with compose, and compose builds the image from the repository's Dockerfile. No registry and no server login are needed. The full configuration is in [`deploy/komodo/ssl-checker.toml`](../../deploy/komodo/ssl-checker.toml).

### 1. Registering secrets

In Komodo **Settings → Variables**, create the three values below and turn on **Secret**. With Secret on, the values are masked in logs and hidden from non-admin users.

| Name | Value |
| --- | --- |
| `SSLCHK_ADMIN_KEY` | `openssl rand -hex 32` |
| `SSLCHK_CF_API_TOKEN` | Cloudflare token |
| `SSLCHK_POSTGRES_PASSWORD` | `openssl rand -hex 24` |

If you would rather not keep secrets in Komodo Core, you can instead put them under `[secrets]` with the same names in the Periphery config file on the deployment server. Periphery secrets are not transmitted through Core.

### 2. Creating the Stack

In the UI, click **Stacks → New Stack** and fill it in as below. Field names follow the Komodo v2.3 UI.

| UI field | Value |
| --- | --- |
| Server | The server to deploy to |
| Choose Mode | **Git Repo**. If you choose "Files On Server", the repository is not pulled and `compose.yaml` cannot be found. |
| Source: Git Provider / Repo / Branch | The Git server hosting this repository / `<org>/<repo>` / `main` (e.g. `git.example.com` / `ops/ssl-checker` / `main`. For GitHub, `github.com`) |
| Source: Git Account | For a private repository, the Git account registered in Komodo |
| Files: Run Directory | `deploy` (if left empty, it looks in the repository root and fails) |
| Files: File Paths | Behind Caddy: two lines, `compose.yaml` and `compose.caddy.yaml`. When exposing on a host port, change the second line to `compose.ports.yaml`. If left empty, only `compose.yaml` is used and the api is not exposed anywhere. |
| Ignore Services | `migrate` (a service that exits normally, so it is excluded from status checks) |
| Build Images: Pre Build Images | Optional. Even when off, the image is built on Deploy because of `pull_policy: build` in `compose.yaml`. When on, `docker compose build` runs before Deploy. Not shown for Swarm Stacks. |
| Environment | The `environment` block from `deploy/komodo/ssl-checker.toml`. Fill in the `<...>` values. This block assumes the behind-Caddy setup. When exposing on a host port, set `BIND_ADDR` and `API_PORT` instead of `CADDY_NETWORK`. |

In Environment, always insert secrets only through interpolation, such as `ADMIN_KEY=[[SSLCHK_ADMIN_KEY]]`. Komodo writes this content to `deploy/.env` and passes it with `--env-file`.

If you use Resource Sync, instead of entering values in the UI, put `deploy/komodo/ssl-checker.toml` in the sync's `resource_path` and fill in the `<...>` values in a copy of that file.

### 3. Deploying and verifying

Click **Deploy** on the Stack page. Komodo pulls the repository, builds the image, and starts `migrate` → `api`/`worker` in that order. Once the service status shows `api` healthy and `worker` running, do the [6. First-start check](#6-first-start-check) above.

### Updating

When a new commit lands on `main`, click **Deploy** on the Stack again. It pulls the latest commit, rebuilds, and replaces the containers. To deploy automatically on every push, copy the **Webhook URL** from the Stack settings and register it under the GitLab project's **Settings → Webhooks** for Push events.

## Next steps

Connect your first domain with [Connecting domains and issuing certificates](onboarding.md). For the first run, issuing against the Let's Encrypt **staging** environment is recommended.
