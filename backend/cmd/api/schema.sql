CREATE TABLE IF NOT EXISTS users (
 id TEXT PRIMARY KEY,
 email TEXT UNIQUE NOT NULL,
 password_hash TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS projects (
 id TEXT PRIMARY KEY,
 owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS servers (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 enrollment_hash TEXT,
 enrollment_expires TIMESTAMPTZ,
 agent_token_hash TEXT UNIQUE,
 hostname TEXT NOT NULL DEFAULT '',
 os TEXT NOT NULL DEFAULT '',
 netbird_ip TEXT NOT NULL DEFAULT '',
 netbird_status TEXT NOT NULL DEFAULT 'unknown',
 last_seen TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS apps (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 name TEXT NOT NULL,
 image TEXT NOT NULL,
 host_port INTEGER NOT NULL CHECK(host_port BETWEEN 1024 AND 65535),
 container_port INTEGER NOT NULL CHECK(container_port BETWEEN 1 AND 65535),
 status TEXT NOT NULL DEFAULT 'pending',
 last_error TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(server_id,host_port),
 UNIQUE(server_id,name)
);
CREATE TABLE IF NOT EXISTS jobs (
 id TEXT PRIMARY KEY,
 server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('deploy','restart','stop','start','logs')),
 payload JSONB NOT NULL DEFAULT '{}'::jsonb,
 status TEXT NOT NULL DEFAULT 'queued',
 output TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS jobs_server_queue ON jobs(server_id, status,created_at);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);

-- Stakraf operations modules. Schema is additive; existing installations retain data.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_kind_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_kind_check CHECK(kind IN ('deploy','restart','stop','start','logs'));
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS retry_of TEXT REFERENCES jobs(id);
CREATE UNIQUE INDEX IF NOT EXISTS jobs_retry_once ON jobs(retry_of) WHERE retry_of IS NOT NULL;
CREATE INDEX IF NOT EXISTS jobs_execution_window ON jobs(lease_expires_at) WHERE status='running';
-- Older running jobs get a bounded grace period during upgrade.
UPDATE jobs SET lease_expires_at=now()+interval '5 minutes' WHERE status='running' AND lease_expires_at IS NULL;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS cpu_percent DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS memory_percent DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS disk_percent DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS agent_version TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS docker_ready BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS docker_version TEXT NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS docker_error TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS telemetry (
 server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
 recorded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 cpu DOUBLE PRECISION NOT NULL,
 memory DOUBLE PRECISION NOT NULL,
 disk DOUBLE PRECISION NOT NULL,
 PRIMARY KEY(server_id,recorded_at)
);
CREATE INDEX IF NOT EXISTS telemetry_server_time ON telemetry(server_id, recorded_at DESC);
CREATE TABLE IF NOT EXISTS incidents (
 id TEXT PRIMARY KEY,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 server_id TEXT REFERENCES servers(id) ON DELETE SET NULL,
 title TEXT NOT NULL,
 description TEXT NOT NULL DEFAULT '',
 severity TEXT NOT NULL CHECK(severity IN ('critical','high','medium','low')),
 status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','acknowledged','resolved')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS incident_project ON incidents(project_id,status,created_at DESC);
CREATE TABLE IF NOT EXISTS change_requests (
 id TEXT PRIMARY KEY,
 app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
 project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
 kind TEXT NOT NULL CHECK(kind IN ('restart','stop','start')),
 reason TEXT NOT NULL DEFAULT '',
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 requested_by TEXT NOT NULL REFERENCES users(id),
 decided_by TEXT REFERENCES users(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 decided_at TIMESTAMPTZ,
 job_id TEXT REFERENCES jobs(id)
);
CREATE TABLE IF NOT EXISTS audit_events (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
 event_type TEXT NOT NULL,
 resource_type TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 details TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_user_time ON audit_events(user_id,created_at DESC);
CREATE TABLE IF NOT EXISTS api_keys (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 label TEXT NOT NULL,
 token_hash TEXT UNIQUE NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 last_used_at TIMESTAMPTZ,
 revoked_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS api_keys_owner ON api_keys(user_id,revoked_at);
ALTER TABLE apps ADD COLUMN IF NOT EXISTS pipeline_token_hash TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS apps_pipeline_token ON apps(pipeline_token_hash) WHERE pipeline_token_hash IS NOT NULL;
CREATE TABLE IF NOT EXISTS app_releases (
 id TEXT PRIMARY KEY,
 app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
 image TEXT NOT NULL,
 commit_sha TEXT NOT NULL DEFAULT '',
 note TEXT NOT NULL DEFAULT '',
 request_id TEXT NOT NULL,
 status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','rejected','queued')),
 job_id TEXT UNIQUE REFERENCES jobs(id),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 decided_at TIMESTAMPTZ,
 UNIQUE(app_id,request_id)
);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'api' CHECK(scope IN ('api','mcp_read','mcp_propose'));
ALTER TABLE change_requests ADD COLUMN IF NOT EXISTS mcp_request_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS changes_mcp_request ON change_requests(requested_by,mcp_request_id) WHERE mcp_request_id IS NOT NULL;
CREATE TABLE IF NOT EXISTS workspace_settings (
 user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
 name TEXT NOT NULL DEFAULT 'My workspace',
 timezone TEXT NOT NULL DEFAULT 'UTC',
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
