import React, { useEffect, useMemo, useRef, useState } from 'react';
import { AlertCircle, ArrowUpRight, Check, Clock3, Copy, Download, GitBranch, RotateCcw, Search, Server, ShieldCheck, Terminal, X } from 'lucide-react';
import './deployment.css';

const date = value => value ? new Date(value).toLocaleString(undefined, { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '—';
const terminal = status => ['success', 'failed', 'cancelled', 'timed_out'].includes(status);
const duration = (job, now = Date.now()) => {
  if (!job.started_at) return 'Not started';
  const seconds = Math.max(0, Math.floor(((job.finished_at ? new Date(job.finished_at).getTime() : now) - new Date(job.started_at).getTime()) / 1000));
  return seconds < 60 ? `${seconds}s` : `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
};
function Status({ status }) {
  const kind = { success: 'good', failed: 'bad', timed_out: 'bad', running: 'info', queued: 'pending', cancelled: 'muted' }[status] || 'muted';
  return <span className={`pill ${kind}`}><i />{status.replaceAll('_', ' ')}</span>;
}

export function DeploymentConsole({ jobs, projects, servers, inspect }) {
  const [query, setQuery] = useState('');
  const [status, setStatus] = useState('all');
  const [project, setProject] = useState('all');
  const [server, setServer] = useState('all');
  const [kind, setKind] = useState('all');
  const visible = useMemo(() => jobs.filter(job =>
    (status === 'all' || job.status === status) && (project === 'all' || job.project_id === project) &&
    (server === 'all' || job.server_id === server) && (kind === 'all' || job.kind === kind) &&
    [job.app_name, job.server_name, job.project_name, job.id, job.image].some(value => String(value || '').toLowerCase().includes(query.toLowerCase()))
  ), [jobs, query, status, project, server, kind]);
  const completed = jobs.filter(job => ['success', 'failed', 'timed_out'].includes(job.status));
  const success = completed.filter(job => job.status === 'success').length;
  const metrics = [
    ['IN QUEUE', jobs.filter(job => job.status === 'queued').length, 'Waiting for a host', Clock3],
    ['EXECUTING', jobs.filter(job => job.status === 'running').length, 'One operation per host', Server],
    ['NEEDS ATTENTION', jobs.filter(job => ['failed', 'timed_out'].includes(job.status)).length, 'Inspect before recovery', AlertCircle],
    ['SUCCESS RATE', completed.length ? `${Math.round(success / completed.length * 100)}%` : '—', `${completed.length} finished executions`, Check],
  ];
  const filters = [
    ['State', status, setStatus, ['queued', 'running', 'success', 'failed', 'timed_out', 'cancelled'].map(value => [value, value.replaceAll('_', ' ')])],
    ['Project', project, setProject, projects.map(item => [item.id, item.name])],
    ['Host', server, setServer, servers.map(item => [item.id, item.name])],
    ['Operation', kind, setKind, ['deploy', 'restart', 'stop', 'start', 'logs'].map(value => [value, value])],
  ];
  return <div className="deployment-console">
    <div className="execution-scope"><span><GitBranch size={14} /> EXECUTION CONTROL</span><small>Metrics cover the latest {jobs.length} jobs · up to 200</small></div>
    <div className="kpi-grid execution-metrics">{metrics.map(([label, value, detail, Icon]) => <div className="kpi" key={label}><div className="kpi-top"><span>{label}</span><Icon size={18} /></div><div className="kpi-value">{value}</div><div className="kpi-footer">{detail}</div></div>)}</div>
    <div className="execution-controls">
      <label className="execution-search"><Search size={16} /><input aria-label="Search execution history" placeholder="Search applications, hosts, images or job IDs" value={query} onChange={event => setQuery(event.target.value)} /></label>
      <div className="execution-filters">{filters.map(([label, value, setValue, options]) => <label key={label}><span>{label}</span><select value={value} onChange={event => setValue(event.target.value)}><option value="all">All {label === 'State' ? 'states' : label === 'Operation' ? 'operations' : label === 'Host' ? 'hosts' : 'projects'}</option>{options.map(([id, name]) => <option value={id} key={id}>{name}</option>)}</select></label>)}</div>
    </div>
    <section className="panel"><header className="panel-header"><div><h3>Execution history</h3><p>{visible.length} matching jobs · newest first</p></div><span className="execution-readonly"><ShieldCheck size={14} /> Approval gates enforced</span></header>
      <div className="table-wrap"><table><thead><tr><th>APPLICATION / OPERATION</th><th>HOST / PROJECT</th><th>STATE</th><th>QUEUED</th><th>EXECUTION TIME</th><th /></tr></thead><tbody>{visible.map(job => <tr key={job.id}>
        <td><button className="execution-open" onClick={() => inspect(job)}><span className="resource-icon"><Terminal size={16} /></span><span><strong>{job.app_name}</strong><small>{job.kind.toUpperCase()} {job.retry_of && '· RETRY'}</small></span></button></td>
        <td><div className="row-stack"><strong>{job.server_name}</strong><small>{job.project_name}</small></div></td>
        <td><Status status={job.status} /></td><td className="mono">{date(job.created_at)}</td><td className="mono">{duration(job)}</td>
        <td><button className="icon-button" aria-label={`Inspect ${job.kind} for ${job.app_name}`} onClick={() => inspect(job)}><ArrowUpRight size={16} /></button></td>
      </tr>)}</tbody></table></div>
      {!visible.length && <div className="empty-state"><span className="empty-symbol"><GitBranch size={25} /></span><strong>{jobs.length ? 'No matching executions' : 'Your execution history starts here'}</strong><p>{jobs.length ? 'Adjust the filters to see more jobs.' : 'Deploy an application or approve an operation to follow its progress here.'}</p>{jobs.length > 0 && <button className="btn subtle" onClick={() => { setQuery(''); setStatus('all'); setProject('all'); setServer('all'); setKind('all'); }}>Clear filters</button>}</div>}
    </section>
    <div className="execution-policy"><ShieldCheck size={17} /><p>Queued work can be cancelled. Interrupted operations are marked for inspection. Restart, stop, and start always require a new approval before another attempt.</p></div>
  </div>;
}

export function JobInspector({ job, close, post, requestChange }) {
  const [busy, setBusy] = useState(false);
  const [action, setAction] = useState('');
  const [message, setMessage] = useState('');
  const [now, setNow] = useState(Date.now());
  const dialog = useRef(null);
  useEffect(() => {
    const previous = document.activeElement;
    const element = dialog.current;
    element.querySelector('button')?.focus();
    const onKey = event => {
      if (event.key === 'Escape') { event.stopPropagation(); close(); }
      if (event.key !== 'Tab') return;
      const controls = [...element.querySelectorAll('button:not(:disabled), a[href], input, select, textarea, [tabindex="0"]')];
      if (!controls.length) return;
      if (event.shiftKey && (document.activeElement === controls[0] || !element.contains(document.activeElement))) { event.preventDefault(); controls.at(-1).focus(); }
      else if (!event.shiftKey && (document.activeElement === controls.at(-1) || !element.contains(document.activeElement))) { event.preventDefault(); controls[0].focus(); }
    };
    element.addEventListener('keydown', onKey);
    return () => { element.removeEventListener('keydown', onKey); previous?.focus(); };
  }, [job.id]);
  useEffect(() => { const timer = setInterval(() => setNow(Date.now()), 1000); return () => clearInterval(timer); }, []);
  const controlled = ['restart', 'stop', 'start'].includes(job.kind);
  const retry = (job.kind === 'deploy' && ['failed', 'cancelled'].includes(job.status)) || (job.kind === 'logs' && ['failed', 'cancelled', 'timed_out'].includes(job.status));
  async function execute() {
    setBusy(true); setMessage('');
    try { await post(`/jobs/${job.id}/${action}`, {}); setAction(''); setMessage(action === 'cancel' ? 'Queued work cancelled.' : 'A new attempt has been queued.'); }
    catch (error) { setMessage(error.message); }
    finally { setBusy(false); }
  }
  function download() {
    const url = URL.createObjectURL(new Blob([job.output || 'No output recorded.'], { type: 'text/plain' }));
    const link = document.createElement('a'); link.href = url; link.download = `stakraf-${job.id}.log`; link.click(); URL.revokeObjectURL(url);
  }
  const events = [['QUEUED', job.created_at, true], ['HOST EXECUTION', job.started_at, Boolean(job.started_at)], ['RESULT', job.finished_at, terminal(job.status)]];
  return <div className="scrim" onMouseDown={event => event.target === event.currentTarget && !busy && close()}><section className="dialog execution-dialog" role="dialog" aria-modal="true" aria-labelledby="execution-title" ref={dialog}>
    <header className="dialog-head"><div><span className="kicker">EXECUTION INSPECTOR / {job.kind.toUpperCase()}</span><h2 id="execution-title">{job.app_name}</h2><p>{job.server_name} · {job.project_name}</p></div><button className="icon-button" aria-label="Close execution inspector" onClick={close}><X size={18} /></button></header>
    <div className="execution-body"><div className="execution-heading"><Status status={job.status} /><span className="mono">{job.id}</span></div>
      <div className="execution-timeline">{events.map(([label, at, done], index) => <div className={done ? 'done' : ''} key={label}><span className="execution-step">{done ? <Check size={13} /> : index + 1}</span><div><strong>{label}</strong><small>{at ? date(at) : job.status === 'cancelled' ? 'Not executed' : 'Waiting'}</small></div></div>)}</div>
      <div className="execution-facts"><div><small>EXECUTION TIME</small><strong>{duration(job, now)}</strong></div><div><small>EXECUTION WINDOW</small><strong>{job.lease_expires_at ? date(job.lease_expires_at) : 'Assigned on host claim'}</strong></div><div><small>CONTAINER IMAGE</small><strong>{job.image}</strong></div>{job.retry_of && <div><small>RETRY OF</small><strong>{job.retry_of}</strong></div>}</div>
      {job.status === 'timed_out' && <div className="execution-warning"><AlertCircle size={18} /><p>The host did not confirm a result within its execution window. Application state is unknown. Inspect the host before requesting another operation.</p></div>}
      <div className="execution-output-head"><span><Terminal size={15} /> AGENT OUTPUT</span><div><button className="icon-button" aria-label="Copy job output" onClick={async () => { try { await navigator.clipboard.writeText(job.output || ''); setMessage('Output copied.'); } catch { setMessage('Clipboard unavailable; download the output instead.'); } }}><Copy size={15} /></button><button className="icon-button" aria-label="Download job output" onClick={download}><Download size={15} /></button></div></div>
      <pre className="execution-output">{job.output || (job.status === 'queued' ? 'Waiting for the host to claim this job.' : 'The agent returns output when execution completes.')}</pre>
      {action && <div className="execution-confirm"><strong>{action === 'cancel' ? 'Cancel this queued operation?' : 'Queue a new attempt?'}</strong><p>{action === 'cancel' ? 'The operation will be removed from the host queue. Running work cannot be cancelled here.' : 'The previous attempt stays in the audit history. Existing containers will not be replaced automatically.'}</p><div><button className="btn subtle" disabled={busy} onClick={() => setAction('')}>Keep current state</button><button className="btn" disabled={busy} onClick={execute}>{busy ? 'Working…' : action === 'cancel' ? 'Confirm cancellation' : 'Confirm retry'}</button></div></div>}
      {message && <p className="execution-feedback" role="status">{message}</p>}
      <div className="execution-footer"><small>{job.status === 'running' ? 'The host is executing this operation.' : 'Each attempt is preserved in execution history.'}</small><div>{job.status === 'queued' && <button className="btn subtle" disabled={busy} onClick={() => setAction('cancel')}>Cancel queued job</button>}{retry && <button className="btn" disabled={busy} onClick={() => setAction('retry')}><RotateCcw size={15} /> Retry operation</button>}{controlled && ['failed', 'timed_out', 'cancelled'].includes(job.status) && <button className="btn" onClick={() => requestChange(job)}><ShieldCheck size={15} /> Request new approval</button>}</div></div>
    </div>
  </section></div>;
}
