package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const incidentSelect = `SELECT i.id,i.project_id,p.name,coalesce(i.server_id,''),coalesce(s.name,''),coalesce(i.app_id,''),coalesce(a.name,''),coalesce(i.job_id,''),i.title,i.description,i.severity,i.status,i.source,i.responder,i.impact,i.root_cause,i.resolution,i.follow_up,i.version,i.created_at,i.updated_at,i.acknowledged_at,i.resolved_at FROM incidents i JOIN projects p ON p.id=i.project_id LEFT JOIN servers s ON s.id=i.server_id LEFT JOIN apps a ON a.id=i.app_id `

type incidentScanner interface{ Scan(...any) error }

func scanIncident(row incidentScanner) (M, error) {
	var i, p, pn, s, sn, a, an, j, title, description, severity, status, source, responder, impact, root, resolution, follow string
	var version int
	var created, updated time.Time
	var ack, resolved sql.NullTime
	err := row.Scan(&i, &p, &pn, &s, &sn, &a, &an, &j, &title, &description, &severity, &status, &source, &responder, &impact, &root, &resolution, &follow, &version, &created, &updated, &ack, &resolved)
	return M{"id": i, "project_id": p, "project_name": pn, "server_id": s, "server_name": sn, "app_id": a, "app_name": an, "job_id": j, "title": title, "description": description, "severity": severity, "status": status, "source": source, "responder": responder, "impact": impact, "root_cause": root, "resolution": resolution, "follow_up": follow, "version": version, "created_at": created, "updated_at": updated, "acknowledged_at": nullableTime(ack), "resolved_at": nullableTime(resolved)}, err
}
func (a *API) listIncidents(w http.ResponseWriter, r *http.Request, u string) {
	rows, err := a.db.QueryContext(r.Context(), incidentSelect+`WHERE p.owner_id=$1 ORDER BY CASE WHEN i.status='resolved' THEN 1 ELSE 0 END,i.created_at DESC,i.id DESC LIMIT 300`, u)
	if err != nil {
		fail(w, 500, "Could not load incidents")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		v, err := scanIncident(rows)
		if err != nil {
			fail(w, 500, "Could not read incident")
			return
		}
		out = append(out, v)
	}
	if rows.Err() != nil {
		fail(w, 500, "Could not read incidents")
		return
	}
	jsonOut(w, 200, out)
}
func (a *API) createIncident(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		ProjectID   string `json:"project_id"`
		ServerID    string `json:"server_id"`
		AppID       string `json:"app_id"`
		JobID       string `json:"job_id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Severity    string `json:"severity"`
		Impact      string `json:"impact"`
		Responder   string `json:"responder"`
	}
	if read(r, &x) != nil || !a.ownsProject(r.Context(), u, x.ProjectID) || len(strings.TrimSpace(x.Title)) < 4 || len(x.Title) > 140 || len(x.Description) > 3000 || len(x.Impact) > 2000 || len(x.Responder) > 80 || !oneOf(x.Severity, "critical", "high", "medium", "low") {
		fail(w, 400, "Invalid incident fields")
		return
	}
	if x.JobID != "" {
		var aid, sid string
		if a.db.QueryRowContext(r.Context(), `SELECT j.app_id,j.server_id FROM jobs j JOIN apps a ON a.id=j.app_id WHERE j.id=$1 AND a.project_id=$2`, x.JobID, x.ProjectID).Scan(&aid, &sid) != nil || (x.AppID != "" && x.AppID != aid) || (x.ServerID != "" && x.ServerID != sid) {
			fail(w, 400, "Job must belong to the incident project and target")
			return
		}
		x.AppID, x.ServerID = aid, sid
	}
	if x.AppID != "" {
		var sid string
		if a.db.QueryRowContext(r.Context(), "SELECT server_id FROM apps WHERE id=$1 AND project_id=$2", x.AppID, x.ProjectID).Scan(&sid) != nil || (x.ServerID != "" && x.ServerID != sid) {
			fail(w, 400, "Application must belong to the incident project and server")
			return
		}
		x.ServerID = sid
	}
	if x.ServerID != "" {
		var found bool
		if a.db.QueryRowContext(r.Context(), "SELECT EXISTS(SELECT 1 FROM servers WHERE id=$1 AND project_id=$2)", x.ServerID, x.ProjectID).Scan(&found) != nil || !found {
			fail(w, 400, "Server must belong to the incident project")
			return
		}
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Cannot begin incident")
		return
	}
	defer tx.Rollback()
	iid := id()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO incidents(id,project_id,server_id,app_id,job_id,title,description,severity,impact,responder) VALUES($1,$2,nullif($3,''),nullif($4,''),nullif($5,''),$6,$7,$8,$9,$10)`, iid, x.ProjectID, x.ServerID, x.AppID, x.JobID, strings.TrimSpace(x.Title), x.Description, x.Severity, x.Impact, strings.TrimSpace(x.Responder)); err != nil {
		fail(w, 500, "Cannot save incident")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO incident_events(id,incident_id,actor_id,kind,body) VALUES($1,$2,$3,'created',$4)`, id(), iid, u, x.Title); err != nil || tx.Commit() != nil {
		fail(w, 500, "Cannot commit incident")
		return
	}
	a.audit(r.Context(), u, x.ProjectID, "incident.created", "incident", iid, x.Title)
	jsonOut(w, 201, M{"id": iid, "version": 1})
}
func (a *API) incidentRoute(w http.ResponseWriter, r *http.Request, u string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/incidents/"), "/")
	if len(parts) == 1 && r.Method == "GET" {
		a.incidentDetail(w, r, u, parts[0])
		return
	}
	if len(parts) == 1 && r.Method == "PATCH" {
		a.updateIncident(w, r, u, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "notes" && r.Method == "POST" {
		a.incidentNote(w, r, u, parts[0])
		return
	}
	fail(w, 404, "Incident route not found")
}
func (a *API) incidentDetail(w http.ResponseWriter, r *http.Request, u, iid string) {
	// Repeatable-read snapshot keeps metadata and its timeline consistent.
	tx, err := a.db.BeginTx(r.Context(), &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		fail(w, 500, "Cannot load incident")
		return
	}
	defer tx.Rollback()
	v, err := scanIncident(tx.QueryRowContext(r.Context(), incidentSelect+"WHERE p.owner_id=$1 AND i.id=$2", u, iid))
	if err == sql.ErrNoRows {
		fail(w, 404, "Incident not found")
		return
	}
	if err != nil {
		fail(w, 500, "Cannot load incident")
		return
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT e.id,e.kind,e.body,e.created_at,coalesce(u.email,'System') FROM (SELECT * FROM incident_events WHERE incident_id=$1 ORDER BY created_at DESC,id DESC LIMIT 200) e LEFT JOIN users u ON u.id=e.actor_id ORDER BY e.created_at,e.id`, iid)
	if err != nil {
		fail(w, 500, "Timeline unavailable")
		return
	}
	events := []M{}
	for rows.Next() {
		var id, kind, body, actor string
		var at time.Time
		if rows.Scan(&id, &kind, &body, &at, &actor) != nil {
			rows.Close()
			fail(w, 500, "Cannot read timeline")
			return
		}
		events = append(events, M{"id": id, "kind": kind, "body": body, "actor": actor, "created_at": at})
	}
	err = rows.Err()
	rows.Close()
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "Timeline unavailable")
		return
	}
	v["events"] = events
	v["timeline_limit"] = 200
	jsonOut(w, 200, v)
}

type incidentUpdate struct {
	Version    int     `json:"version"`
	Status     *string `json:"status"`
	Severity   *string `json:"severity"`
	Responder  *string `json:"responder"`
	Impact     *string `json:"impact"`
	RootCause  *string `json:"root_cause"`
	Resolution *string `json:"resolution"`
	FollowUp   *string `json:"follow_up"`
	Note       string  `json:"note"`
}

func validIncidentUpdate(x incidentUpdate) bool {
	if x.Version < 1 || len(x.Note) > 3000 {
		return false
	}
	if x.Status != nil && !oneOf(*x.Status, "open", "acknowledged", "investigating", "monitoring", "resolved") {
		return false
	}
	if x.Severity != nil && !oneOf(*x.Severity, "critical", "high", "medium", "low") {
		return false
	}
	for _, v := range []struct {
		s   *string
		max int
	}{{x.Responder, 80}, {x.Impact, 2000}, {x.RootCause, 3000}, {x.Resolution, 3000}, {x.FollowUp, 3000}} {
		if v.s != nil && len(*v.s) > v.max {
			return false
		}
	}
	return x.Status != nil || x.Severity != nil || x.Responder != nil || x.Impact != nil || x.RootCause != nil || x.Resolution != nil || x.FollowUp != nil
}
func (a *API) updateIncident(w http.ResponseWriter, r *http.Request, u, iid string) {
	var x incidentUpdate
	if read(r, &x) != nil || !validIncidentUpdate(x) {
		fail(w, 400, "Valid incident fields and current version required")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Cannot update incident")
		return
	}
	defer tx.Rollback()
	var pid, status, severity, responder, impact, root, resolution, follow string
	var version int
	err = tx.QueryRowContext(r.Context(), `SELECT i.project_id,i.status,i.severity,i.responder,i.impact,i.root_cause,i.resolution,i.follow_up,i.version FROM incidents i JOIN projects p ON p.id=i.project_id WHERE i.id=$1 AND p.owner_id=$2 FOR UPDATE OF i`, iid, u).Scan(&pid, &status, &severity, &responder, &impact, &root, &resolution, &follow, &version)
	if err == sql.ErrNoRows {
		fail(w, 404, "Incident not found")
		return
	}
	if err != nil {
		fail(w, 500, "Cannot load incident")
		return
	}
	if version != x.Version {
		fail(w, 409, "Incident changed. Reload before saving.")
		return
	}
	old := status
	changes := []string{}
	pairs := []struct {
		dst  *string
		src  *string
		name string
	}{{&status, x.Status, "Status"}, {&severity, x.Severity, "Severity"}, {&responder, x.Responder, "Responder"}, {&impact, x.Impact, "Impact"}, {&root, x.RootCause, "Root cause"}, {&resolution, x.Resolution, "Resolution"}, {&follow, x.FollowUp, "Follow-up"}}
	for _, p := range pairs {
		if p.src != nil {
			if *p.dst != strings.TrimSpace(*p.src) {
				changes = append(changes, p.name+": "+strings.TrimSpace(*p.src))
			}
			*p.dst = strings.TrimSpace(*p.src)
		}
	}
	if old == "resolved" && status != "resolved" {
		if len(strings.TrimSpace(x.Note)) < 10 {
			fail(w, 400, "Reopening requires a reason of at least 10 characters")
			return
		}
		resolution = ""
	}
	if status == "resolved" && len(resolution) < 10 {
		fail(w, 400, "Resolution summary must contain at least 10 characters")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE incidents SET status=$1,severity=$2,responder=$3,impact=$4,root_cause=$5,resolution=$6,follow_up=$7,version=version+1,updated_at=now(),acknowledged_at=CASE WHEN $1 IN ('acknowledged','investigating','monitoring','resolved') THEN coalesce(acknowledged_at,now()) ELSE acknowledged_at END,resolved_at=CASE WHEN $1='resolved' THEN coalesce(resolved_at,now()) ELSE NULL END WHERE id=$8`, status, severity, responder, impact, root, resolution, follow, iid); err != nil {
		fail(w, 500, "Cannot save incident")
		return
	}
	body := "Updated incident details"
	if len(changes) > 0 {
		body += "\n" + strings.Join(changes, "\n")
	}
	if old != status {
		body = fmt.Sprintf("Status: %s → %s\n%s", old, status, strings.Join(changes, "\n"))
	}
	if x.Note != "" {
		body += "\n" + strings.TrimSpace(x.Note)
	}
	if old != "resolved" && status == "resolved" {
		body += "\nResolution: " + resolution + "\nRoot cause: " + root + "\nFollow-up: " + follow
	}
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO incident_events(id,incident_id,actor_id,kind,body) VALUES($1,$2,$3,'updated',$4)`, id(), iid, u, body); err != nil || tx.Commit() != nil {
		fail(w, 500, "Cannot commit incident")
		return
	}
	a.audit(r.Context(), u, pid, "incident."+status, "incident", iid, body)
	jsonOut(w, 200, M{"ok": true, "version": version + 1})
}
func (a *API) incidentNote(w http.ResponseWriter, r *http.Request, u, iid string) {
	var x struct {
		Body string `json:"body"`
	}
	if read(r, &x) != nil || len(strings.TrimSpace(x.Body)) < 2 || len(x.Body) > 3000 {
		fail(w, 400, "Note must contain 2–3000 characters")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Cannot save note")
		return
	}
	defer tx.Rollback()
	var pid string
	if tx.QueryRowContext(r.Context(), `SELECT i.project_id FROM incidents i JOIN projects p ON p.id=i.project_id WHERE i.id=$1 AND p.owner_id=$2 FOR UPDATE OF i`, iid, u).Scan(&pid) != nil {
		fail(w, 404, "Incident not found")
		return
	}
	eid := id()
	if _, err = tx.ExecContext(r.Context(), `INSERT INTO incident_events(id,incident_id,actor_id,kind,body) VALUES($1,$2,$3,'note',$4)`, eid, iid, u, strings.TrimSpace(x.Body)); err != nil || tx.Commit() != nil {
		fail(w, 500, "Cannot save note")
		return
	}
	a.audit(r.Context(), u, pid, "incident.note_added", "incident", iid, "Investigation note added")
	jsonOut(w, 201, M{"id": eid})
}

func (a *API) failedDeploymentIncident(r *http.Request, tx *sql.Tx, app, job string) error {
	var pid, sid, name string
	if err := tx.QueryRowContext(r.Context(), "SELECT project_id,server_id,name FROM apps WHERE id=$1", app).Scan(&pid, &sid, &name); err != nil {
		return err
	}
	var iid string
	err := tx.QueryRowContext(r.Context(), `INSERT INTO incidents(id,project_id,server_id,app_id,job_id,title,description,severity,source)
 VALUES($1,$2,$3,$4,$5,$6,'A deployment failed. Inspect its execution output and application state before taking action.','high','job_failure')
 ON CONFLICT(app_id) WHERE source='job_failure' AND status!='resolved' DO UPDATE SET job_id=EXCLUDED.job_id,updated_at=now(),version=incidents.version+1 RETURNING id`, id(), pid, sid, app, job, "Deployment failed: "+name).Scan(&iid)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO incident_events(id,incident_id,kind,body) VALUES($1,$2,'deployment_failed',$3)`, id(), iid, "Failed deployment job "+job+". Container restoration, if attempted, is recorded in job output.")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO audit_events(id,user_id,project_id,event_type,resource_type,resource_id,details) SELECT $1,owner_id,id,'incident.deployment_failed','incident',$2,$3 FROM projects WHERE id=$4`, id(), iid, "Failed deployment job "+job, pid)
	return err
}
