package main

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"
)

// Operations API: every query is owner-scoped; all mutations create audit events.
func (a *API) audit(ctx context.Context, uid, projectID, typ, resource, rid, details string) {
	var project any
	if projectID != "" {
		project = projectID
	}
	_, _ = a.db.ExecContext(ctx, `INSERT INTO audit_events(id,user_id,project_id,event_type,resource_type,resource_id,details) VALUES($1,$2,$3,$4,$5,$6,$7)`, id(), uid, project, typ, resource, rid, details)
}
func (a *API) apiKeyUser(r *http.Request) string {
	uid, scope := a.keyIdentity(r)
	if scope != "api" {
		return ""
	}
	return uid
}
func (a *API) keyIdentity(r *http.Request) (string, string) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer stk_") {
		return "", ""
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	var uid, scope string
	if a.db.QueryRowContext(r.Context(), `SELECT user_id,scope FROM api_keys WHERE token_hash=$1 AND revoked_at IS NULL`, hash(token)).Scan(&uid, &scope) != nil {
		return "", ""
	}
	_, _ = a.db.ExecContext(r.Context(), `UPDATE api_keys SET last_used_at=now() WHERE token_hash=$1`, hash(token))
	return uid, scope
}
func (a *API) operations(w http.ResponseWriter, r *http.Request, u string) bool {
	p := r.URL.Path
	switch {
	case p == "/api/overview" && r.Method == "GET":
		a.overview(w, r, u)
	case p == "/api/telemetry" && r.Method == "GET":
		a.telemetry(w, r, u)
	case p == "/api/incidents" && r.Method == "GET":
		a.listIncidents(w, r, u)
	case p == "/api/incidents" && r.Method == "POST":
		a.createIncident(w, r, u)
	case strings.HasPrefix(p, "/api/incidents/"):
		a.incidentRoute(w, r, u)
	case p == "/api/changes" && r.Method == "GET":
		a.listChanges(w, r, u)
	case p == "/api/changes" && r.Method == "POST":
		a.createChange(w, r, u)
	case strings.HasPrefix(p, "/api/changes/") && r.Method == "POST":
		a.decideChange(w, r, u)
	case p == "/api/audit" && r.Method == "GET":
		a.auditList(w, r, u)
	case p == "/api/keys" && r.Method == "GET":
		a.keys(w, r, u)
	case p == "/api/keys" && r.Method == "POST":
		a.createKey(w, r, u)
	case strings.HasPrefix(p, "/api/keys/") && r.Method == "DELETE":
		a.revokeKey(w, r, u)
	case p == "/api/settings" && r.Method == "GET":
		a.settings(w, r, u)
	case p == "/api/settings" && r.Method == "PATCH":
		a.updateSettings(w, r, u)
	default:
		return false
	}
	return true
}
func (a *API) overview(w http.ResponseWriter, r *http.Request, u string) {
	var ps, ss, online, app, run, pending, inc, changes int
	e := a.db.QueryRowContext(r.Context(), `SELECT (SELECT count(*) FROM projects WHERE owner_id=$1),
 (SELECT count(*) FROM servers s JOIN projects p ON s.project_id=p.id WHERE p.owner_id=$1),
 (SELECT count(*) FROM servers s JOIN projects p ON s.project_id=p.id WHERE p.owner_id=$1 AND s.last_seen>now()-interval '45 seconds'),
 (SELECT count(*) FROM apps a JOIN projects p ON a.project_id=p.id WHERE p.owner_id=$1),
 (SELECT count(*) FROM apps a JOIN projects p ON a.project_id=p.id WHERE p.owner_id=$1 AND a.status='running'),
 (SELECT count(*) FROM jobs j JOIN apps a ON j.app_id=a.id JOIN projects p ON a.project_id=p.id WHERE p.owner_id=$1 AND j.status IN ('queued','running')),
 (SELECT count(*) FROM incidents i JOIN projects p ON i.project_id=p.id WHERE p.owner_id=$1 AND i.status!='resolved'),
 (SELECT count(*) FROM change_requests c JOIN projects p ON c.project_id=p.id WHERE p.owner_id=$1 AND c.status='pending')`, u).Scan(&ps, &ss, &online, &app, &run, &pending, &inc, &changes)
	if e != nil {
		fail(w, 500, "Unable to load operational overview")
		return
	}
	jsonOut(w, 200, M{"projects": ps, "servers": ss, "online_servers": online, "apps": app, "running_apps": run, "pending_jobs": pending, "open_incidents": inc, "pending_changes": changes})
}
func (a *API) telemetry(w http.ResponseWriter, r *http.Request, u string) {
	sid := r.URL.Query().Get("server_id")
	if sid == "" || !a.ownsServer(r.Context(), u, sid) {
		fail(w, 404, "Server not found")
		return
	}
	rows, e := a.db.QueryContext(r.Context(), `SELECT recorded_at,cpu,memory,disk FROM (SELECT recorded_at,cpu,memory,disk FROM telemetry WHERE server_id=$1 AND recorded_at>now()-interval '1 day' ORDER BY recorded_at DESC LIMIT 1440) t ORDER BY recorded_at`, sid)
	if e != nil {
		fail(w, 500, "Telemetry unavailable")
		return
	}
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var t time.Time
		var c, m, d float64
		if rows.Scan(&t, &c, &m, &d) == nil {
			data = append(data, M{"at": t, "cpu": c, "memory": m, "disk": d})
		}
	}
	jsonOut(w, 200, data)
}
func oneOf(v string, vs ...string) bool {
	for _, s := range vs {
		if v == s {
			return true
		}
	}
	return false
}
func (a *API) listChanges(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT c.id,c.app_id,c.project_id,c.kind,c.reason,c.status,c.created_at,c.decided_at,a.name,s.name,coalesce(c.job_id,'') FROM change_requests c JOIN projects p ON c.project_id=p.id JOIN apps a ON a.id=c.app_id JOIN servers s ON a.server_id=s.id WHERE p.owner_id=$1 ORDER BY c.created_at DESC LIMIT 200`, u)
	if e != nil {
		fail(w, 500, "Could not load changes")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var i, app, p, kind, reason, status, name, server, job string
		var created time.Time
		var decided sql.NullTime
		if rows.Scan(&i, &app, &p, &kind, &reason, &status, &created, &decided, &name, &server, &job) == nil {
			out = append(out, M{"id": i, "app_id": app, "project_id": p, "kind": kind, "reason": reason, "status": status, "created_at": created, "decided_at": decided.Time, "app_name": name, "server_name": server, "job_id": job})
		}
	}
	jsonOut(w, 200, out)
}
func (a *API) createChange(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		AppID  string `json:"app_id"`
		Kind   string `json:"kind"`
		Reason string `json:"reason"`
	}
	if read(r, &x) != nil || !oneOf(x.Kind, "restart", "stop", "start") || len(x.Reason) > 500 || !a.ownsApp(r.Context(), u, x.AppID) {
		fail(w, 400, "Invalid change request")
		return
	}
	var pid string
	_ = a.db.QueryRowContext(r.Context(), "SELECT project_id FROM apps WHERE id=$1", x.AppID).Scan(&pid)
	i := id()
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO change_requests(id,app_id,project_id,kind,reason,requested_by) VALUES($1,$2,$3,$4,$5,$6)`, i, x.AppID, pid, x.Kind, strings.TrimSpace(x.Reason), u)
	if e != nil {
		fail(w, 500, "Could not create change")
		return
	}
	a.audit(r.Context(), u, pid, "change.requested", "change", i, x.Kind)
	jsonOut(w, 201, M{"id": i, "status": "pending"})
}
func (a *API) decideChange(w http.ResponseWriter, r *http.Request, u string) {
	cid := strings.TrimSuffix(pathID(r.URL.Path, "/api/changes/"), "/decision")
	if !strings.HasSuffix(r.URL.Path, "/decision") {
		fail(w, 404, "Not found")
		return
	}
	var x struct {
		Decision string `json:"decision"`
	}
	if read(r, &x) != nil || !oneOf(x.Decision, "approve", "reject") {
		fail(w, 400, "Invalid decision")
		return
	}
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Could not begin decision")
		return
	}
	defer tx.Rollback()
	var app, pid, kind, sid string
	e = tx.QueryRowContext(r.Context(), `SELECT c.app_id,c.project_id,c.kind,a.server_id FROM change_requests c JOIN projects p ON p.id=c.project_id JOIN apps a ON a.id=c.app_id WHERE c.id=$1 AND p.owner_id=$2 AND c.status='pending' FOR UPDATE OF c`, cid, u).Scan(&app, &pid, &kind, &sid)
	if e != nil {
		fail(w, 404, "Pending change not found")
		return
	}
	status := "rejected"
	var job any
	if x.Decision == "approve" {
		status = "approved"
		jid := id()
		_, e = tx.ExecContext(r.Context(), "INSERT INTO jobs(id,server_id,app_id,kind) VALUES($1,$2,$3,$4)", jid, sid, app, kind)
		if e != nil {
			fail(w, 500, "Cannot queue approved operation")
			return
		}
		job = jid
	}
	_, e = tx.ExecContext(r.Context(), `UPDATE change_requests SET status=$1,decided_by=$2,decided_at=now(),job_id=$3 WHERE id=$4`, status, u, job, cid)
	if e != nil || tx.Commit() != nil {
		fail(w, 500, "Could not complete decision")
		return
	}
	a.audit(r.Context(), u, pid, "change."+status, "change", cid, kind)
	jsonOut(w, 200, M{"ok": true, "status": status, "job_id": job})
}
func (a *API) auditList(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,event_type,resource_type,resource_id,details,created_at FROM audit_events WHERE user_id=$1 ORDER BY created_at DESC LIMIT 200`, u)
	if e != nil {
		fail(w, 500, "Audit unavailable")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var i, t, rt, rid, d string
		var at time.Time
		if rows.Scan(&i, &t, &rt, &rid, &d, &at) == nil {
			out = append(out, M{"id": i, "event_type": t, "resource_type": rt, "resource_id": rid, "details": d, "created_at": at})
		}
	}
	jsonOut(w, 200, out)
}
func (a *API) keys(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,label,created_at,last_used_at,scope FROM api_keys WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, u)
	if e != nil {
		fail(w, 500, "API keys unavailable")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var i, l, scope string
		var cr time.Time
		var used sql.NullTime
		if rows.Scan(&i, &l, &cr, &used, &scope) == nil {
			out = append(out, M{"id": i, "label": l, "created_at": cr, "last_used_at": used.Time, "scope": scope})
		}
	}
	jsonOut(w, 200, out)
}
func (a *API) createKey(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		Label string `json:"label"`
		Scope string `json:"scope"`
	}
	if read(r, &x) != nil || len(strings.TrimSpace(x.Label)) < 2 || len(x.Label) > 70 {
		fail(w, 400, "Invalid API key label")
		return
	}
	key := "stk_" + secret(32)
	if x.Scope == "" {
		x.Scope = "api"
	}
	if !oneOf(x.Scope, "api", "mcp_read", "mcp_propose") {
		fail(w, 400, "Invalid key scope")
		return
	}
	i := id()
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO api_keys(id,user_id,label,token_hash,scope) VALUES($1,$2,$3,$4,$5)`, i, u, strings.TrimSpace(x.Label), hash(key), x.Scope)
	if e != nil {
		fail(w, 500, "Could not issue key")
		return
	}
	a.audit(r.Context(), u, "", "access.key_created", "api_key", i, x.Label)
	jsonOut(w, 201, M{"id": i, "label": x.Label, "key": key, "scope": x.Scope})
}
func (a *API) revokeKey(w http.ResponseWriter, r *http.Request, u string) {
	i := pathID(r.URL.Path, "/api/keys/")
	res, e := a.db.ExecContext(r.Context(), `UPDATE api_keys SET revoked_at=now() WHERE id=$1 AND user_id=$2 AND revoked_at IS NULL`, i, u)
	if e != nil {
		fail(w, 500, "Cannot revoke key")
		return
	}
	count, _ := res.RowsAffected()
	if count == 0 {
		fail(w, 404, "Key not found")
		return
	}
	a.audit(r.Context(), u, "", "access.key_revoked", "api_key", i, "")
	jsonOut(w, 200, M{"ok": true})
}
func (a *API) settings(w http.ResponseWriter, r *http.Request, u string) {
	var n, t string
	e := a.db.QueryRowContext(r.Context(), `SELECT name,timezone FROM workspace_settings WHERE user_id=$1`, u).Scan(&n, &t)
	if e == sql.ErrNoRows {
		jsonOut(w, 200, M{"name": "My workspace", "timezone": "UTC"})
		return
	}
	if e != nil {
		fail(w, 500, "Cannot load settings")
		return
	}
	jsonOut(w, 200, M{"name": n, "timezone": t})
}
func (a *API) updateSettings(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		Name     string `json:"name"`
		Timezone string `json:"timezone"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid settings")
		return
	}
	x.Name = strings.TrimSpace(x.Name)
	x.Timezone = strings.TrimSpace(x.Timezone)
	if len(x.Name) < 2 || len(x.Name) > 80 || len(x.Timezone) < 2 || len(x.Timezone) > 80 || strings.ContainsAny(x.Timezone, "\x00\n\r") {
		fail(w, 400, "Invalid settings")
		return
	}
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO workspace_settings(user_id,name,timezone) VALUES($1,$2,$3) ON CONFLICT(user_id) DO UPDATE SET name=EXCLUDED.name,timezone=EXCLUDED.timezone,updated_at=now()`, u, x.Name, x.Timezone)
	if e != nil {
		fail(w, 500, "Could not save settings")
		return
	}
	a.audit(r.Context(), u, "", "workspace.updated", "workspace", u, x.Name)
	jsonOut(w, 200, M{"ok": true})
}
