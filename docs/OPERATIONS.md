# Installation and operations

## Hosted or self-hosted console

Use a Linux server with Docker Engine, Compose, and openssl. For the hosted default, point `console.stakraf.com` to the server's public IP and allow TCP ports 80/443. The root domain can remain your main website. Only add an AAAA record if IPv6 reaches the same host.

```sh
bash scripts/setup.sh --domain console.stakraf.com
```

The script generates a private `.env` with random database and first-account setup secrets, validates configuration, and starts the production stack. Caddy obtains/renews HTTPS certificates when public DNS and ports are reachable. PostgreSQL and certificate state persist in named volumes. Existing `.env` files are preserved. No database or API port is exposed publicly.

Open the HTTPS console, create the first account, and use `STAKRAF_SETUP_TOKEN` from the private `.env` as its setup code. Registration then closes. Use a password of 12–72 characters and keep it securely. `STAKRAF_SIGNUP_MODE=open` intentionally permits independent accounts; it does not provide team roles. First-account registration cannot succeed without the secret setup code.

Self-hosting uses the same command with your own hostname. API, MCP, installer, and agent downloads share that origin. Connectors use your configured URL; self-hosting does not require the hosted Stakraf domain.

## Connect a Linux server

Create a project, choose **Connect server**, and enter the console URL reachable from the target. Download/review the installer and run its displayed command. Paste the expiring enrollment token at the hidden-input prompt. The console checks the resulting heartbeat and Docker readiness automatically.

Supported hosts: Linux x86_64 or ARM64, systemd, and a running Docker Engine. The installer creates a service account in the `docker` group; Docker access permits control of host containers. It installs a verified binary at `/usr/local/bin/stakraf-agent`, configuration at `/etc/stakraf/agent.env`, a mode-0600 credential at `/var/lib/stakraf/agent.json`, and `stakraf-agent.service`. Tokens are not embedded in install commands or service configuration. Agents connect outward over HTTPS; Stakraf does not require a public SSH port.

Re-running the installer preserves enrollment and restarts the service. To reconnect, disconnect the host in its inspector, issue a new token, and run with `--reconnect`. The old local credential is moved to a dated `.revoked` file; delete it after confirming reconnection. Disconnect from the previous console before changing consoles.

```sh
sudo systemctl status stakraf-agent --no-pager
sudo journalctl -u stakraf-agent -n 50 --no-pager
sudo -u stakraf docker info
sudo -u stakraf env STAKRAF_API_URL=https://console.stakraf.com \
  STAKRAF_STATE_FILE=/var/lib/stakraf/agent.json /usr/local/bin/stakraf-agent --check
```

Disconnecting revokes access and cancels queued jobs. An operation already started may finish on the host; inspect it before repeating. Existing containers remain. To stop the local connector afterward, run `sudo systemctl disable --now stakraf-agent`. NetBird is optional and installed independently; its status is observed when available.

## Workloads

Choose an online server reporting Docker readiness. The example image is `nginxinc/nginx-unprivileged:stable-alpine` on container port 8080. Containers run with capabilities dropped, no privilege escalation, 512 MB memory, one CPU, and a 128-process limit. Use images compatible with those restrictions. Host ports bind to loopback; configure an application reverse proxy explicitly for public traffic.

The connector reports real container states, including externally stopped or missing containers, and host metrics. Heartbeats continue during long operations. Job completion confirms Docker execution, not universal application health; use the application's own monitoring. Restart/stop/start require approval. Unknown outcomes after execution expiry require inspection and are not automatically replayed.

## Backups and recovery

```sh
bash scripts/backup.sh
```

The script writes a private PostgreSQL archive and validates its archive structure. Store an encrypted copy off the server; retain private `.env`, TLS volume backups, and each agent's state separately. Schedule regular backups and test restores.

Restore into a fresh database on a separate recovery/test installation first:

```sh
docker compose -f docker-compose.production.yml exec -T postgres \
  pg_restore -U stakraf -d YOUR_FRESH_DATABASE --exit-on-error < backups/YOUR_ARCHIVE.dump
```

Do not restore over an operating database. Stop the API for a planned cutover and configure its database URL to the recovered database. Adjust the username if you configured a custom one.

## Updates and checks

If the operator loses a password, a console-host administrator can reset it without email infrastructure. Read the new password without putting it in shell history:

```bash
read -r -s -p 'New password: ' STAKRAF_RECOVERY_PASSWORD
printf '%s\n' "$STAKRAF_RECOVERY_PASSWORD" | docker compose -f docker-compose.production.yml \
  exec -T api /api --reset-password operator@example.com
unset STAKRAF_RECOVERY_PASSWORD
```

Replace the email with the existing account. Browser sessions are revoked and the reset is audited; API keys remain valid and can be revoked separately from the console.

Back up first, rebuild from a verified revision, and update connectors by re-running the installer.

```sh
docker compose -f docker-compose.production.yml ps
docker compose -f docker-compose.production.yml logs --tail 100 api gateway
curl --fail https://console.stakraf.com/api/health
```

Authentication throttling uses the API peer IP; behind the bundled proxy it is a shared limit. Shared teams/RBAC, managed cloud provisioning, billing, reset-email/MFA, source builds, and automatic application HTTPS are separate capabilities and are not represented as implemented by this package.
