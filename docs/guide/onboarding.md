# Connecting a domain and issuing certificates

**English** | [한국어](onboarding.ko.md)

This guide walks through automatically issuing and renewing certificates for `a.com` and `*.a.com` on an nginx server. It assumes ssl-checker is [installed](installation.md) and `/health` returns `{"db":"ok","provider":"ok"}`.

Below, `<app>` is the api address that clients call. If you exposed it on a host port, it is `http://<BIND_ADDR>:<API_PORT>`; behind Caddy, it is the address configured in Caddy (e.g. `https://ssl-checker.example.com`). `<zone>` is the value of `CHALLENGE_ZONE`.

The API calls below can also be made from the [dashboard](dashboard.md). On the domain detail page, **Enable ACME** corresponds to step 1 and **Check CNAME now** to the check in step 2, and the dialog that appears right after enabling ACME contains the step 3 command with the values filled in. When delegating to an agent, point it to `<app>/llms.txt`.

## Overall flow

| Step | Who | How often |
| --- | --- | --- |
| 1. Register the domain and enable ACME | Administrator (holds `ADMIN_KEY`) | Once per domain |
| 2. Create the CNAME | DNS administrator of that domain | Once per domain |
| 3. Issue and install | Each server | Once per server |
| 4. Automatic renewal | acme.sh cron on each server | Automatic |

## 1. Register the domain and enable ACME

Create one ACME account per domain. This account handles both `a.com` and `*.a.com`. The `*.a.com` certificate covers all names one level below, such as `www.a.com` and `api.a.com`. If you need two levels below (`*.dev.a.com`), register `dev.a.com` separately.

Read `ADMIN_KEY` from input so that it does not end up in the shell history.

```bash
read -rs ADMIN_KEY      # paste and press Enter. Nothing is shown on screen
A='Authorization: Bearer '"$ADMIN_KEY"
J='Content-Type: application/json'

# Register the domain (if it already exists: 409 domain_exists — find the id in GET <app>/api/domains)
curl -s -X POST <app>/api/domains -H "$A" -H "$J" -d '{"fqdn":"a.com"}'
# Enable ACME with the "id" from the response
curl -s -X POST <app>/api/domains/<id>/acme -H "$A" -H "$J" \
  -d '{"allowfrom":["10.0.1.10/32","10.0.1.11/32"]}'
unset ADMIN_KEY A
```

- `allowfrom`: List of server IPs or CIDRs that may call `/update` with this account. If empty, it can be called from anywhere. If you know the server IPs, adding them is recommended. Behind a proxy or with rootless Docker, [check the source IP](configuration.md#network-and-proxies) first.

The response contains `username`, `password`, `subdomain`, `fulldomain`, the one-line CNAME record to create (`cname_record`), and the command the servers will run in step 3 (`setup`). The script contains no secrets.

The `password` is shown only this once and cannot be viewed again. It is a secret that lets anyone obtain certificates for this domain, so put it into your secret store right away. Do not paste it into tickets or chat. If you lose it, [rotate the key](operations.md#revoking-accounts-and-rotating-keys). The subdomain stays the same, so you do not need to set up the CNAME again.

The older `POST /register` (`{"domain":"a.com","allowfrom":[...]}`) still works as before. If the domain does not exist, it creates it and enables ACME.

## 2. Create the CNAME

Add one record to the DNS of `a.com`. This is the only time you touch the DNS of `a.com`.

```
_acme-challenge.a.com.  CNAME  <fulldomain>.
```

Example: `_acme-challenge.a.com. CNAME 8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example.`

Verify it. Right after creating it, resolver caches may delay it by a few minutes.

```bash
dig +short CNAME _acme-challenge.a.com @1.1.1.1
# 8e5700ea-a4bf-41c7-8a77-e990661dcc6a.acme-chk.example.
```

ssl-checker also checks via public DNS: it is fine when `cname_status` from `POST <app>/api/domains/<id>/acme/check` is `ok` (the worker also checks every `CHECK_INTERVAL`).

If `a.com` has a CAA record, `letsencrypt.org` must be in the allowed list.

```bash
dig +short CAA a.com
```

## 3. Issue and install

Run this on the server that will use the certificate. Use acme.sh **3.1.4 or later**. Older versions do not support ARI (renewal timing information).

### All at once with the install and setup script

ssl-checker serves the script. Copy the command from the dashboard's [How to use](dashboard.md) page or from the dialog shown when you enable ACME. The script contains no secrets and asks for values when it runs. On the server that will receive the certificate, this one line is all you need.

```bash
curl -fsSL <app>/acme/setup.sh -o setup.sh && bash setup.sh
```

The script proceeds in this order, and if something is wrong it explains why and stops before issuing.

1. **Check the credentials and allowed IP.** Prompts for the ssl-checker address (defaults to the address the script was downloaded from), the username and the password (input is hidden), and checks them. The subdomain is tied to the account and is supplied by the server, so you do not enter it. If this server's IP is not in the allowed IP list (`allowfrom`), the script shows that IP; have an administrator [add it to the allow list](api.md#post-acmeverify) and run the script again.
2. **Check acme.sh.** Checks that version 3.1.4 or later is installed, and installs it if it is missing or older (asks for an email address for expiry notices).
3. **Choose the web server and check its locations.** Detects installed nginx, apache and caddy, offers the detected one as the default, and checks the executable and configuration directory. **If none is detected or you want to reload it yourself, choose `manual`** and enter a reload script or command (e.g. `/usr/local/bin/reload-web.sh`, `systemctl reload haproxy`). A script given as a path is checked to be executable.
4. **Check the domain and CNAME.** Enter the domain to get a certificate for (`*.domain` is issued along with it), and the script queries public DNS (default 1.1.1.1) to check that the CNAME of `_acme-challenge.<domain>` points to the address given when ACME was enabled. If it is missing or different, the script shows the record to create (`_acme-challenge.a.com. CNAME <subdomain>.<challenge zone>.`); create it and press Enter to check again (DNS propagation can take a few minutes). Without a terminal the script stops, so create the record and run it again.
5. **Certificate directory.** Decides the directory to store the certificate in (default `/etc/ssl/<domain>`) and the reload command to run after installation. For Caddy, it also asks for the group that will read the key file.
6. **Issue and install.** Issues and stores the certificate, and registers a reload to run on every renewal.
7. **Check the web server configuration.** Checks whether the web server configuration already uses this certificate path, and if not, shows the configuration to add.
8. **Check renewal.** Warns if the reload command uses `sudo` and requires a password (see [Automatic renewal](#4-automatic-renewal)).

Values passed in advance as environment variables are not prompted for (for automation without a terminal). Values with a default can be omitted.

| Value | Environment variable | Default |
| --- | --- | --- |
| ssl-checker address | `SSLCHK_URL` | The address the script was downloaded from |
| username, password | `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD` | None |
| subdomain | `ACMEDNS_SUBDOMAIN` | The value supplied by the server. The server's value takes precedence |
| Email address for installing acme.sh | `ACME_EMAIL` | None (needed only when installing) |
| Web server | `WEB_SERVER` | The first detected web server, or `manual` if none (`nginx`, `apache`, `caddy`, `manual`) |
| Web server configuration directory | `WEB_CONF_DIR` | A common location per web server (`/etc/nginx`, etc.) |
| Skip the web server executable check | `SKIP_WEB_CHECK=1` | Checked |
| Additional places to look for the executable | `WEB_SEARCH_DIRS` | `/usr/sbin /usr/local/sbin /sbin /usr/local/bin /opt/homebrew/bin` |
| sudo prefix for reload and directory creation | `SUDO_CMD` | None for root, otherwise `sudo`. If empty, sudo is not used |
| Domain to get a certificate for | `DOMAIN` | None. Enter it without `*.`, like `example.com`. The wildcard is always issued along with it, and entering `*.example.com` is treated as `example.com` |
| Public resolver for the CNAME check | `DNS_RESOLVER` | `1.1.1.1` |
| Skip the CNAME check | `SKIP_CNAME_CHECK=1` | Checked. Use when public DNS cannot see the record, e.g. when only internal DNS is used |
| Directory to store the certificate in | `CERT_DIR` | `/etc/ssl/<domain>` |
| Reload script or command to run after installation | `RELOAD_CMD` | The web server's reload. None for `manual` |
| Group that reads the key file (Caddy only) | `CERT_GROUP` | `caddy` |

It is safe to run the script again on a server that already has a certificate (even if `--issue` ends with "not yet time to renew", it continues through installation). After rotating the key, you must run it as `FORCE=1 bash setup.sh` for the new key to be saved. Running it as `curl ... | bash` also works; in that case input is read from the terminal (`/dev/tty`).

The script is equivalent to running `~/.acme.sh/acme.sh --issue --dns dns_acmedns` (including the wildcard) and `--install-cert --reloadcmd`. To do it by hand, set `ACMEDNS_BASE_URL=<app>/acme` together with `ACMEDNS_USERNAME`, `ACMEDNS_PASSWORD` and `ACMEDNS_SUBDOMAIN` as environment variables and run these commands.

### Try staging first

For a domain you are connecting for the first time, try issuing with Let's Encrypt staging first. This avoids hitting the production CA's issuance rate limits. Do this before running the setup script.

```bash
ACMEDNS_BASE_URL=<app>/acme ACMEDNS_USERNAME=<username> ACMEDNS_SUBDOMAIN=<subdomain> \
  bash -c 'read -rsp "password: " ACMEDNS_PASSWORD; echo; export ACMEDNS_PASSWORD
  ~/.acme.sh/acme.sh --issue --staging --dns dns_acmedns -d a.com -d "*.a.com"'
```

When `Cert success.` appears, remove the staging certificate.

```bash
~/.acme.sh/acme.sh --remove -d a.com --ecc
rm -rf ~/.acme.sh/a.com_ecc
```

### Web server configuration

The script stores the certificate in `CERT_DIR` (default `/etc/ssl/<domain>`), and if the web server configuration does not use that path yet, it prints the configuration to add. To add it yourself, do the following.

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

For other web servers such as HAProxy, choose `manual` and enter a reload script. For HAProxy, use a script that combines the certificate and key into one file and then reloads; for a Docker container, use `docker kill -s HUP <container>`.

## 4. Automatic renewal

acme.sh registers a cron job when it is installed. When renewal time comes, it calls `/acme/update` again with the same credentials, installs the new certificate, and runs `--reloadcmd`. Nothing else needs to be done.

- acme.sh stores the credentials (`ACMEDNS_*`) in the domain configuration (`~/.acme.sh/a.com_ecc/a.com.conf`), so no environment variables are needed for renewal. This file contains the password in plain text, so control access to the server.
- Check renewal status: `~/.acme.sh/acme.sh --list`
- **If the reload uses `sudo`, it must work without a password.** Automatic renewal (cron) cannot enter a sudo password. If a password is required, the certificate files are renewed but the web server is not reloaded, so it keeps serving the old certificate. The script checks for this at the end and warns. On servers that are already set up, check it like this.

  ```bash
  # The stored reload command (check whether it contains sudo)
  sed -n "s/^Le_ReloadCmd='__ACME_BASE64__START_\(.*\)__ACME_BASE64__END_'/\1/p" ~/.acme.sh/a.com_ecc/a.com.conf | base64 -d; echo
  # Whether sudo works without a password (if this fails, the reload is blocked at renewal)
  sudo -k; sudo -n true && echo "works without a password" || echo "password required"
  ```

  There are three ways to fix it.
  1. **Install as root.** Become a root shell with `sudo -i` (or `su -`) and then run `setup.sh`. As root, the script drops `sudo` from the default reload command. acme.sh and the renewal cron are also registered under root, so there is no password problem. Avoid `sudo bash setup.sh`; depending on the environment, acme.sh and the cron job may end up installed for the regular account.
  2. Use `visudo` to allow only the reload command without a password (`<account> ALL=(root) NOPASSWD: /bin/systemctl reload nginx`). This grants narrower privileges than root, so it is safer.
  3. Use a reload script that works without sudo (e.g. if the account is in the docker group, `docker exec <container> caddy reload ...`).

  Save the changed command again with `~/.acme.sh/acme.sh --install-cert -d a.com --ecc --reloadcmd '<new command>'`. Reinstalling under a different account issues a new certificate, so keep Let's Encrypt's duplicate certificate rate limit (5 per week for the same names) in mind.
- During installation you may be asked for the `sudo` password once. This is to create the certificate directory when it cannot be written, and it is not needed for renewal (the directory is owned by the current account).
- Test a forced renewal: `~/.acme.sh/acme.sh --renew -d a.com --ecc --force`

## Adding a server to the same domain

Leave the account and CNAME as they are, and run only step 3 again on the new server. If the new server's IP is not in `allowfrom`, `/update` is rejected. Fix this by sending the whole list again as `{"allowfrom":[...]}` to `PATCH <app>/api/domains/<id>/acme`. The account and CNAME stay the same.

## If something goes wrong

See [Operations → Troubleshooting](operations.md#troubleshooting).
