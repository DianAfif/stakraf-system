// Dependency-free stdio adapter for clients without authenticated HTTP transport.
// Node.js 22+. Secrets stay in the environment; stdout contains JSON-RPC only.
import { createInterface } from 'node:readline';
import { once } from 'node:events';

let endpoint;
try {
 endpoint = new URL(process.env.STAKRAF_MCP_URL || 'https://console.stakraf.com/mcp');
 const loopback = ['localhost', '127.0.0.1', '[::1]'].includes(endpoint.hostname);
 if ((endpoint.protocol !== 'https:' && !(endpoint.protocol === 'http:' && loopback)) ||
     endpoint.username || endpoint.password || endpoint.search || endpoint.hash) throw new Error();
 if (!/^stk_[a-f0-9]+$/.test(process.env.STAKRAF_MCP_TOKEN || '')) throw new Error();
} catch {
 console.error('Set STAKRAF_MCP_URL to an HTTPS MCP endpoint and STAKRAF_MCP_TOKEN to a Stakraf key. HTTP is allowed only on loopback.');
 process.exit(1);
}
let version = '2025-06-18';
const lines = createInterface({ input: process.stdin, crlfDelay: Infinity });
for await (const line of lines) {
 let request;
 try {
  if (Buffer.byteLength(line) > 1024 * 1024) throw new Error();
  request = JSON.parse(line);
  if (!request || Array.isArray(request) || request.jsonrpc !== '2.0' || typeof request.method !== 'string') throw new Error();
 } catch {
  await write({ jsonrpc: '2.0', id: null, error: { code: -32700, message: 'Invalid JSON-RPC input' } });
  continue;
 }
 try {
  const response = await fetch(endpoint, {
   method: 'POST', redirect: 'error', signal: AbortSignal.timeout(30000),
   headers: { Authorization: `Bearer ${process.env.STAKRAF_MCP_TOKEN}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'MCP-Protocol-Version': version },
   body: JSON.stringify(request),
  });
  if (!response.ok) throw new Error(`Stakraf returned HTTP ${response.status}`);
  if (request.id === undefined) continue;
  const body = await response.json();
  if (request.method === 'initialize' && body.result?.protocolVersion) version = body.result.protocolVersion;
  await write(body);
 } catch (error) {
  if (request.id !== undefined) await write({ jsonrpc: '2.0', id: request.id, error: { code: -32000, message: error.message.startsWith('Stakraf returned HTTP ') ? error.message : 'Stakraf connection failed. Check URL, key and network.' } });
 }
}
async function write(value) {
 if (!process.stdout.write(JSON.stringify(value) + '\n')) await once(process.stdout, 'drain');
}
