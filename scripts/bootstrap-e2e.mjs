// Tests first-account setup against a separate disposable database and API.
import assert from 'node:assert/strict';
import {execFileSync} from 'node:child_process';
import {randomUUID} from 'node:crypto';
assert.equal(process.env.STAKRAF_CONNECTOR_HOST_TEST,'true');
const run=(args,options={})=>execFileSync('docker',args,{encoding:'utf8',stdio:['pipe','pipe','pipe'],...options}).trim();
const api=run(['compose','ps','-q','api']);
const networks=JSON.parse(run(['inspect','--format','{{json .NetworkSettings.Networks}}',api]));
const network=Object.keys(networks)[0];
const image=run(['inspect','--format','{{.Config.Image}}',api]);
const database='stakraf_setup_test';
const name='stakraf-bootstrap-'+randomUUID().slice(0,8);
const code=randomUUID();
run(['compose','exec','-T','postgres','psql','-U','stakraf','-d','stakraf','-v','ON_ERROR_STOP=1','-c',`CREATE DATABASE ${database}`]);
let created=false;
try{
  run(['run','-d','--name',name,'--network',network,'-p','127.0.0.1::8080','-e',`DATABASE_URL=postgres://stakraf:${process.env.POSTGRES_PASSWORD}@postgres:5432/${database}?sslmode=disable`,'-e','STAKRAF_SIGNUP_MODE=first_user','-e',`STAKRAF_SETUP_TOKEN=${code}`,image]);
  created=true;
  const base='http://'+run(['port',name,'8080/tcp']);
  async function call(path,method='GET',body,status=200){const r=await fetch(base+path,{method,headers:{'Content-Type':'application/json'},body:body===undefined?undefined:JSON.stringify(body),signal:AbortSignal.timeout(5000)});assert.equal(r.status,status,path);return r.json()}
  let healthy=false;
  for(let attempt=0;attempt<30;attempt++){try{await call('/api/health');healthy=true;break}catch{await new Promise(resolve=>setTimeout(resolve,1000))}}
  assert.ok(healthy,'Bootstrap API started');
  assert.deepEqual(await call('/api/auth/options'),{signup_available:true,setup_required:true});
  const account={email:'owner@example.test',password:randomUUID()};
  await call('/api/auth/signup','POST',account,403);
  await call('/api/auth/signup','POST',{...account,setup_token:'wrong'},403);
  const simultaneous=await Promise.all(['owner','other'].map(async user=>{const response=await fetch(base+'/api/auth/signup',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({...account,email:`${user}@example.test`,setup_token:code})});return response.status}));
  assert.deepEqual(simultaneous.sort(),[200,403],'Only one initial owner may win concurrent setup');
  assert.equal((await call('/api/auth/options')).signup_available,false);
  await call('/api/auth/signup','POST',{email:'later@example.test',password:randomUUID(),setup_token:code},403);
  console.log('First-account setup passed: secret required, concurrent registration serialized, further signup closed.');
}finally{if(created)run(['rm','-f',name])}
