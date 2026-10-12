import React,{useEffect,useState} from 'react';
import './releases.css';

export default function ApplicationReleases({app,request,changed}) {
 const[releases,setReleases]=useState([]),[pipeline,setPipeline]=useState(false),[key,setKey]=useState(''),[image,setImage]=useState(''),[note,setNote]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(false);
 const path='/apps/'+app.id;
 async function load(){
  try {const[r,p]=await Promise.all([request(path+'/releases'),request(path+'/pipeline-key')]);setReleases(r);setPipeline(p.enabled);setError('')}catch(e){setError(e.message)}
 }
 useEffect(()=>{load();const timer=setInterval(load,8000);return()=>clearInterval(timer)},[app.id]);
 async function action(fn){setBusy(true);setError('');try{await fn();await load();changed?.()}catch(e){setError(e.message)}finally{setBusy(false)}}
 async function submit(e){e.preventDefault();await action(async()=>{await request(path+'/releases',{method:'POST',body:{image,note,request_id:crypto.randomUUID()}});setImage('');setNote('')})}
 return <section className="application-releases">
  <h3>Releases & CI/CD</h3>
  <p>Build and test in your CI provider. Submit an immutable image, review it here, then approve deployment to this server.</p>
  {error&&<p role="alert" className="error-text">{error}</p>}
  <form onSubmit={submit}>
   <label>IMAGE DIGEST<input value={image} onChange={e=>setImage(e.target.value)} placeholder="ghcr.io/team/app@sha256:…" required maxLength={260}/></label>
   <label>RELEASE NOTE<textarea value={note} onChange={e=>setNote(e.target.value)} maxLength={500} rows={2}/></label>
   <button className="btn" disabled={busy} type="submit">Submit release for approval</button>
  </form>
  <div className="release-pipeline">
   <h4>Application pipeline key</h4>
   <p>{pipeline?'A submission-only key is active.':'No pipeline key is active.'} This key cannot read workspace data, approve releases or execute work.</p>
   <button className="btn btn-subtle" disabled={busy} onClick={()=>{if(pipeline&&!confirm('Rotate this application pipeline key? The old key will stop working.'))return;action(async()=>{const result=await request(path+'/pipeline-key',{method:'POST'});setKey(result.key)})}}>{pipeline?'Rotate pipeline key':'Create pipeline key'}</button>
   {pipeline&&<button className="btn btn-subtle" disabled={busy} onClick={()=>{if(confirm('Revoke this pipeline key?'))action(async()=>{await request(path+'/pipeline-key',{method:'DELETE'});setKey('')})}}>Revoke key</button>}
   {key&&<div className="pipeline-secret"><p>Save once as your CI secret <code>STAKRAF_PIPELINE_TOKEN</code>. Stakraf stores only its hash.</p><input aria-label="New pipeline secret" type="password" readOnly value={key}/><button className="btn btn-subtle" onClick={async()=>{try{await navigator.clipboard.writeText(key)}catch{setError('Select and copy the secret manually.')}}}>Copy secret</button><button className="btn btn-subtle" onClick={()=>setKey('')}>Hide permanently</button></div>}
   <p>Submission endpoint: <code>{location.origin}/api/pipeline/releases</code></p>
   <p>The downloadable source package includes <code>deploy/examples/application-ci.yml</code> and <code>scripts/ci-submit.mjs</code>. Copy them into your application repository and adapt the test command. Registry pull credentials must be configured on the connector host for private images.</p>
  </div>
  <h4>Release history</h4>
  {!releases.length&&<p>No digest releases submitted yet. Your initial deployment remains in deployment history.</p>}
  {releases.map(r=><article className="release-record" key={r.id}>
   <header><strong>{r.status}</strong><small>{new Date(r.created_at).toLocaleString()}</small></header>
   <code>{r.image}</code>
   {r.commit_sha&&<small>Commit {r.commit_sha}</small>}
   {r.note&&<p>{r.note}</p>}
   {r.status==='pending'&&<div className="release-buttons">{['approve','reject'].map(decision=><button className={'btn '+(decision==='reject'?'btn-subtle':'')} key={decision} disabled={busy} onClick={()=>{if(confirm(decision==='approve'?'Deploy this release? The current container will be stopped briefly.':'Reject this release?'))action(()=>request(path+'/releases/'+r.id+'/decision',{method:'POST',body:{decision}}))}}>{decision==='approve'?'Approve deployment':'Reject'}</button>)}</div>}
   {r.status==='success'&&<button className="btn btn-subtle" disabled={busy} onClick={()=>{setImage(r.image);setNote('Rollback to successful release '+r.id)}}>Prepare rollback proposal</button>}
   {r.output&&<details><summary>Execution output</summary><pre>{r.output}</pre></details>}
  </article>)}
  <p>Rollback creates another release proposal and requires approval. It changes the container image only; it cannot reverse database migrations or external side effects. Container startup is checked, not application health.</p>
 </section>;
}
