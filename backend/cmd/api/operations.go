package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer stk_") {
		return ""
	}
	token := strings.TrimPrefix(auth, "Bearer ")
	var uid string
	if a.db.QueryRowContext(r.Context(), `SELECT user_id FROM api_keys WHERE token_hash=$1 AND revoked_at IS NULL`, hash(token)).Scan(&uid) != nil {
		return ""
	}
	_, _ = a.db.ExecContext(r.Context(), `UPDATE api_keys SET last_used_at=now() WHERE token_hash=$1`, hash(token))
	return uid
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
	case strings.HasPrefix(p, "/api/incidents/") && r.Method == "PATCH":
		a.updateIncident(w, r, u)
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
func (a *API) listIncidents(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT i.id,i.project_id,p.name,coalesce(i.server_id,''),i.title,i.description,i.severity,i.status,i.created_at,i.updated_at FROM incidents i JOIN projects p ON i.project_id=p.id WHERE p.owner_id=$1 ORDER BY CASE WHEN i.status='resolved' THEN 1 ELSE 0 END,i.created_at DESC LIMIT 300`, u)
	if e != nil {
		fail(w, 500, "Could not load incidents")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var id, pr, pn, s, title, desc, sev, status string
		var cr, up time.Time
		if rows.Scan(&id, &pr, &pn, &s, &title, &desc, &sev, &status, &cr, &up) == nil {
			out = append(out, M{"id": id, "project_id": pr, "project_name": pn, "server_id": s, "title": title, "description": desc, "severity": sev, "status": status, "created_at": cr, "updated_at": up})
		}
	}
	jsonOut(w, 200, out)
}
func (a *API) createIncident(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		ProjectID   string `json:"project_id"`
		ServerID    string `json:"server_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Severity    string `json:"severity"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid incident")
		return
	}
	x.Title = strings.TrimSpace(x.Title)
	if !a.ownsProject(r.Context(), u, x.ProjectID) || len(x.Title) < 4 || len(x.Title) > 140 || len(x.Description) > 3000 || !oneOf(x.Severity, "critical", "high", "medium", "low") {
		fail(w, 400, "Invalid project, title, or severity")
		return
	}
	var sid any
	if x.ServerID != "" {
		if !a.ownsServer(r.Context(), u, x.ServerID) {
			fail(w, 400, "Invalid server")
			return
		}
		var belongs bool
		if a.db.QueryRowContext(r.Context(), "SELECT project_id=$1 FROM servers WHERE id=$2", x.ProjectID, x.ServerID).Scan(&belongs) != nil || !belongs {
			fail(w, 400, "Server must belong to incident project")
			return
		}
		sid = x.ServerID
	}
	i := id()
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO incidents(id,project_id,server_id,title,description,severity) VALUES($1,$2,$3,$4,$5,$6)`, i, x.ProjectID, sid, x.Title, x.Description, x.Severity)
	if e != nil {
		fail(w, 500, "Could not record incident")
		return
	}
	a.audit(r.Context(), u, x.ProjectID, "incident.created", "incident", i, x.Title)
	jsonOut(w, 201, M{"id": i})
}
func (a *API) updateIncident(w http.ResponseWriter, r *http.Request, u string) {
	iid := pathID(r.URL.Path, "/api/incidents/")
	var status struct {
		Status string `json:"status"`
	}
	if read(r, &status) != nil || !oneOf(status.Status, "open", "acknowledged", "resolved") {
		fail(w, 400, "Invalid incident status")
		return
	}
	var pid string
	e := a.db.QueryRowContext(r.Context(), `UPDATE incidents i SET status=$1,updated_at=now() FROM projects p WHERE i.id=$2 AND p.id=i.project_id AND p.owner_id=$3 RETURNING i.project_id`, status.Status, iid, u).Scan(&pid)
	if e != nil {
		fail(w, 404, "Incident not found")
		return
	}
	a.audit(r.Context(), u, pid, "incident."+status.Status, "incident", iid, "")
	jsonOut(w, 200, M{"ok": true})
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
	rows, e := a.db.QueryContext(r.Context(), `SELECT id,label,created_at,last_used_at FROM api_keys WHERE user_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC`, u)
	if e != nil {
		fail(w, 500, "API keys unavailable")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var i, l string
		var cr time.Time
		var used sql.NullTime
		if rows.Scan(&i, &l, &cr, &used) == nil {
			out = append(out, M{"id": i, "label": l, "created_at": cr, "last_used_at": used.Time})
		}
	}
	jsonOut(w, 200, out)
}
func (a *API) createKey(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		Label string `json:"label"`
	}
	if read(r, &x) != nil || len(strings.TrimSpace(x.Label)) < 2 || len(x.Label) > 70 {
		fail(w, 400, "Invalid API key label")
		return
	}
	key := "stk_" + secret(32)
	i := id()
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO api_keys(id,user_id,label,token_hash) VALUES($1,$2,$3,$4)`, i, u, strings.TrimSpace(x.Label), hash(key))
	if e != nil {
		fail(w, 500, "Could not issue key")
		return
	}
	a.audit(r.Context(), u, "", "access.key_created", "api_key", i, x.Label)
	jsonOut(w, 201, M{"id": i, "label": x.Label, "key": key})
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

// JSON-RPC MCP over HTTP. No proprietary model, only narrowly scoped platform tools.
func (a *API) mcp(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "Use POST")
		return
	}
	u := a.apiKeyUser(r)
	if u == "" {
		fail(w, 401, "Bearer API key required")
		return
	}
	var x struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if read(r, &x) != nil || x.JSONRPC != "2.0" {
		fail(w, 400, "Invalid JSON-RPC request")
		return
	}
	if x.Method == "notifications/initialized" {
		w.WriteHeader(202)
		return
	}
	result := any(nil)
	switch x.Method {
	case "initialize":
		result = M{"protocolVersion": "2025-03-26", "capabilities": M{"tools": M{"listChanged": false}}, "serverInfo": M{"name": "stakraf-system", "version": "0.2.0"}}
	case "ping":
		result = M{}
	case "tools/list":
		result = M{"tools": []M{
			{"name": "list_projects", "description": "Read your Stakraf projects", "inputSchema": M{"type": "object", "properties": M{}}},
			{"name": "list_servers", "description": "Read authorized connected servers and health", "inputSchema": M{"type": "object", "properties": M{}}},
			{"name": "list_applications", "description": "Read your deployed applications", "inputSchema": M{"type": "object", "properties": M{}}},
			{"name": "list_incidents", "description": "Read reported incidents", "inputSchema": M{"type": "object", "properties": M{}}},
			{"name": "get_operations_summary", "description": "Read the infrastructure operations overview", "inputSchema": M{"type": "object", "properties": M{}}},
		}}
	case "tools/call":
		var args struct {
			Name      string `json:"name"`
			Arguments M      `json:"arguments"`
		}
		if json.Unmarshal(x.Params, &args) != nil {
			fail(w, 400, "Invalid tool call")
			return
		}
		var query string
		switch args.Name {
		case "list_projects":
			query = `SELECT coalesce(json_agg(row_to_json(t)),'[]'::json) FROM (SELECT id,name,description FROM projects WHERE owner_id=$1 ORDER BY created_at DESC LIMIT 100) t`
		case "list_servers":
			query = `SELECT coalesce(json_agg(row_to_json(t)),'[]'::json) FROM (SELECT s.id,s.name,s.hostname,s.netbird_status,s.cpu_percent,s.memory_percent,s.disk_percent,(s.last_seen>now()-interval '45 seconds') online FROM servers s JOIN projects p ON s.project_id=p.id WHERE p.owner_id=$1 LIMIT 100) t`
		case "list_applications":
			query = `SELECT coalesce(json_agg(row_to_json(t)),'[]'::json) FROM (SELECT a.id,a.name,a.image,a.status,a.last_error FROM apps a JOIN projects p ON a.project_id=p.id WHERE p.owner_id=$1 LIMIT 100) t`
		case "list_incidents":
			query = `SELECT coalesce(json_agg(row_to_json(t)),'[]'::json) FROM (SELECT i.id,i.title,i.severity,i.status FROM incidents i JOIN projects p ON i.project_id=p.id WHERE p.owner_id=$1 ORDER BY i.created_at DESC LIMIT 100) t`
		case "get_operations_summary":
			query = `SELECT json_build_object('projects',(SELECT count(*) FROM projects WHERE owner_id=$1),'servers',(SELECT count(*) FROM servers s JOIN projects p ON p.id=s.project_id WHERE p.owner_id=$1),'applications',(SELECT count(*) FROM apps a JOIN projects p ON p.id=a.project_id WHERE p.owner_id=$1),'open_incidents',(SELECT count(*) FROM incidents i JOIN projects p ON p.id=i.project_id WHERE p.owner_id=$1 AND i.status!='resolved'))`
		default:
			jsonOut(w, 200, M{"jsonrpc": "2.0", "id": x.ID, "error": M{"code": -32601, "message": "Unknown tool"}})
			return
		}
		var raw []byte
		err := a.db.QueryRowContext(r.Context(), query, u).Scan(&raw)
		if err != nil {
			jsonOut(w, 200, M{"jsonrpc": "2.0", "id": x.ID, "error": M{"code": -32000, "message": "Tool query failed"}})
			return
		}
		result = M{"content": []M{{"type": "text", "text": string(raw)}}, "isError": false}
	default:
		jsonOut(w, 200, M{"jsonrpc": "2.0", "id": x.ID, "error": M{"code": -32601, "message": fmt.Sprintf("Unsupported method %s", x.Method)}})
		return
	}
	jsonOut(w, 200, M{"jsonrpc": "2.0", "id": x.ID, "result": result})
}
