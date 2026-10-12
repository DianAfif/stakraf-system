import React,{useEffect,useRef,useState} from 'react';
import './incidents.css';

const states=['open','acknowledged','investigating','monitoring','resolved'];
const draftOf=i=>({version:i.version,status:i.status,severity:i.severity,responder:i.responder||'',impact:i.impact||'',root_cause:i.root_cause||'',resolution:i.resolution||'',follow_up:i.follow_up||'',note:''});
const time=s=>s?new Date(s).toLocaleString():'Not recorded';
export default function IncidentConsole({incidents,request,changed,create}) {
 const[selected,setSelected]=useState(''),[detail,setDetail]=useState(null),[draft,setDraft]=useState(null),[dirty,setDirty]=useState(false),[note,setNote]=useState(''),[error,setError]=useState(''),[busy,setBusy]=useState(false),[search,setSearch]=useState(''),[state,setState]=useState('active'),[severity,setSeverity]=useState('all');
 const selectedRef=useRef(selected);selectedRef.current=selected;
 const dirtyRef=useRef(dirty);dirtyRef.current=dirty;
 const active=incidents.filter(i=>i.status!=='resolved');
 const filtered=incidents.filter(i=>(state==='all'||state==='active'?state==='all'||i.status!=='resolved':i.status===state)&&(severity==='all'||i.severity===severity)&&[i.title,i.description,i.project_name,i.app_name,i.responder].join(' ').toLowerCase().includes(search.toLowerCase()));
 async function load(id=selected,force=false){
  if(!id)return;
  try{const data=await request('/incidents/'+id);if(selectedRef.current!==id||(!force&&dirtyRef.current))return;setDetail(data);setDraft(draftOf(data));setDirty(false);setError('')}catch(e){if(selectedRef.current===id)setError(e.message)}
 }
 useEffect(()=>{setDetail(null);setDraft(null);setDirty(false);setNote('');load(selected)},[selected]);
 useEffect(()=>{if(!selected||dirty)return;const timer=setInterval(()=>load(selected),8000);return()=>clearInterval(timer)},[selected,dirty]);
 function edit(field,value){setDraft(d=>({...d,[field]:value}));setDirty(true)}
 function choose(id){if(dirty&&!confirm('Discard unsaved incident changes?'))return;setSelected(id)}
 async function save(e){
  e.preventDefault();setBusy(true);setError('');
  try{await request('/incidents/'+selected,{method:'PATCH',body:draft});await load(selected,true);changed?.()}catch(e){setError(e.message+' Use Reload latest before retrying a conflicting edit.')}finally{setBusy(false)}
 }
 async function addNote(e){
  e.preventDefault();setBusy(true);setError('');
  try{await request('/incidents/'+selected+'/notes',{method:'POST',body:{body:note}});setNote('');if(!dirty)await load();else{const data=await request('/incidents/'+selected);setDetail(data)}changed?.()}catch(e){setError(e.message)}finally{setBusy(false)}
 }
 return <section className="incident-console">
  <div className="incident-metrics">
   {[['ACTIVE',active.length],['CRITICAL / HIGH',active.filter(i=>['critical','high'].includes(i.severity)).length],['UNASSIGNED',active.filter(i=>!i.responder).length],['RESOLVED',incidents.filter(i=>i.status==='resolved').length]].map(([label,count])=><div key={label}><small>{label}</small><strong>{count}</strong></div>)}
  </div>
  <div className="incident-toolbar">
   <input aria-label="Search incidents" value={search} onChange={e=>setSearch(e.target.value)} placeholder="Search title, project, application or responder…"/>
   <select aria-label="Incident state filter" value={state} onChange={e=>setState(e.target.value)}><option value="active">Active incidents</option><option value="all">All incidents</option>{states.map(s=><option key={s} value={s}>{s}</option>)}</select>
   <select aria-label="Severity filter" value={severity} onChange={e=>setSeverity(e.target.value)}><option value="all">All severities</option>{['critical','high','medium','low'].map(s=><option key={s}>{s}</option>)}</select>
  </div>
  <div className="incident-workspace">
   <div className="incident-register">
    <header><h3>Incident register</h3><small>Latest 300 · workspace-owned records</small></header>
    {!filtered.length&&<div className="incident-empty"><h3>No matching incidents</h3><p>Report an issue or adjust the filters. Failed deployments also create records automatically.</p><button className="btn" onClick={create}>Report incident</button></div>}
    {filtered.map(i=><button className={'incident-row '+(selected===i.id?'selected':'')} key={i.id} onClick={()=>choose(i.id)}>
     <span><strong>{i.title}</strong><small>{i.project_name}{i.app_name?' · '+i.app_name:''}</small><small>{i.responder||'No responder recorded'} · {i.source==='job_failure'?'Deployment failure':'Reported by operator'}</small></span>
     <span className="incident-row-flags"><b className={'incident-severity '+i.severity}>{i.severity}</b><small>{i.status}</small></span>
    </button>)}
   </div>
   <div className="incident-detail">
    {!selected?<div className="incident-empty"><h3>Select an incident to investigate</h3><p>Record impact, track response, attach notes and preserve the resolution timeline. No automatic remediation is triggered.</p></div>:!detail||!draft?<p role="status">Loading incident…</p>:<>
     <header><div><small>INCIDENT / {detail.id.slice(0,8).toUpperCase()}</small><h2>{detail.title}</h2><p>{detail.project_name} · {detail.server_name||'No server linked'}{detail.app_name?' · '+detail.app_name:''}</p></div><button className="btn btn-subtle" disabled={busy} onClick={()=>{if(!dirty||confirm('Discard unsaved edits and reload?'))load(selected,true)}}>Reload latest</button></header>
     {error&&<p className="error-text" role="alert">{error}</p>}
     <p className="incident-description">{detail.description||'No initial description.'}</p>
     <div className="incident-times"><span>Opened <b>{time(detail.created_at)}</b></span><span>Responded <b>{time(detail.acknowledged_at)}</b></span><span>Resolved <b>{time(detail.resolved_at)}</b></span></div>
     {detail.job_id&&<p>Related execution: <code>{detail.job_id}</code>. Inspect its output in Deployments before acting.</p>}
     <form onSubmit={save} className="incident-form">
      <div className="incident-form-grid">
       <label>STATUS<select value={draft.status} onChange={e=>edit('status',e.target.value)}>{states.map(s=><option key={s}>{s}</option>)}</select></label>
       <label>SEVERITY<select value={draft.severity} onChange={e=>edit('severity',e.target.value)}>{['critical','high','medium','low'].map(s=><option key={s}>{s}</option>)}</select></label>
      </div>
      <label>RESPONDER<input value={draft.responder} onChange={e=>edit('responder',e.target.value)} maxLength={80} placeholder="Name or response role; this does not grant account access"/></label>
      {[['impact','USER / SERVICE IMPACT',2000],['root_cause','ROOT CAUSE',3000],['resolution','RESOLUTION SUMMARY',3000],['follow_up','FOLLOW-UP ACTIONS',3000],['note','CHANGE / REOPEN REASON',3000]].map(([field,label,max])=><label key={field}>{label}<textarea value={draft[field]} onChange={e=>edit(field,e.target.value)} maxLength={max} rows={field==='impact'?2:3}/></label>)}
      <p>Resolution needs a meaningful summary. Reopening needs a reason. Unsaved edits stop automatic refresh; version checks protect against overwriting another update.</p>
      <button className="btn" type="submit" disabled={busy||!dirty}>{busy?'Saving…':'Save incident'}</button>{dirty&&<small className="incident-unsaved">Unsaved changes · version {draft.version}</small>}
     </form>
     <section className="incident-timeline"><h3>Response timeline</h3><small>Latest 200 events · oldest first</small>
      {!detail.events.length&&<p>This older record has no timeline events yet. Historical response times are not reconstructed.</p>}
      {detail.events.map(e=><article key={e.id}><header><strong>{e.kind.replaceAll('_',' ')}</strong><small>{time(e.created_at)} · {e.actor}</small></header><p>{e.body}</p></article>)}
      <form onSubmit={addNote}><label>INVESTIGATION NOTE<textarea value={note} onChange={e=>setNote(e.target.value)} rows={3} maxLength={3000} minLength={2} required placeholder="Evidence, actions taken, current hypothesis. Do not include credentials."/></label><button className="btn btn-subtle" disabled={busy||note.trim().length<2}>Add timeline note</button></form>
     </section>
    </>}
    {error&&!detail&&<p className="error-text" role="alert">{error}</p>}
   </div>
  </div>
 </section>;
}
