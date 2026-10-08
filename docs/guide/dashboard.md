# Dashboard

**English** | [한국어](dashboard.ko.md)

The web UI shows the certificate status of your domains; the admin also manages domains and check targets and enables ACME there. Below, `<app>` is the api address (see [Installation](installation.md)). The UI opens at `<app>/`. The UI is available in English and Korean (a server option picks the language); where a button is named below, its Korean label is given in parentheses.

## Who can do what

| | Not logged in | Admin (logged in with `ADMIN_KEY`) |
| --- | --- | --- |
| Domain status, check results, history, ACME status | Can view | Can view |
| Add, edit and delete domains and targets; check now | Buttons are hidden | Allowed |
| Enable ACME, edit allowed IPs, rotate key, check CNAME, disable ACME | Buttons are hidden | Allowed |
| Audit log | Cannot view | Can view |

Viewing requires no authentication, so server IPs and notes are visible in the UI. If you cannot expose them publicly, restrict access at the front proxy (see the Caddy example in [Configuration](configuration.md)).

## Screens

**Dashboard (`/`)**: The cards at the top show the number of domains per status. Clicking a card shows only that status; clicking it again clears the filter. The table is sorted by worst status first, then by nearest expiry. It refreshes automatically every 60 seconds.

**Domain detail (`/domains/<id>`)**: For each target and each connection address, it shows the status, expiry date, issuer, first 8 characters of the serial, TLS version and last check. Clicking **History** (`이력` in the Korean UI) shows the last 50 checks of that target. The ACME panel at the bottom shows the CNAME record, the CNAME check result, the last TXT update, the number of consecutive failures and the allowed IPs.

**Usage guide (`/guide`)**: Instructions that can be viewed without logging in. It contains the one-line command to run on the server that receives the certificate, what the script does (checks credentials and allowed IPs, installs acme.sh, selects the web server, sets the storage path, checks the configuration), and configuration examples for each web server.

**Audit log (`/audit`, admin)**: Logins, write operations and ACME updates, newest first.

### Statuses and result codes

Badges use both color and text: **Critical** (red), **Warning** (orange), **Info** (blue), **OK** (green), **Unchecked** (gray); in the Korean UI these are **긴급**, **경고**, **정보**, **정상** and **미점검**. Hover over a code such as `CONNECT_FAILED` under a badge to see what it means. The full table is in the [API reference](api.md).

**Stale badge** (`오래됨` in the Korean UI): Shown when the last check is older than twice the check interval (`CHECK_INTERVAL`), on both the dashboard and the domain detail screen. Status is stored at check time, so if the worker stops, the last "OK" keeps showing. If you see this badge, check the worker (see [Operations](operations.md)).

## Admin login

Go to `<app>/admin` and enter `ADMIN_KEY`. The top bar has no login button, so the admin has to open this address directly (this only hides the address and is not a security measure; protection comes from the key and the server's permission checks). A login lasts 12 hours, and opening `/admin` while already logged in takes you back to the dashboard.

- **Connect over HTTPS.** If the public address is `http://`, the login cookie is not marked `Secure` (if it were, the browser would not send the cookie), so the cookie can be seen on the network.
- **Log out only deletes the cookie in the browser.** The server keeps no sessions, so a cookie that has already been copied stays valid until it expires (12 hours). To end all sessions, change `ADMIN_KEY` and redeploy.
- A wrong key is accepted up to 5 times per IP; after that, only 1 attempt every 12 seconds.
- If you attempt a write after the session has expired, the UI switches back to read-only. Log in again.

## Registering domains and targets

1. In **Add domain** (`도메인 추가` in the Korean UI), enter the domain without a wildcard (`a.com`). ACME is off at this stage.
2. On the domain detail screen, use **Add target** (`대상 추가` in the Korean UI) to register the TLS endpoint to check.

| Field | Description |
| --- | --- |
| Hostname, port | Required. Example: `www.a.com`, `443` |
| SNI | If empty, the hostname is used |
| Server IPs | If empty, it connects once using the hostname (only one address is checked even if DNS returns several). If set, it connects to each IP separately and compares whether every server has the same certificate installed. Up to 16 |

Saving runs one check immediately and shows the result. After that, the worker checks every `CHECK_INTERVAL` (default 6 hours). If you change the connection settings, results for the old addresses are no longer shown.

## Enabling ACME

In the ACME panel on the domain detail screen, click **Enable ACME** (`ACME 켜기` in the Korean UI).

1. Enter the **allowed IPs**: the IPs or CIDRs of the servers that will receive the certificate. Specify them whenever possible, so that a leaked key cannot be used from anywhere else.
2. After enabling, a **dialog that is shown only once** appears. It contains the CNAME record, the username and password, and the one-line command to run on the server. **Once you close this dialog, you cannot see the password again.** It does not close with Esc; it closes only with its button.
3. Add the one CNAME record to the domain's DNS, then use **Check CNAME now** (`CNAME 지금 확인` in the Korean UI) in the panel and confirm that it shows CNAME OK (`CNAME 정상` in the Korean UI).
4. Run the command from the dialog on the server that receives the certificate (`curl -fsSL <app>/acme/setup.sh -o setup.sh && bash setup.sh`). The script asks for values (the password is not shown as you type it and does not end up in the shell history), checks this server's allowed IP, the acme.sh installation, the web server type and install location, whether the domain's CNAME is in place, and the certificate storage path, and then issues and installs the certificate. If no web server is detected or you want to reload it yourself, choose `manual` and enter a reload script. On servers where reload needs `sudo`, run it from a root shell (`sudo -i`) (the automatic renewal cron cannot enter a sudo password). The full flow is described in [Connecting a domain and issuing certificates](onboarding.md).
5. Once issuance succeeds, the panel's last TXT update (`마지막 TXT 갱신` in the Korean UI) is filled in.

**Rotate key** (`키 재발급` in the Korean UI): The old key becomes invalid immediately, and the subdomain (CNAME) stays the same. You must run the script again on the server with `FORCE=1 bash setup.sh` and enter the new password for the new key to be saved in the acme.sh configuration (the command in the dialog already includes `FORCE=1`). Otherwise the next automatic renewal fails with 401.

**Disable ACME** (`ACME 끄기` in the Korean UI): Revokes the account and deletes the challenge TXT. If you enable it again, a new subdomain is created, so you have to set up the CNAME again.

## `/llms.txt` for agents

`<app>/llms.txt` is a text file with the procedure an agent should follow to set up ACME with this server. It is filled in with this server's address (`PUBLIC_URL`, or a value built from the request if empty) and the challenge zone (`CHALLENGE_ZONE`), and it can be read without authentication. Give the agent this address and pass the admin key to it separately. It is safer not to paste the key into chats or tickets.

## Trying it locally

If you have Docker, you can start it right away with the memory provider (nothing is written to real DNS).

```bash
docker compose -p sslchk-demo -f deploy/compose.yaml -f deploy/compose.ports.yaml \
  --env-file deploy/local.env.example up -d --build
# open http://127.0.0.1:18090 and log in with the ADMIN_KEY from deploy/local.env.example
docker compose -p sslchk-demo -f deploy/compose.yaml -f deploy/compose.ports.yaml \
  --env-file deploy/local.env.example down -v
```

## UI development

The UI is React + Vite + TypeScript in `web/`. It requires Node 24.

```bash
make web-test      # logic tests (node --test)
make web-build     # type check + build; the output goes to internal/webui/dist and is embedded in the binary
cd web && npm run dev   # dev server (:5173); /api is proxied to a local ssl-checker serve (:8080)
```

The Docker image runs the UI tests and build first, so if either fails, the image is not built. A binary built with `go build` alone, without building the UI, shows a notice at `<app>/` that the UI has not been built (503), while the API and `/llms.txt` keep working.
