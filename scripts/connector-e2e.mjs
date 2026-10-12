// Installs a service on a disposable Linux CI runner and executes real Docker
// operations. Never run this against a production host or console.
import assert from 'node:assert/strict';
import {randomUUID,createHash} from 'node:crypto';
import {execFileSync} from 'node:child_process';
import {mkdtempSync,writeFileSync,readFileSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

assert.equal(process.env.STAKRAF_CONNECTOR_HOST_TEST,'true','Explicit disposable-host test flag required');
assert.equal(process.platform,'linux','The installer test requires Linux/systemd');
const base=process.env.STAKRAF_SMOKE_URL||'http://localhost:3000';
let cookie='';
const containers=[];
const folder=mkdtempSync(join(tmpdir(),'stakraf-connector-test-'));
const rootToken=join(folder,'root-enrollment-token');
const credentials={email:`connector-${randomUUID()}@example.test`,password:randomUUID()};
async function request(path,method='GET',body,status=200){
  const response=await fetch(base+path,{method,headers:{'Content-Type':'application/json',Origin:new URL(base).origin,...(cookie?{Cookie:cookie}:{})},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(10000)});
  assert.equal(response.status,status,`${method} ${path} returned ${response.status}`);
  if(response.headers.get('set-cookie'))cookie=response.headers.get('set-cookie').split(';')[0];
  return response.json();
}
async function waitFor(label,check,timeout=180000){
  const end=Date.now()+timeout;
  while(Date.now()<end){if(await check())return;await new Promise(resolve=>setTimeout(resolve,2000))}
  throw new Error(`Timed out waiting for ${label}`);
}
const jobFinished=async(id)=>{const job=(await request('/api/jobs')).find(item=>item.id===id);if(job?.status==='failed')throw new Error(`Host job ${id} failed: ${job.output}`);return job?.status==='success'};
try{
  await request('/api/auth/signup','POST',credentials);
  const project=await request('/api/projects','POST',{name:'Real connector test'},201);
  const server=await request('/api/servers','POST',{project_id:project.id,name:'systemd-ci-host'},201);
  const script=await fetch(base+'/api/connector/install.sh');assert.equal(script.status,200);
  const installer=join(folder,'installer.sh');writeFileSync(installer,await script.text(),{mode:0o600});
  const token=join(folder,'enrollment-token');writeFileSync(token,server.enrollment_token,{mode:0o600});
  execFileSync('sudo',['install','-o','root','-g','root','-m','0600',token,rootToken]);
  const asset='stakraf-agent-linux-amd64';
  const binary=await fetch(`${base}/api/connector/${asset}`);assert.equal(binary.status,200);
  const bytes=Buffer.from(await binary.arrayBuffer());
  const digest=(await (await fetch(`${base}/api/connector/${asset}.sha256`)).text()).trim().split(/\s+/)[0];
  assert.equal(createHash('sha256').update(bytes).digest('hex'),digest,'Bundled download checksum');
  execFileSync('sudo',['bash',installer,'--url',base,'--allow-http','--token-file',rootToken],{stdio:'pipe',timeout:120000});
  execFileSync('sudo',['systemctl','is-active','--quiet','stakraf-agent']);
  await waitFor('online host with Docker readiness',async()=>{const host=(await request('/api/servers')).find(item=>item.id===server.id);return host?.online&&host?.docker_ready},60000);
  assert.match(execFileSync('/usr/local/bin/stakraf-agent',['--version'],{encoding:'utf8'}),/0\.4\.0/);
  const app=await request('/api/apps','POST',{project_id:project.id,server_id:server.id,name:'real-web',image:'nginxinc/nginx-unprivileged:stable-alpine',host_port:18088,container_port:8080},201);
  assert.match(app.id,/^[a-f0-9]{24}$/);containers.push(`stakraf-${app.id}`);
  await waitFor('real container deployment',()=>jobFinished(app.job_id));
  const web=await fetch('http://127.0.0.1:18088');assert.equal(web.status,200);assert.match(await web.text(),/Welcome to nginx/);
  assert.equal((await request('/api/apps')).find(item=>item.id===app.id).status,'running');
  const logJob=await request(`/api/apps/${app.id}/actions`,'POST',{kind:'logs'},201);
  await waitFor('real logs',()=>jobFinished(logJob.job_id));
  assert.ok((await request('/api/jobs')).find(item=>item.id===logJob.job_id).output.length>0);
  for(const kind of ['stop','start','restart']){
    const change=await request('/api/changes','POST',{app_id:app.id,kind,reason:'Disposable host E2E check'},201);
    const decision=await request(`/api/changes/${change.id}/decision`,'POST',{decision:'approve'});
    await waitFor(`real ${kind}`,()=>jobFinished(decision.job_id));
    const running=execFileSync('docker',['inspect','--format','{{.State.Running}}',`stakraf-${app.id}`],{encoding:'utf8'}).trim();
    assert.equal(running,kind==='stop'?'false':'true');
  }
  assert.equal((await fetch('http://127.0.0.1:18088')).status,200);
  execFileSync('docker',['stop',`stakraf-${app.id}`],{stdio:'pipe'});
  await waitFor('externally stopped container reconciliation',async()=>(await request('/api/apps')).find(item=>item.id===app.id).status==='stopped',45000);
  execFileSync('docker',['start',`stakraf-${app.id}`],{stdio:'pipe'});
  await waitFor('externally started container reconciliation',async()=>(await request('/api/apps')).find(item=>item.id===app.id).status==='running',45000);
  execFileSync('docker',['rm','-f',`stakraf-${app.id}`],{stdio:'pipe'});
  await waitFor('missing container reconciliation',async()=>(await request('/api/apps')).find(item=>item.id===app.id).status==='missing',45000);
  // A second installer run must preserve enrollment and recover the service.
  const before=execFileSync('sudo',['cat','/var/lib/stakraf/agent.json'],{encoding:'utf8'});
  execFileSync('sudo',['bash',installer,'--url',base,'--allow-http'],{stdio:'pipe',timeout:120000});
  assert.equal(execFileSync('sudo',['cat','/var/lib/stakraf/agent.json'],{encoding:'utf8'}),before,'Reinstallation preserves the host credential');
  await request(`/api/servers/${server.id}/disconnect`,'POST',{});
  assert.equal((await request('/api/servers')).find(item=>item.id===server.id).enrolled,false);
  const state=JSON.parse(before);
  const revoked=await fetch(base+'/api/agent/status',{headers:{Authorization:`Bearer ${state.agent_token}`}});assert.equal(revoked.status,401);
  const newEnrollment=await request(`/api/servers/${server.id}/enrollment`,'POST',{});
  assert.equal(newEnrollment.reconnect,true);
  const newTokenFile=join(folder,'reconnect-token');writeFileSync(newTokenFile,newEnrollment.enrollment_token,{mode:0o600});
  execFileSync('sudo',['install','-o','root','-g','root','-m','0600',newTokenFile,rootToken]);
  execFileSync('sudo',['bash',installer,'--url',base,'--allow-http','--reconnect','--token-file',rootToken],{stdio:'pipe',timeout:120000});
  await waitFor('reconnected host',async()=>{const host=(await request('/api/servers')).find(item=>item.id===server.id);return host?.online&&host?.docker_ready},60000);
  const renewed=JSON.parse(execFileSync('sudo',['cat','/var/lib/stakraf/agent.json'],{encoding:'utf8'}));
  assert.notEqual(renewed.agent_token,state.agent_token);
  assert.equal((await fetch(base+'/api/agent/status',{headers:{Authorization:`Bearer ${state.agent_token}`}})).status,401);
  assert.equal((await fetch(base+'/api/agent/status',{headers:{Authorization:`Bearer ${renewed.agent_token}`}})).status,200);
  const recoveredPassword=randomUUID();
  execFileSync('docker',['compose','exec','-T','api','/api','--reset-password',credentials.email],{input:recoveredPassword+'\n',stdio:['pipe','pipe','pipe'],timeout:30000});
  await request('/api/auth/me','GET',undefined,401);
  await request('/api/auth/login','POST',credentials,401);
  await request('/api/auth/login','POST',{email:credentials.email,password:recoveredPassword});
  assert.ok((await request('/api/audit')).some(item=>item.event_type==='account.password_recovered'));
  console.log('Real Linux connector passed: verified download, systemd installation, enrollment, heartbeat, Docker readiness, deployment HTTP response, logs, stop/start/restart, inventory reconciliation, reinstall and credential revocation.');
}finally{
  try{execFileSync('sudo',['systemctl','disable','--now','stakraf-agent'],{stdio:'pipe'})}catch{}
  for(const name of containers){try{execFileSync('docker',['rm','-f',name],{stdio:'pipe'})}catch{}}
  try{execFileSync('sudo',['rm','-f',rootToken],{stdio:'pipe'})}catch{}
}
