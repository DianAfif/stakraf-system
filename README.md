# Stakraf System

**Start crafting more.**

Stakraf System is a self-hostable infrastructure operations control plane built with **Go, React and PostgreSQL**. It connects user-owned Linux servers, manages container workloads, collects real host telemetry and centralizes operational workflows. It does **not** contain an AI model or chatbot: supported external AI clients can read authorized infrastructure data through an MCP endpoint.

[GitHub repository](https://github.com/DianAfif/stakraf-system) · [Infrastructure command design](docs/DESIGN.md) · [Architecture](docs/ARCHITECTURE.md) · [API reference](docs/API.md)

## What works

| Area | Available functionality |
| --- | --- |
| Account and workspace | Account sign-up/sign-in, 7-day HttpOnly sessions, workspace settings |
| Projects | Create projects, scope infrastructure to project owners |
| Server management | Single-use expiring enrollment keys, per-host Go agents, heartbeat and status, NetBird observation |
| Server connector | Bundled x86_64/ARM64 downloads, checksum verification, Linux/systemd installer, Docker readiness, container inventory, disconnect/reconnect |
| Application deployment | Deploy existing container images, serialize host jobs, track execution windows, cancel queued work, retry eligible failures, and preserve attempt history |
| Telemetry | CPU/memory/disk metrics captured by host agent, 60s PostgreSQL samples, 24h trend charts |
| Incidents | Create, acknowledge, resolve, reopen, severity assignment and project ownership |
| Change control | Request app start/stop/restart, explicit approve/reject, queue only after approval |
| Governance | Append-only audit events for major user actions, API key issuance and revocation |
| External AI | Read-only JSON-RPC MCP HTTP endpoint (projects, infrastructure, apps, incidents, operations) via API keys |
| UI | White and graphite operations console, filtered execution history, job timelines/output, recovery controls, and keyboard quick switch (Ctrl/Cmd+K) |
| CI/CD | GitHub Actions: Go tests/vet/build, React production build, Docker/PostgreSQL control-plane smoke checks |

**Not completed:** Cloud-provider purchasing/provisioning, managed NetBird tenant provisioning, public HTTPS/ACME automation, source-to-image CI, multi-user RBAC/organizations, backups/rollback orchestration, automatic repair, SMS/email integration, billing, and production-grade security hardening. The platform is a wide **operational foundation**, not a production-complete managed service. Do not use it to operate critical infrastructure until security and recovery processes are independently reviewed.

## Quick start

For `console.stakraf.com`, or self-hosting under your own hostname, use `bash scripts/setup.sh --domain YOUR_DOMAIN`. See [installation, connection, backup, and recovery](docs/OPERATIONS.md). The production stack includes HTTPS, persistent storage, a first-account setup code, and registration closure after initial setup.

Requirements: Docker Engine, Docker Compose plugin. For agent machines: Linux and Docker Engine; NetBird optional.

```bash
cp .env.example .env
# Set POSTGRES_PASSWORD to a strong, random value; configure APP_ORIGIN for deployment.
# Set STAKRAF_SETUP_TOKEN to a random secret for first-account setup.
docker compose up --build -d
# Open http://localhost:3000
```

Default bind address is `127.0.0.1`, intentionally not exposed publicly. For internet deployment configure a TLS terminating reverse proxy, use `APP_ORIGIN=https://your-domain` and use safe firewall rules. Consider a TLS proxy at the public endpoint and configure the Stakraf agent to use **only HTTPS**. Do not expose the database or API container publicly.

Create an account (password >=12 characters, with your setup code), then create a project and register a server. The connection guide installs the packaged connector without a Go compiler on the target host. Manual source installation remains available:

```bash
cd backend
go build -o stakraf-agent ./cmd/agent
export STAKRAF_API_URL='https://console.example.com'
export STAKRAF_ENROLLMENT_TOKEN='<ONE_TIME_TOKEN_FROM_CONSOLE>'
export STAKRAF_STATE_FILE='/var/lib/stakraf/agent.json'
./stakraf-agent
```

The Go agent needs Docker permissions. **Docker socket access is highly privileged**; run only on explicitly authorized hosts and configure a scoped service identity, host restrictions and security monitoring. Its state file contains the agent credential; permissions are set to `0600`. The agent polls a restricted set of Docker actions; it never executes arbitrary AI-supplied shell commands. Initial deployments bind to `127.0.0.1` on the host by default. For a public app, configure a vetted reverse proxy explicitly rather than exposing it accidentally.

The backend uses Go `pgx` and `bcrypt`, and the UI uses React and Vite. Go checksums and the npm lockfile keep local, CI, and Docker dependency installs reproducible. No secrets are included in the repository.

## Integration architecture

To verify an isolated running test stack, run `node scripts/smoke.mjs` (Node 22+). Override `STAKRAF_SMOKE_URL` when needed. This creates test accounts and resources and checks authentication, single-use enrollment, telemetry, approval gates, incidents, settings, audit records, and MCP key revocation. It does not execute Docker jobs on a target host. CI uses a disposable database for this check.

The deployment console shows recent execution metrics, filters by state/project/host/operation, and opens a job inspector with lifecycle timestamps and downloadable output. Queued jobs can be cancelled. Eligible retries are linked to the original job; controlled operations need fresh approval. Host heartbeats continue during long operations. Work without a confirmed result is marked for inspection rather than automatically replayed. See [execution lifecycle](docs/API.md#execution-lifecycle).

```
       Browser           ChatGPT / Codex / external MCP clients
          |                              |
       HTTPS cookie                 Bearer API key
          |                              |
          +----------- Go API / MCP -----+
                           |
                        PostgreSQL
                           |
                        Agent jobs
                           |
          Linux Stakraf Agent (outbound polling)
                     |               |
                  Docker          NetBird CLI
```

The control plane stores only one-way hashes of enrollment keys, agent bearer credentials, and API keys. NetBird peering must be configured separately; Stakraf shows reported connectivity, not an integrated NetBird control plane.

## Product identity

**Company / Product:** Stakraf · **Tagline:** Start crafting more. · **Positioning:** Infrastructure, deployment and operations in one control plane. See `docs/BRAND.md`.

## License

No open-source license is granted by default. All rights reserved until a project license is selected. Review dependency licenses before distributing as a commercial product.
