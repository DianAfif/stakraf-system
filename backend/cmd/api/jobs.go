package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
)

const expiredJobMessage = "Execution window expired. The host outcome is unknown; inspect the application before requesting another operation."

func nullableTime(t sql.NullTime) any {
	if t.Valid {
		return t.Time
	}
	return nil
}

// A lease outlasts the agent's four-minute execution timeout. Expired work is
// never replayed automatically: Docker may have acted before the host vanished.
func (a *API) expireJobs(ctx context.Context, server, owner string) error {
	_, err := a.db.ExecContext(ctx, `WITH expired AS (
	 UPDATE jobs j SET status='timed_out',finished_at=now(),output=$3
	 WHERE status='running' AND lease_expires_at<=now()
	 AND (j.server_id=$1 OR EXISTS(SELECT 1 FROM apps ap JOIN projects p ON p.id=ap.project_id WHERE ap.id=j.app_id AND p.owner_id=$2))
	 RETURNING app_id,kind
	) UPDATE apps SET status='unknown',last_error=$3 WHERE id IN (
	 SELECT app_id FROM expired WHERE kind!='logs'
	)`, server, owner, expiredJobMessage)
	return err
}

func (a *API) nextJob(w http.ResponseWriter, r *http.Request, sid string) {
	if err := a.expireJobs(r.Context(), sid, ""); err != nil {
		fail(w, 500, "Could not check execution windows")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Job queue unavailable")
		return
	}
	defer tx.Rollback()
	// Lock the server before claiming so concurrent agent polls cannot execute
	// different jobs on the same host at the same time.
	var server string
	if err = tx.QueryRowContext(r.Context(), "SELECT id FROM servers WHERE id=$1 AND agent_token_hash=$2 FOR UPDATE", sid, hash(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))).Scan(&server); err != nil {
		fail(w, 401, "Connector is no longer authorized")
		return
	}
	var i, app, kind string
	var payload json.RawMessage
	err = tx.QueryRowContext(r.Context(), `UPDATE jobs SET status='running',started_at=now(),lease_expires_at=now()+interval '5 minutes'
	 WHERE id=(SELECT id FROM jobs WHERE server_id=$1 AND status='queued'
	 AND NOT EXISTS(SELECT 1 FROM jobs WHERE server_id=$1 AND status='running')
	 ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id,app_id,kind,payload`, sid).Scan(&i, &app, &kind, &payload)
	if err == sql.ErrNoRows {
		jsonOut(w, 200, M{"job": nil})
		return
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "Job queue unavailable")
		return
	}
	jsonOut(w, 200, M{"job": M{"id": i, "app_id": app, "kind": kind, "payload": payload}})
}

func (a *API) completeJob(w http.ResponseWriter, r *http.Request, sid string) {
	i := strings.TrimSuffix(pathID(r.URL.Path, "/api/agent/jobs/"), "/complete")
	var x struct {
		Success bool   `json:"success"`
		Output  string `json:"output"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid completion")
		return
	}
	if len(x.Output) > 10000 {
		x.Output = x.Output[:10000]
	}
	status := "failed"
	if x.Success {
		status = "success"
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Could not record result")
		return
	}
	defer tx.Rollback()
	var app, kind, previous, output string
	var valid bool
	err = tx.QueryRowContext(r.Context(), `SELECT app_id,kind,status,output,coalesce(lease_expires_at>now(),false)
	 FROM jobs WHERE id=$1 AND server_id=$2 FOR UPDATE`, i, sid).Scan(&app, &kind, &previous, &output, &valid)
	if err == sql.ErrNoRows {
		fail(w, 409, "Unknown job")
		return
	}
	if err != nil {
		fail(w, 500, "Could not record result")
		return
	}
	// A response can be lost after committing. The identical result is safe to
	// acknowledge again, but a conflicting or late result must not change state.
	if previous == status && output == x.Output {
		jsonOut(w, 200, M{"ok": true})
		return
	}
	if previous != "running" || !valid {
		fail(w, 409, "Job is no longer running or its execution window expired")
		return
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE jobs SET status=$1,output=$2,finished_at=now() WHERE id=$3`, status, x.Output, i); err != nil {
		fail(w, 500, "Could not record result")
		return
	}
	if kind != "logs" {
		state, detail := "running", ""
		if kind == "stop" {
			state = "stopped"
		}
		if !x.Success {
			state = "failed"
			detail = x.Output
		}
		if _, err = tx.ExecContext(r.Context(), "UPDATE apps SET status=$1,last_error=$2 WHERE id=$3", state, detail, app); err != nil {
			fail(w, 500, "Could not update application")
			return
		}
	}
	if tx.Commit() != nil {
		fail(w, 500, "Could not commit result")
		return
	}
	jsonOut(w, 200, M{"ok": true})
}

func retryAllowed(kind, status string) bool {
	return (kind == "deploy" && oneOf(status, "failed", "cancelled")) || (kind == "logs" && oneOf(status, "failed", "cancelled", "timed_out"))
}

func (a *API) jobAction(w http.ResponseWriter, r *http.Request, u string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/jobs/"), "/")
	if len(parts) != 2 || parts[0] == "" || !oneOf(parts[1], "cancel", "retry") {
		fail(w, 404, "Job action not found")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Could not begin job action")
		return
	}
	defer tx.Rollback()
	var app, server, project, kind, status string
	var payload json.RawMessage
	err = tx.QueryRowContext(r.Context(), `SELECT j.app_id,j.server_id,a.project_id,j.kind,j.status,j.payload
	 FROM jobs j JOIN apps a ON a.id=j.app_id JOIN projects p ON p.id=a.project_id
	 WHERE j.id=$1 AND p.owner_id=$2 FOR UPDATE OF j`, parts[0], u).Scan(&app, &server, &project, &kind, &status, &payload)
	if err == sql.ErrNoRows {
		fail(w, 404, "Job not found")
		return
	}
	if err != nil {
		fail(w, 500, "Could not inspect job")
		return
	}
	result := M{"ok": true}
	if parts[1] == "cancel" {
		if status != "queued" {
			fail(w, 409, "Only queued work can be cancelled")
			return
		}
		_, err = tx.ExecContext(r.Context(), `UPDATE jobs SET status='cancelled',finished_at=now(),output='Cancelled before host execution.' WHERE id=$1`, parts[0])
		if err == nil && kind == "deploy" {
			_, err = tx.ExecContext(r.Context(), `UPDATE apps SET status='cancelled',last_error='' WHERE id=$1`, app)
		}
	} else {
		if !retryAllowed(kind, status) {
			fail(w, 409, "This job cannot be retried directly. Inspect uncertain deployments; controlled operations require a new approval.")
			return
		}
		// Lock the application too, preventing concurrent retries of separate
		// failed attempts for the same application from queuing duplicate work.
		var target string
		if err = tx.QueryRowContext(r.Context(), "SELECT id FROM apps WHERE id=$1 FOR UPDATE", app).Scan(&target); err != nil {
			fail(w, 500, "Could not lock application")
			return
		}
		var active bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM jobs WHERE app_id=$1 AND status IN ('queued','running'))`, app).Scan(&active); err != nil {
			fail(w, 500, "Could not inspect queue")
			return
		}
		if active {
			fail(w, 409, "This application already has queued or running work")
			return
		}
		var existing string
		err = tx.QueryRowContext(r.Context(), "SELECT id FROM jobs WHERE retry_of=$1", parts[0]).Scan(&existing)
		if err == nil {
			fail(w, 409, "This job already has a retry; inspect the newer attempt")
			return
		}
		if err != sql.ErrNoRows {
			fail(w, 500, "Could not inspect retries")
			return
		}
		newID := id()
		_, err = tx.ExecContext(r.Context(), `INSERT INTO jobs(id,server_id,app_id,kind,payload,retry_of) VALUES($1,$2,$3,$4,$5,$6)`, newID, server, app, kind, payload, parts[0])
		if err == nil && kind == "deploy" {
			_, err = tx.ExecContext(r.Context(), "UPDATE apps SET status='queued',last_error='' WHERE id=$1", app)
		}
		result["job_id"] = newID
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "Could not commit job action")
		return
	}
	a.audit(r.Context(), u, project, "job."+parts[1], "job", parts[0], kind)
	jsonOut(w, 200, result)
}
