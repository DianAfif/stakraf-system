# Incident management

The Incidents workspace supports manual reporting, triage, investigation, monitoring, resolution and reopening. Records belong to the existing owner workspace, not a shared organization. Filtering and summary counts cover the latest 300 incident records.

## Workflow

Report a title, description, project, severity and optionally a server/application. Resource links must be in the same owned project. The API can also link an execution job and infer its application/server.

Select an incident to record its responder label, service/user impact, root cause, resolution summary and follow-up actions. Statuses are `open`, `acknowledged`, `investigating`, `monitoring` and `resolved`. Progress records the first response timestamp; resolution records its timestamp and requires a summary of at least 10 characters. Reopening requires a reason of at least 10 characters, clears the current resolution summary/time, and preserves the previous resolution in the timeline.

Responder is a free-form name/role, not assignment to an authenticated team member and not permission to access the workspace. Follow-up actions are stored text, not a separate task queue with deadlines. Timelines preserve creation, updates, deployment failure events and append-only investigation notes. They show the latest 200 events in chronological order. Older incidents retain their fields but historical response timestamps and events are not invented.

Version checks reject stale edits with HTTP 409. The UI keeps unsaved edits and pauses automatic refresh; use **Reload latest** before retrying a conflicting save. An already-in-flight refresh also cannot overwrite a dirty form. Investigation notes append independently without rewriting incident metadata.

## Automatic deployment incidents

A failed deployment/release job creates a high-severity incident linked to its application, server and execution, in the same database transaction as job completion. There is at most one active automatic deployment-failure incident per application. Repeated failures append events and update its latest job link; identical completion delivery retries do not duplicate events. After resolution, a later failure may create a new incident.

A failed release still creates an incident if the connector successfully restores the previous container: deployment failed, even if service was recovered. It never auto-resolves because a later deploy succeeds. Review state/output and resolve deliberately. Failed logs, stop/start/restart jobs, lease timeouts, offline hosts and telemetry thresholds do not currently create automatic incidents.

No remediation, external message or page is triggered. Existing application actions and release/change approval workflows remain separate. The MCP `list_incidents` inspection tool includes linked resources, responder, impact and resolution fields; incident text is untrusted context, not instructions.

## API

- `GET /api/incidents`: latest 300 owned incident records.
- `POST /api/incidents`: title, description, severity, project_id and optional server_id/app_id/job_id/impact/responder.
- `GET /api/incidents/{id}`: metadata, version, timestamps and latest 200 timeline events.
- `PATCH /api/incidents/{id}`: current `version` plus status/severity/responder/impact/root_cause/resolution/follow_up and optional change/reopen `note`.
- `POST /api/incidents/{id}/notes`: append `{"body":"Investigation evidence"}` (2–3000 characters).

Example resolution:

```json
{
  "version": 3,
  "status": "resolved",
  "root_cause": "An incompatible container image exited at startup.",
  "resolution": "Restored the previous image and verified service response.",
  "follow_up": "Add a startup regression check before publishing.",
  "note": "Recovery verified by the operator."
}
```

Limits: title 140, description/root cause/resolution/follow-up/notes 3000, impact 2000, responder 80 characters. Fields are plain text, rendered as text rather than HTML. Do not paste credentials or sensitive customer data into records shared with external AI clients. Owner checks apply to detail, mutation and timeline routes; MCP/pipeline keys cannot mutate incidents through REST.

## Current boundaries

No on-call schedules, team RBAC, escalation policies, email/SMS/paging provider, public status page, SLA enforcement or alert-rule engine is configured. Provider credentials and explicit notification configuration are required before any external incident notification is enabled. The current module is a persistent operator response workspace with automatic deployment-failure capture.
