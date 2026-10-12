package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var digestImagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:-]{1,180}@sha256:[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{7,64}$`)

func releaseAgentSupported(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	patch, e3 := strconv.Atoi(parts[2])
	return e1 == nil && e2 == nil && e3 == nil && patch >= 0 && (major > 0 || (major == 0 && minor >= 5))
}

type releaseInput struct {
	Image     string `json:"image"`
	Commit    string `json:"commit_sha"`
	Note      string `json:"note"`
	RequestID string `json:"request_id"`
}

func validRelease(x releaseInput) bool {
	return digestImagePattern.MatchString(x.Image) && (x.Commit == "" || commitPattern.MatchString(x.Commit)) && len(x.Note) <= 500 && len(x.RequestID) >= 8 && len(x.RequestID) <= 100
}
func (a *API) pipelineRelease(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "Use POST")
		return
	}
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(token, "sci_") {
		fail(w, 401, "Application pipeline key required")
		return
	}
	var aid, u string
	if a.db.QueryRowContext(r.Context(), `SELECT a.id,p.owner_id FROM apps a JOIN projects p ON p.id=a.project_id WHERE a.pipeline_token_hash=$1`, hash(token)).Scan(&aid, &u) != nil {
		fail(w, 401, "Pipeline key invalid or revoked")
		return
	}
	a.submitRelease(w, r, u, aid, hash(token))
}
func (a *API) releaseRoute(w http.ResponseWriter, r *http.Request, u string) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/")
	if len(parts) < 2 || !a.ownsApp(r.Context(), u, parts[0]) {
		fail(w, 404, "Application not found")
		return
	}
	aid := parts[0]
	if len(parts) == 2 && parts[1] == "pipeline-key" {
		switch r.Method {
		case "POST":
			key := "sci_" + secret(32)
			if _, err := a.db.ExecContext(r.Context(), "UPDATE apps SET pipeline_token_hash=$1 WHERE id=$2", hash(key), aid); err != nil {
				fail(w, 500, "Cannot issue pipeline key")
				return
			}
			a.audit(r.Context(), u, "", "pipeline.key_rotated", "application", aid, "")
			jsonOut(w, 201, M{"key": key, "app_id": aid, "scope": "submit_release_only"})
		case "DELETE":
			if _, err := a.db.ExecContext(r.Context(), "UPDATE apps SET pipeline_token_hash=NULL WHERE id=$1", aid); err != nil {
				fail(w, 500, "Cannot revoke pipeline key")
				return
			}
			a.audit(r.Context(), u, "", "pipeline.key_revoked", "application", aid, "")
			jsonOut(w, 200, M{"ok": true})
		case "GET":
			var enabled bool
			if a.db.QueryRowContext(r.Context(), "SELECT pipeline_token_hash IS NOT NULL FROM apps WHERE id=$1", aid).Scan(&enabled) != nil {
				fail(w, 500, "Cannot load pipeline")
				return
			}
			jsonOut(w, 200, M{"enabled": enabled, "app_id": aid})
		default:
			fail(w, 405, "Method not allowed")
		}
		return
	}
	if parts[1] != "releases" {
		fail(w, 404, "Not found")
		return
	}
	if len(parts) == 2 && r.Method == "GET" {
		a.releases(w, r, aid)
		return
	}
	if len(parts) == 2 && r.Method == "POST" {
		a.submitRelease(w, r, u, aid, "")
		return
	}
	if len(parts) == 4 && parts[3] == "decision" && r.Method == "POST" {
		a.releaseDecision(w, r, u, aid, parts[2])
		return
	}
	fail(w, 404, "Release route not found")
}
func (a *API) submitRelease(w http.ResponseWriter, r *http.Request, u, aid, pipelineHash string) {
	var x releaseInput
	if read(r, &x) != nil || !validRelease(x) {
		fail(w, 400, "Use an immutable image@sha256 digest, valid commit and unique request_id (8–100 characters)")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Release unavailable")
		return
	}
	defer tx.Rollback()
	var pid string
	// Serialize token revocation/rotation and submissions on the application row.
	if tx.QueryRowContext(r.Context(), `SELECT a.project_id FROM apps a JOIN projects p ON p.id=a.project_id WHERE a.id=$1 AND p.owner_id=$2 AND ($3='' OR a.pipeline_token_hash=$3) FOR UPDATE OF a`, aid, u, pipelineHash).Scan(&pid) != nil {
		fail(w, 401, "Application or pipeline credential no longer authorized")
		return
	}
	rid := id()
	_, err = tx.ExecContext(r.Context(), `INSERT INTO app_releases(id,app_id,image,commit_sha,note,request_id) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(app_id,request_id) DO NOTHING`, rid, aid, x.Image, x.Commit, x.Note, x.RequestID)
	if err != nil {
		fail(w, 500, "Could not save release")
		return
	}
	var image, commit, note, status string
	if tx.QueryRowContext(r.Context(), `SELECT id,image,commit_sha,note,status FROM app_releases WHERE app_id=$1 AND request_id=$2`, aid, x.RequestID).Scan(&rid, &image, &commit, &note, &status) != nil {
		fail(w, 500, "Could not read release")
		return
	}
	if image != x.Image || commit != x.Commit || note != x.Note {
		fail(w, 409, "request_id already used for a different release")
		return
	}
	if tx.Commit() != nil {
		fail(w, 500, "Could not commit release")
		return
	}
	a.audit(r.Context(), u, pid, "release.submitted", "release", rid, x.Image)
	jsonOut(w, 201, M{"id": rid, "app_id": aid, "status": status, "approval_required": status == "pending", "executed": false})
}
func (a *API) releases(w http.ResponseWriter, r *http.Request, aid string) {
	rows, err := a.db.QueryContext(r.Context(), `SELECT r.id,r.image,r.commit_sha,r.note,r.created_at,r.decided_at,coalesce(j.status,r.status),coalesce(r.job_id,''),coalesce(j.output,'') FROM app_releases r LEFT JOIN jobs j ON j.id=r.job_id WHERE r.app_id=$1 ORDER BY r.created_at DESC,r.id DESC LIMIT 100`, aid)
	if err != nil {
		fail(w, 500, "Release history unavailable")
		return
	}
	defer rows.Close()
	out := []M{}
	for rows.Next() {
		var i, image, commit, note, status, job, output string
		var created time.Time
		var decided sql.NullTime
		if rows.Scan(&i, &image, &commit, &note, &created, &decided, &status, &job, &output) != nil {
			fail(w, 500, "Invalid release record")
			return
		}
		out = append(out, M{"id": i, "image": image, "commit_sha": commit, "note": note, "status": status, "job_id": job, "output": output, "created_at": created, "decided_at": nullableTime(decided)})
	}
	if rows.Err() != nil {
		fail(w, 500, "Release history unavailable")
		return
	}
	jsonOut(w, 200, out)
}
func (a *API) releaseDecision(w http.ResponseWriter, r *http.Request, u, aid, rid string) {
	var x struct {
		Decision string `json:"decision"`
	}
	if read(r, &x) != nil || !oneOf(x.Decision, "approve", "reject") {
		fail(w, 400, "Invalid decision")
		return
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Cannot begin release")
		return
	}
	defer tx.Rollback()
	var sid, pid string
	var hp, cp int
	var ready bool
	var agentVersion string
	if tx.QueryRowContext(r.Context(), `SELECT a.server_id,a.project_id,a.host_port,a.container_port,(s.agent_token_hash IS NOT NULL AND s.docker_ready AND coalesce(s.last_seen>now()-interval '45 seconds',false)),s.agent_version FROM apps a JOIN servers s ON s.id=a.server_id JOIN projects p ON p.id=a.project_id WHERE a.id=$1 AND p.owner_id=$2 FOR UPDATE OF a`, aid, u).Scan(&sid, &pid, &hp, &cp, &ready, &agentVersion) != nil {
		fail(w, 404, "Application not found")
		return
	}
	var image string
	if tx.QueryRowContext(r.Context(), `SELECT image FROM app_releases WHERE id=$1 AND app_id=$2 AND status='pending' FOR UPDATE`, rid, aid).Scan(&image) != nil {
		fail(w, 404, "Pending release not found")
		return
	}
	status, job := "rejected", any(nil)
	if x.Decision == "approve" {
		if !releaseAgentSupported(agentVersion) {
			fail(w, 409, "Upgrade the connector to version 0.5 or newer before deploying releases")
			return
		}
		if !ready {
			fail(w, 409, "Connect an online Docker-ready server before approving")
			return
		}
		var busy bool
		if tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM jobs WHERE app_id=$1 AND status IN ('queued','running'))`, aid).Scan(&busy) != nil || busy {
			fail(w, 409, "Application already has active work")
			return
		}
		job = id()
		status = "queued"
		payload, _ := json.Marshal(M{"image": image, "host_port": hp, "container_port": cp, "replace": true})
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO jobs(id,server_id,app_id,kind,payload) VALUES($1,$2,$3,'deploy',$4)`, job, sid, aid, payload); err != nil {
			fail(w, 500, "Cannot queue release")
			return
		}
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE app_releases SET status=$1,job_id=$2,decided_at=now() WHERE id=$3`, status, job, rid); err != nil || tx.Commit() != nil {
		fail(w, 500, "Cannot decide release")
		return
	}
	a.audit(r.Context(), u, pid, "release."+x.Decision, "release", rid, image)
	jsonOut(w, 200, M{"id": rid, "status": status, "job_id": job})
}
