package main

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

//go:embed installer.sh
var connectorFiles embed.FS

func (a *API) connectorAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "HEAD" {
		fail(w, 405, "Use GET")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/connector/")
	if name == "install.sh" {
		data, err := connectorFiles.ReadFile("installer.sh")
		if err != nil {
			fail(w, 503, "Installer unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == "GET" {
			_, _ = w.Write(data)
		}
		return
	}
	checksum := strings.HasSuffix(name, ".sha256")
	base := strings.TrimSuffix(name, ".sha256")
	if base != "stakraf-agent-linux-amd64" && base != "stakraf-agent-linux-arm64" {
		fail(w, 404, "Connector asset not found")
		return
	}
	path := filepath.Join(env("AGENT_ASSETS_DIR", "./agent-assets"), base)
	if checksum {
		data, err := os.ReadFile(path)
		if err != nil {
			fail(w, 503, "Agent download not built. Use the packaged control-plane image.")
			return
		}
		sum := sha256.Sum256(data)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == "GET" {
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + base + "\n"))
		}
		return
	}
	if _, err := os.Stat(path); err != nil {
		fail(w, 503, "Agent download not built. Use the packaged control-plane image.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, path)
}

func (a *API) disconnectServer(w http.ResponseWriter, r *http.Request, u string) {
	sid := strings.TrimSuffix(pathID(r.URL.Path, "/api/servers/"), "/disconnect")
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		fail(w, 500, "Could not disconnect server")
		return
	}
	defer tx.Rollback()
	var project string
	err = tx.QueryRowContext(r.Context(), `SELECT s.project_id FROM servers s JOIN projects p ON p.id=s.project_id WHERE s.id=$1 AND p.owner_id=$2 FOR UPDATE OF s`, sid, u).Scan(&project)
	if err != nil {
		fail(w, 404, "Server not found")
		return
	}
	_, err = tx.ExecContext(r.Context(), `UPDATE servers SET agent_token_hash=NULL,enrollment_hash=NULL,enrollment_expires=NULL,last_seen=NULL,docker_ready=false,docker_error='Disconnected by operator' WHERE id=$1`, sid)
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE jobs SET status=CASE WHEN status='queued' THEN 'cancelled' ELSE 'timed_out' END,finished_at=now(),output='Server disconnected. Inspect any operation that had already started.' WHERE server_id=$1 AND status IN ('queued','running')`, sid)
	}
	if err == nil {
		_, err = tx.ExecContext(r.Context(), `UPDATE apps SET status='unknown',last_error='Server disconnected; host state has not been confirmed.' WHERE server_id=$1`, sid)
	}
	if err != nil || tx.Commit() != nil {
		fail(w, 500, "Could not disconnect server")
		return
	}
	a.audit(r.Context(), u, project, "server.disconnected", "server", sid, "")
	jsonOut(w, 200, M{"ok": true})
}
