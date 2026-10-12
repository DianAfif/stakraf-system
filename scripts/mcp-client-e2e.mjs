// Real official SDK check; installs no dependencies in the product runtime.
// Run only against an isolated test database with registration enabled.
import assert from 'node:assert/strict';
import { randomUUID } from 'node:crypto';
import { pathToFileURL } from 'node:url';
import { resolve } from 'node:path';

const sdk=process.env.STAKRAF_MCP_SDK_PATH;
if (!sdk) throw new Error('Set STAKRAF_MCP_SDK_PATH to the official SDK package directory.');
const {Client}=await import(pathToFileURL(resolve(sdk,'dist/esm/client/index.js')));
const {StreamableHTTPClientTransport}=await import(pathToFileURL(resolve(sdk,'dist/esm/client/streamableHttp.js')));
const {StdioClientTransport}=await import(pathToFileURL(resolve(sdk,'dist/esm/client/stdio.js')));
const base=process.env.STAKRAF_SMOKE_URL||'http://localhost:3000';
const origin=process.env.STAKRAF_SMOKE_ORIGIN||new URL(base).origin;
const credentials={email:`sdk-${randomUUID()}@example.test`,password:randomUUID()};
const signup=await fetch(base+'/api/auth/signup',{method:'POST',headers:{'Content-Type':'application/json',Origin:origin},body:JSON.stringify(credentials)});
assert.equal(signup.status,200);
const cookie=signup.headers.get('set-cookie').split(';')[0];
const issue=await fetch(base+'/api/keys',{method:'POST',headers:{'Content-Type':'application/json',Origin:origin,Cookie:cookie},body:JSON.stringify({label:'SDK interoperability',scope:'mcp_read'})});
assert.equal(issue.status,201);
const key=await issue.json();
try {
 for (const transport of [
  new StreamableHTTPClientTransport(new URL(base+'/mcp'),{requestInit:{headers:{Authorization:'Bearer '+key.key}}}),
  new StdioClientTransport({command:process.execPath,args:[resolve('scripts/mcp-stdio.mjs')],env:{...process.env,STAKRAF_MCP_URL:base+'/mcp',STAKRAF_MCP_TOKEN:key.key}}),
 ]) {
  const client=new Client({name:'stakraf-sdk-check',version:'1'},{capabilities:{}});
  try {
   await client.connect(transport);
   assert.equal(client.getServerVersion().name,'stakraf-system');
   assert.equal((await client.listTools()).tools.length,10);
   const result=await client.callTool({name:'get_operations_summary',arguments:{}});
   assert.equal(result.isError,false);
   assert.equal(result.structuredContent.data.projects,0);
  } finally { await client.close() }
 }
 console.log('Official MCP SDK: authenticated HTTP and stdio handshake, discovery and real tool calls passed.');
} finally {
 const revoke=await fetch(base+'/api/keys/'+key.id,{method:'DELETE',headers:{Origin:origin,Cookie:cookie}});
 assert.equal(revoke.status,200);
}
