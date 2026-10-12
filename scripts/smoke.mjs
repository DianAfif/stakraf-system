// Run only against an isolated test workspace; this creates persistent test data.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { execFileSync } from 'node:child_process';

const base = process.env.STAKRAF_SMOKE_URL || 'http://localhost:3000';
const origin = new URL(base).origin;
let cookie = '';
async function request(path, { method = 'GET', body, token, anonymous = false, status = 200 } = {}) {
  const response = await fetch(base + path, {
    method,
    headers: {
      'Content-Type': 'application/json', Origin: origin,
      ...(token ? { Authorization: `Bearer ${token}` } : !anonymous && cookie ? { Cookie: cookie } : {}),
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(10000),
  });
  // Do not include response bodies in failures: enrollment and key responses contain secrets.
  assert.equal(response.status, status, `${method} ${path}: expected ${status}, received ${response.status}`);
  const session = response.headers.get('set-cookie');
  if (session && !anonymous) cookie = session.split(';')[0];
  return response.json();
}

assert.equal((await request('/api/health', { anonymous: true })).status, 'ok');
await request('/api/auth/me', { anonymous: true, status: 401 });
const credentials = { email: `smoke-${randomUUID()}@example.test`, password: randomUUID() };
await request('/api/auth/signup', { method: 'POST', body: credentials });
assert.equal((await request('/api/auth/me')).email, credentials.email);
const project = await request('/api/projects', {
  method: 'POST', status: 201, body: { name: 'CI smoke project', description: 'Isolated control-plane verification' },
});
const server = await request('/api/servers', {
  method: 'POST', status: 201, body: { project_id: project.id, name: 'ci-host' },
});
const enrollment = { token: server.enrollment_token, hostname: 'ci-host', os: 'linux' };
const agent = await request('/api/agent/enroll', { method: 'POST', anonymous: true, body: enrollment });
await request('/api/agent/enroll', { method: 'POST', anonymous: true, body: enrollment, status: 401 });
await request('/api/agent/heartbeat', {
  method: 'POST', token: agent.agent_token,
  body: { hostname: 'ci-host', os: 'linux', cpu: 25, memory: 40, disk: 15, agent_version: 'smoke', netbird_status: 'unknown' },
});
assert.equal((await request(`/api/telemetry?server_id=${server.id}`))[0].cpu, 25);
const app = await request('/api/apps', {
  method: 'POST', status: 201,
  body: { project_id: project.id, server_id: server.id, name: 'ci-app', image: 'nginx:alpine', host_port: 8088, container_port: 80 },
});
// Exercise approval persistence; host execution is simulated via its API.
for (const kind of ['restart', 'stop', 'start']) {
  const before = (await request('/api/jobs')).length;
  const change = await request('/api/changes', {
    method: 'POST', status: 201, body: { app_id: app.id, kind, reason: 'Smoke check' },
  });
  assert.equal(change.status, 'pending');
  assert.equal((await request('/api/jobs')).length, before, 'Pending changes must not queue work');
  const decision = await request(`/api/changes/${change.id}/decision`, { method: 'POST', body: { decision: 'approve' } });
  assert.equal(decision.status, 'approved');
  assert.ok((await request('/api/jobs')).some(job => job.id === decision.job_id && job.kind === kind));
  await request(`/api/changes/${change.id}/decision`, { method: 'POST', body: { decision: 'approve' }, status: 404 });
}
const rejected = await request('/api/changes', {
  method: 'POST', status: 201, body: { app_id: app.id, kind: 'restart', reason: 'Reject check' },
});
const beforeReject = (await request('/api/jobs')).length;
assert.equal((await request(`/api/changes/${rejected.id}/decision`, { method: 'POST', body: { decision: 'reject' } })).status, 'rejected');
assert.equal((await request('/api/jobs')).length, beforeReject);
const poll = () => request('/api/agent/jobs/next', { token: agent.agent_token });
const complete = (id, success, output, status = 200) => request(`/api/agent/jobs/${id}/complete`, {
  method: 'POST', token: agent.agent_token, body: { success, output }, status,
});
const concurrent = await Promise.all(Array.from({ length: 6 }, poll));
assert.equal(concurrent.filter(result => result.job).length, 1, 'Concurrent polls must claim only one job per host');
const first = concurrent.find(result => result.job).job;
assert.equal(first.kind, 'deploy');
const running = (await request('/api/jobs')).find(job => job.id === first.id);
assert.ok(running.started_at && running.lease_expires_at, 'Running jobs must have timing and an execution window');
await request(`/api/jobs/${first.id}/cancel`, { method: 'POST', status: 409 });
await complete(first.id, true, 'Simulated deployment complete');
await complete(first.id, true, 'Simulated deployment complete'); // Response-loss retry is idempotent.
await complete(first.id, false, 'Conflicting result', 409);
const pendingRestart = (await request('/api/jobs')).find(job => job.kind === 'restart');
await request(`/api/jobs/${pendingRestart.id}/cancel`, { method: 'POST' });
await request(`/api/jobs/${pendingRestart.id}/retry`, { method: 'POST', status: 409 });
assert.equal((await request('/api/jobs')).find(job => job.id === pendingRestart.id).status, 'cancelled');
for (let attempt = 0; attempt < 5; attempt++) {
  const result = await poll();
  if (!result.job) break;
  await complete(result.job.id, true, 'Simulated controlled operation complete');
}
const retryApp = await request('/api/apps', {
  method: 'POST', status: 201,
  body: { project_id: project.id, server_id: server.id, name: 'ci-retry', image: 'nginx:alpine', host_port: 8089, container_port: 80 },
});
const failed = (await poll()).job;
assert.equal(failed.id, retryApp.job_id);
await complete(failed.id, false, 'Simulated image pull failure');
const retried = await request(`/api/jobs/${failed.id}/retry`, { method: 'POST' });
await request(`/api/jobs/${failed.id}/retry`, { method: 'POST', status: 409 });
assert.equal((await request('/api/jobs')).find(job => job.id === retried.job_id).retry_of, failed.id);
const retryClaim = (await poll()).job;
assert.equal(retryClaim.id, retried.job_id);
await complete(retryClaim.id, true, 'Simulated retry complete');
const logs = await request(`/api/apps/${app.id}/actions`, { method: 'POST', status: 201, body: { kind: 'logs' } });
await request(`/api/jobs/${logs.job_id}/cancel`, { method: 'POST' });
const retryLogs = await request(`/api/jobs/${logs.job_id}/retry`, { method: 'POST' });
assert.equal((await poll()).job.id, retryLogs.job_id);
await complete(retryLogs.job_id, true, 'Simulated container output');
// CI can expire a fixture in its disposable PostgreSQL database without a five-minute wait.
if (process.env.STAKRAF_SMOKE_EXPIRE_FIXTURE === 'true') {
  const uncertain = await request('/api/apps', {
    method: 'POST', status: 201,
    body: { project_id: project.id, server_id: server.id, name: 'ci-timeout', image: 'nginx:alpine', host_port: 8090, container_port: 80 },
  });
  assert.equal((await poll()).job.id, uncertain.job_id);
  assert.match(uncertain.job_id, /^[a-f0-9]{24}$/);
  execFileSync('docker', ['compose', 'exec', '-T', 'postgres', 'psql', '-U', process.env.POSTGRES_USER || 'stakraf', '-d', process.env.POSTGRES_DB || 'stakraf', '-v', 'ON_ERROR_STOP=1', '-c',
    `UPDATE jobs SET lease_expires_at=now()-interval '1 second' WHERE id='${uncertain.job_id}'`], { stdio: 'pipe' });
  await complete(uncertain.job_id, true, 'Late result', 409);
  assert.equal((await request('/api/jobs')).find(job => job.id === uncertain.job_id).status, 'timed_out');
  assert.equal((await request('/api/apps')).find(item => item.id === uncertain.id).status, 'unknown');
  await request(`/api/jobs/${uncertain.job_id}/retry`, { method: 'POST', status: 409 });
  assert.equal((await poll()).job, null, 'Expired work must not replay automatically');
}
const ownerCookie = cookie;
await request('/api/auth/signup', { method: 'POST', body: { email: `other-${randomUUID()}@example.test`, password: randomUUID() } });
assert.equal((await request('/api/jobs')).length, 0, 'Execution history is account-scoped');
await request(`/api/jobs/${failed.id}/retry`, { method: 'POST', status: 404 });
await request(`/api/jobs/${logs.job_id}/cancel`, { method: 'POST', status: 404 });
cookie = ownerCookie;
const incident = await request('/api/incidents', {
  method: 'POST', status: 201,
  body: { project_id: project.id, server_id: server.id, title: 'CI incident check', severity: 'medium', description: 'Test record' },
});
for (const status of ['acknowledged', 'resolved', 'open']) {
  await request(`/api/incidents/${incident.id}`, { method: 'PATCH', body: { status } });
  assert.equal((await request('/api/incidents')).find(item => item.id === incident.id).status, status);
}
await request('/api/settings', { method: 'PATCH', body: { name: 'CI workspace', timezone: 'UTC' } });
assert.equal((await request('/api/settings')).name, 'CI workspace');
assert.equal((await request('/api/overview')).online_servers, 1);
assert.ok((await request('/api/audit')).some(event => event.event_type === 'change.approved'));
const key = await request('/api/keys', { method: 'POST', status: 201, body: { label: 'CI MCP key' } });
const mcp = { jsonrpc: '2.0', id: 1, method: 'tools/list', params: {} };
assert.equal((await request('/mcp', { method: 'POST', token: key.key, body: mcp })).result.tools.length, 5);
const call = await request('/mcp', {
  method: 'POST', token: key.key,
  body: { ...mcp, method: 'tools/call', params: { name: 'list_projects', arguments: {} } },
});
assert.equal(JSON.parse(call.result.content[0].text)[0].id, project.id);
await request(`/api/keys/${key.id}`, { method: 'DELETE' });
await request('/mcp', { method: 'POST', token: key.key, body: mcp, status: 401 });
await request('/api/auth/logout', { method: 'POST' });
await request('/api/auth/me', { status: 401 });
await request('/api/auth/login', { method: 'POST', body: credentials });
assert.equal((await request('/api/auth/me')).email, credentials.email);
console.log('Control-plane smoke checks passed: auth, enrollment, telemetry, serialized execution, completion replay, cancellation, retry, ownership, approval gates, incidents, audit, settings, MCP and revocation.');
