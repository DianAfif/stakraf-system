# Stakraf API routes

All `/api/` routes except `health`, `auth/signup`, `auth/login`, and `agent/enroll` require an authenticated session cookie or bearer `stk_...` API key. Mutations are origin-checked for browser requests. Agent calls use separate `sha_...` bearer credentials.

| Method | Route | Description |
| --- | --- | --- |
| GET | `/api/health` | Database readiness and API status |
| POST | `/api/auth/signup`, `/api/auth/login`, `/api/auth/logout` | Session auth |
| GET | `/api/auth/me` | Current user |
| GET/POST | `/api/projects` | List/create projects |
| GET/POST | `/api/servers` | List/create servers, issue enrollment token |
| POST | `/api/servers/{id}/enrollment` | Reissue enrollment if not enrolled |
| GET/POST | `/api/apps` | List/create container applications |
| POST | `/api/apps/{id}/actions` | Queue read-only `logs` job |
| GET | `/api/jobs` | Query execution history |
| POST | `/api/jobs/{id}/cancel` | Cancel queued work before host execution |
| POST | `/api/jobs/{id}/retry` | Create a linked attempt for failed/cancelled deploys or failed/cancelled/timed-out log reads |
| GET | `/api/overview` | Aggregated fleet counts |
| GET | `/api/telemetry?server_id=...` | Past 24 hours sampled host metrics |
| GET/POST | `/api/incidents` | List/create incidents |
| PATCH | `/api/incidents/{id}` | Set `open`, `acknowledged` or `resolved` |
| GET/POST | `/api/changes` | List/request controlled app operations |
| POST | `/api/changes/{id}/decision` | Approve or reject pending change |
| GET | `/api/audit` | Account-scoped audit events |
| GET/POST | `/api/keys` | List/create API keys |
| DELETE | `/api/keys/{id}` | Revoke key |
| GET/PATCH | `/api/settings` | Workspace preferences |
| POST | `/mcp` | MCP-compatible JSON-RPC HTTP endpoint with bearer API key |

## MCP example

```bash
curl https://console.example.com/mcp \
  -H 'Authorization: Bearer stk_YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
```

Supported tools: `list_projects`, `list_servers`, `list_applications`, `list_incidents`, `get_operations_summary`.

### Approval model

Submitting `POST /api/changes` creates a **pending** request; execution does not occur until `POST /api/changes/{id}/decision` with `{"decision":"approve"}` commits a job. New Docker deployments submitted from the deploy form are explicit user-authorized operations and go directly to the queue. No automatic AI execution.

### Execution lifecycle

Each host claims at most one running job, including concurrent polls. Claims have a five-minute execution window; the agent limits Docker execution to four minutes and reports heartbeats independently. `/api/jobs` returns the latest 200 records, including start/finish timestamps, execution-window expiry, project and host identity, image, and the `retry_of` link.

Expired work becomes `timed_out` when execution history is loaded or the host polls. Mutating operations then set the application state to `unknown`. They are never automatically replayed; a timeout does not establish whether Docker acted. Late results are rejected. Identical completed results are acknowledged again to tolerate response loss, while conflicting results are rejected. Job completion and application state changes commit together.

Cancellation applies only to `queued` work and races safely with host claims. A retry creates a new record and preserves the previous output; each source job has at most one retry. The application must have no active jobs. Controlled restart/stop/start operations require a fresh change request and approval. Timed-out deployments cannot be retried directly, and existing containers are not automatically replaced.
