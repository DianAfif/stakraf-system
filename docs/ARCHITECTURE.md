# Architecture & risk boundaries

- **Go HTTP API:** owners scoped at SQL joins, sessions with HTTP-only SameSite=Strict cookies, login bcrypt, SHA-256 token hashes for randomly generated high-entropy keys.
- **PostgreSQL:** transaction-backed project/app/job creation, incident and approvals tables, telemetry history, audit trail, API key registry. SQL migrations are additive startup DDL; introduce versioned migrations before a managed hosted launch.
- **Go agent:** outward agent enrollment and authenticated polling, bounded fixed Docker operations (deploy/start/stop/restart/logs), host telemetry from Linux procfs, NetBird CLI status observation only. Docker permissions remain privileged: agent should be restricted to trusted owned hosts.
- **React UI:** real API calls, cookies, periodic 12-second refresh, navigation, operator review, no simulation of connectivity or status.
- **MCP:** HTTP JSON-RPC `initialize`, `tools/list`, `tools/call` over bearer API key; read-only tools. Other MCP features such as SSE sessions, resumability, notifications, and OAuth discovery are not implemented; some clients may need an adapter.
- **NetBird:** Separate product/tenant. Stakraf does not handle NetBird credential lifecycle or install NetBird; only status and declared private IP observation. Permissions and relay/bandwidth belong to NetBird.

Security backlog before production: login rate-limiting, password reset/MFA, tenant and role model, host certificate / identity rotation, HTTPS enforcement, reverse proxy, CSP and CORS hardening, secrets vault, registry credential management, automatic CSRF protection beyond SameSite and Origin checks, immutable audit log export, backup and restore tests, blue/green rollout, signed images, SAST/DAST, load tests, dependency scanning, observability alerting, security review and DR runbooks.
