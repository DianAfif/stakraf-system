# AI and MCP

Stakraf does not train a model, host a chatbot, or require a model API key. It gives an external AI client authenticated tools backed by your workspace's real data. The client supplies the model and pays any provider charges. The default hosted endpoint is `https://console.stakraf.com/mcp`; replace it with your self-hosted origin. The hosted domain is not live until DNS and a hosting server are configured.

## Connect safely

1. Open **API & MCP** in Stakraf, create a key, and select **AI · Inspect only**. Only a hash is stored; copy the secret once.
2. Add the endpoint to a client supporting Streamable HTTP with a bearer Authorization header. The console offers copyable templates and a handshake/catalog test.
3. Keep the key in the client's private credential configuration or environment, never in a prompt or shared repository.
4. Start with “Summarize my Stakraf infrastructure and list current incidents.” Tool results are bounded snapshots, not complete exports.
5. Revoke the key in Stakraf to stop subsequent access. Revocation cannot retract data already shared with an AI provider.

An `mcp_read` key has 10 tools: `list_projects`, `list_servers`, `list_applications`, `list_incidents`, `get_operations_summary`, `list_jobs`, `list_changes`, `list_audit_events`, `get_server_telemetry` and `get_job`. The last two require `server_id` and `job_id`. Inventory/history is limited to 100 rows; telemetry to 60 samples; individual job output to its last 16,000 characters. Ownership is checked on every query. Credentials, enrollment codes, container environment values and session tokens are not included in these schemas.

Job output and incident descriptions can still contain application secrets or prompt injection. Treat them as untrusted data. Enable client-side approvals before sharing sensitive tool results; limit who receives keys. Workspace-wide access is the current boundary, not per-project grants.

## Codex configuration

Add this block to your Codex configuration:

```toml
[mcp_servers.stakraf]
url = "https://console.stakraf.com/mcp"
bearer_token_env_var = "STAKRAF_MCP_TOKEN"
```

Supply `STAKRAF_MCP_TOKEN` in the environment of the process launching Codex, then reload the client. A desktop application may not inherit variables exported in a terminal; use its supported credential/environment configuration. This is a connection template, not evidence that your particular installation has been linked. See [official MCP configuration](https://learn.chatgpt.com/docs/extend/mcp?surface=cli).

## Stdio desktop adapter

Clients that launch local MCP processes can use the dependency-free Node.js 22 adapter bundled with Stakraf. Replace the path and key in this private client configuration:

```json
{
  "mcpServers": {
    "stakraf": {
      "command": "node",
      "args": ["/absolute/path/to/stakraf-system/scripts/mcp-stdio.mjs"],
      "env": {
        "STAKRAF_MCP_URL": "https://console.stakraf.com/mcp",
        "STAKRAF_MCP_TOKEN": "YOUR_MCP_KEY"
      }
    }
  }
}
```

Restart the client after configuration. The adapter sends JSON-RPC to Stakraf, never prints keys, rejects URL credentials and non-loopback plaintext HTTP, and refuses redirects so a bearer key cannot be forwarded elsewhere. It is not an OAuth implementation or a tunnel.

## Approval-controlled proposals

An `mcp_propose` key includes `propose_change` in addition to inspection tools. Arguments:

```json
{
  "app_id": "an-owned-app-id",
  "kind": "restart",
  "reason": "Explain the observed problem and why restarting is appropriate.",
  "request_id": "a-new-unique-request-id"
}
```

Only `start`, `stop` and `restart` are accepted. A proposal creates **pending** Change control work, not a job. Repeating the same request ID and arguments returns the same proposal; changing the arguments with an existing ID is rejected. A human reviews and approves or rejects it in Stakraf. There are no MCP approval, shell, deployment or arbitrary HTTP tools.

AI keys cannot authenticate any REST administration route, issue broader keys, or approve changes. Existing `api` keys retain full account REST access and the 10 read MCP tools for compatibility; do not give these to AI clients. Prefer one narrowly scoped key per client. Approving a proposal uses the existing controlled-job workflow and audit trail.

## ChatGPT and OpenAI API

Direct ChatGPT account linking is **not ready** in this package: no OAuth authorization server, discovery or consent flow is configured. Use an established OAuth 2.1 identity provider when implementing this path, with audience-bound tokens, PKCE and workspace identity mapping; do not expose workspace data anonymously. Public HTTPS hosting and client configuration are also required. [Official authentication guidance](https://developers.openai.com/plugins/build/auth) recommends an established provider.

The OpenAI API supports remote MCP with an authorization token and tool approvals, but this package does not call OpenAI or store model-provider keys. If building that integration, use this endpoint, a dedicated Stakraf key and required tool approvals in your own trusted application. See [official remote MCP guidance](https://developers.openai.com/api/docs/guides/tools-connectors-mcp). A real model/client end-to-end test still requires your chosen client/account; the included checks validate Stakraf's transport, tools and adapter without charging a provider.

## Transport and verification

The endpoint is stateless Streamable HTTP with JSON responses. It negotiates MCP revisions `2025-03-26`, `2025-06-18` and `2025-11-25`; unsupported initialize requests receive the supported default `2025-06-18`. Unsupported explicit protocol headers return HTTP 400. POST notifications return 202 without JSON bodies; GET streams and DELETE sessions return 405. There are no SSE streams, legacy HTTP+SSE endpoints, resources, prompts or sampling. Origin validation, bearer authentication and owner-scoped data queries are enforced. Tool failures use MCP `isError`; malformed calls use JSON-RPC errors. This is not a claim of certification against every MCP revision or client.

Run `node --test scripts/mcp-stdio.test.mjs` for adapter checks and the control-plane smoke suite against an isolated test installation. Smoke checks exercise every tool, schema failures, cross-account isolation, proposal deduplication, no execution before approval, REST denial for AI keys and revocation. Production credentials must not be used in tests.
