import React, { useState } from 'react';

export default function MCPConnection() {
 const [client,setClient]=useState('codex');
 const [token,setToken]=useState('');
 const [status,setStatus]=useState('');
 const [busy,setBusy]=useState(false);
 const url=window.location.origin+'/mcp';
 const examples={
  codex:`[mcp_servers.stakraf]\nurl = "${url}"\nbearer_token_env_var = "STAKRAF_MCP_TOKEN"`,
  stdio:JSON.stringify({mcpServers:{stakraf:{command:'node',args:['/absolute/path/to/stakraf-system/scripts/mcp-stdio.mjs'],env:{STAKRAF_MCP_URL:url,STAKRAF_MCP_TOKEN:'YOUR_MCP_KEY'}}}},null,2),
  http:JSON.stringify({mcpServers:{stakraf:{type:'http',url,headers:{Authorization:'Bearer YOUR_MCP_KEY'}}}},null,2),
 };
 async function test(e) {
  e.preventDefault();setBusy(true);setStatus('');
  try {
   const call=async(method,params,id)=>{
    const r=await fetch('/mcp',{method:'POST',credentials:'omit',headers:{'Content-Type':'application/json',Accept:'application/json, text/event-stream',Authorization:'Bearer '+token,'MCP-Protocol-Version':'2025-06-18'},body:JSON.stringify({jsonrpc:'2.0',id,method,params}),signal:AbortSignal.timeout(10000)});
    if(!r.ok)throw new Error(r.status===401?'Key invalid or revoked.':'Connection failed ('+r.status+').');
    const v=await r.json();if(v.error)throw new Error('MCP request failed.');return v.result;
   };
   const server=await call('initialize',{protocolVersion:'2025-06-18',capabilities:{},clientInfo:{name:'stakraf-connection-test',version:'1'}},1);
   const result=await call('tools/list',{},2);
   setStatus('Connected to '+server.serverInfo.name+' · '+result.tools.length+' tools available. No infrastructure action was performed.');
  }catch(err){setStatus(err.message)}finally{setBusy(false);setToken('')}
 }
 return <section className="mcp-setup" aria-label="AI connection setup">
  <h3>Connect your AI client</h3>
  <p className="fine-print">Create an Inspect-only key first. Stakraf supplies infrastructure tools; your client supplies the model. Data requested by your client is shared with its provider.</p>
  <label>CLIENT CONNECTION<select value={client} onChange={e=>setClient(e.target.value)}><option value="codex">Codex · authenticated HTTP</option><option value="http">HTTP clients · adapt to client format</option><option value="stdio">Desktop clients · local stdio adapter</option></select></label>
  <pre className="code-snippet">{examples[client]}</pre>
  <button className="btn btn-subtle" type="button" onClick={async()=>{try{await navigator.clipboard.writeText(examples[client]);setStatus('Configuration copied; it contains no secret.')}catch{setStatus('Copy the configuration above.')}}}>Copy configuration</button>
  <p className="fine-print">{client==='codex'?'Add the block to your Codex configuration and provide STAKRAF_MCP_TOKEN in the environment of the process launching Codex.':client==='stdio'?'Requires Node.js 22 and the downloaded Stakraf package on your computer. Replace the absolute path and key placeholder; keep the client configuration private.':'Some clients use different field names. Select Streamable HTTP and set the Authorization header shown above.'}</p>
  <div className="form-callout"><span>ChatGPT account linking: OAuth is not configured in this package. Do not publish an anonymous endpoint or paste a full-access API key into a chat to bypass this. An OAuth provider and live HTTPS deployment are required for that path.</span></div>
  <form onSubmit={test}>
   <label>TEST MCP KEY<input type="password" autoComplete="off" value={token} onChange={e=>setToken(e.target.value)} placeholder="stk_…" required/></label>
   <button className="btn" disabled={busy||!token} type="submit">{busy?'Testing…':'Test connection'}</button>
  </form>
  <p className="fine-print">Test keys remain in memory only and are cleared after testing. They are not saved in browser storage.</p>
  {status&&<p role="status">{status}</p>}
 </section>;
}
