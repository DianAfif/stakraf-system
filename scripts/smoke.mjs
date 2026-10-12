// Run only against an isolated test workspace; this creates persistent test data.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';

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
// Exercise approval persistence without claiming jobs or running containers.
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
console.log('Control-plane smoke checks passed: auth, enrollment, telemetry, approval gates, incidents, audit, settings, MCP and revocation.');
