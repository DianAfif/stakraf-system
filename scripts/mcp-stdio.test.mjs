import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { spawn } from 'node:child_process';
import { once } from 'node:events';

async function run(url,input,token='stk_abc123') {
 const child=spawn(process.execPath,['scripts/mcp-stdio.mjs'],{cwd:new URL('..',import.meta.url),env:{...process.env,STAKRAF_MCP_URL:url,STAKRAF_MCP_TOKEN:token},stdio:['pipe','pipe','pipe']});
 let out='',err='';
 child.stdout.on('data',v=>out+=v);child.stderr.on('data',v=>err+=v);
 child.stdin.end(input);
 const [code]=await once(child,'exit');
 return {code,out,err};
}
test('stdio preserves RPC and never prints credentials',async()=>{
 const server=createServer(async(req,res)=>{
  assert.equal(req.headers.authorization,'Bearer stk_abc123');
  let body='';for await(const chunk of req)body+=chunk;
  const v=JSON.parse(body);
  res.setHeader('Content-Type','application/json');
  res.end(JSON.stringify({jsonrpc:'2.0',id:v.id,result:{tools:[]}}));
 });
 server.listen(0,'127.0.0.1');await once(server,'listening');
 try{
  const result=await run('http://127.0.0.1:'+server.address().port+'/mcp',JSON.stringify({jsonrpc:'2.0',id:1,method:'tools/list'})+'\n');
  assert.equal(result.code,0);assert.equal(JSON.parse(result.out).id,1);
  assert.ok(!result.out.includes('stk_'));assert.equal(result.err,'');
 }finally{server.close()}
});
test('rejects non-loopback HTTP and URL credentials',async()=>{
 for(const url of ['http://example.com/mcp','https://user:pass@example.com/mcp']){
  const result=await run(url,'');assert.equal(result.code,1);assert.equal(result.out,'');
 }
});
test('does not forward bearer keys on redirects',async()=>{
 const server=createServer((req,res)=>{res.writeHead(302,{Location:'https://example.com'});res.end()});
 server.listen(0,'127.0.0.1');await once(server,'listening');
 try{
  const result=await run('http://127.0.0.1:'+server.address().port+'/mcp','{"jsonrpc":"2.0","id":4,"method":"ping"}\n');
  assert.equal(JSON.parse(result.out).error.code,-32000);assert.ok(!result.out.includes('stk_'));
 }finally{server.close()}
});
