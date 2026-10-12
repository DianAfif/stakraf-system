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
