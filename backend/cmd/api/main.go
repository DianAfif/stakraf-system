package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

//go:embed schema.sql
var sqlFiles embed.FS

type API struct {
	db     *sql.DB
	origin string
	secure bool
}
type M = map[string]any

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func secret(n int) string {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func id() string           { return secret(12) }
func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func jsonOut(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, msg string) { jsonOut(w, code, M{"error": msg}) }
func read(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("invalid JSON")
	}
	return nil
}
func pathID(p, pre string) string { return strings.TrimPrefix(p, pre) }
func (a *API) user(r *http.Request) string {
	if u := a.apiKeyUser(r); u != "" {
		return u
	}
	c, e := r.Cookie("stakraf_session")
	if e != nil {
		return ""
	}
	var uid string
	err := a.db.QueryRowContext(r.Context(), "SELECT user_id FROM sessions WHERE token_hash=$1 AND expires_at>now()", hash(c.Value)).Scan(&uid)
	if err != nil {
		return ""
	}
	return uid
}
func (a *API) auth(w http.ResponseWriter, r *http.Request) (string, bool) {
	u := a.user(r)
	if u == "" {
		fail(w, 401, "Please sign in")
		return "", false
	}
	return u, true
}
func (a *API) agentID(r *http.Request) string {
	t := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !strings.HasPrefix(t, "sha_") {
		return ""
	}
	var sid string
	if a.db.QueryRowContext(r.Context(), "SELECT id FROM servers WHERE agent_token_hash=$1", hash(t)).Scan(&sid) != nil {
		return ""
	}
	return sid
}
func (a *API) agentAuth(w http.ResponseWriter, r *http.Request) (string, bool) {
	s := a.agentID(r)
	if s == "" {
		fail(w, 401, "Unauthorized agent")
		return "", false
	}
	return s, true
}
func (a *API) ownsProject(ctx context.Context, uid, pid string) bool {
	var v int
	return a.db.QueryRowContext(ctx, "SELECT 1 FROM projects WHERE id=$1 AND owner_id=$2", pid, uid).Scan(&v) == nil
}
func (a *API) ownsServer(ctx context.Context, uid, sid string) bool {
	var v int
	return a.db.QueryRowContext(ctx, "SELECT 1 FROM servers s JOIN projects p ON s.project_id=p.id WHERE s.id=$1 AND p.owner_id=$2", sid, uid).Scan(&v) == nil
}
func (a *API) ownsApp(ctx context.Context, uid, aid string) bool {
	var v int
	return a.db.QueryRowContext(ctx, "SELECT 1 FROM apps a JOIN projects p ON a.project_id=p.id WHERE a.id=$1 AND p.owner_id=$2", aid, uid).Scan(&v) == nil
}
func (a *API) cookie(w http.ResponseWriter, token string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "stakraf_session", Value: token, Path: "/", HttpOnly: true, Secure: a.secure, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func (a *API) handler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != "GET" && r.Method != "HEAD" {
		if o := r.Header.Get("Origin"); o != "" && a.origin != "" && o != a.origin {
			fail(w, 403, "Origin not allowed")
			return
		}
	}
	p := r.URL.Path
	if p == "/mcp" {
		a.mcp(w, r)
		return
	}
	if p == "/api/health" {
		if a.db.PingContext(r.Context()) != nil {
			fail(w, 503, "Database unavailable")
			return
		}
		jsonOut(w, 200, M{"status": "ok"})
		return
	}
	if p == "/api/auth/signup" && r.Method == "POST" {
		a.signup(w, r)
		return
	}
	if p == "/api/auth/login" && r.Method == "POST" {
		a.login(w, r)
		return
	}
	if p == "/api/agent/enroll" && r.Method == "POST" {
		a.enroll(w, r)
		return
	}
	if strings.HasPrefix(p, "/api/agent/") {
		a.agentHandler(w, r)
		return
	}
	uid, ok := a.auth(w, r)
	if !ok {
		return
	}
	switch {
	case p == "/api/auth/me" && r.Method == "GET":
		var email string
		_ = a.db.QueryRowContext(r.Context(), "SELECT email FROM users WHERE id=$1", uid).Scan(&email)
		jsonOut(w, 200, M{"id": uid, "email": email})
	case p == "/api/auth/logout" && r.Method == "POST":
		c, _ := r.Cookie("stakraf_session")
		if c != nil {
			_, _ = a.db.ExecContext(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", hash(c.Value))
		}
		a.cookie(w, "", -1)
		jsonOut(w, 200, M{"ok": true})
	case p == "/api/projects" && r.Method == "GET":
		a.projects(w, r, uid)
	case p == "/api/projects" && r.Method == "POST":
		a.createProject(w, r, uid)
	case p == "/api/servers" && r.Method == "GET":
		a.servers(w, r, uid)
	case p == "/api/servers" && r.Method == "POST":
		a.createServer(w, r, uid)
	case strings.HasPrefix(p, "/api/servers/") && strings.HasSuffix(p, "/enrollment") && r.Method == "POST":
		a.regenerate(w, r, uid)
	case p == "/api/apps" && r.Method == "GET":
		a.apps(w, r, uid)
	case p == "/api/apps" && r.Method == "POST":
		a.createApp(w, r, uid)
	case strings.HasPrefix(p, "/api/apps/") && strings.HasSuffix(p, "/actions") && r.Method == "POST":
		a.action(w, r, uid)
	case p == "/api/jobs" && r.Method == "GET":
		a.jobs(w, r, uid)
	default:
		if !a.operations(w, r, uid) {
			fail(w, 404, "Not found")
		}
	}
}
func (a *API) signup(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid input")
		return
	}
	x.Email = strings.ToLower(strings.TrimSpace(x.Email))
	if !strings.Contains(x.Email, "@") || len(x.Email) > 200 || len(x.Password) < 12 || len(x.Password) > 72 {
		fail(w, 400, "Use a valid email and a password of 12–72 characters")
		return
	}
	pw, e := bcrypt.GenerateFromPassword([]byte(x.Password), bcrypt.DefaultCost)
	if e != nil {
		fail(w, 500, "Authentication unavailable")
		return
	}
	uid := id()
	_, e = a.db.ExecContext(r.Context(), "INSERT INTO users(id,email,password_hash) VALUES($1,$2,$3)", uid, x.Email, string(pw))
	if e != nil {
		fail(w, 409, "Email already registered")
		return
	}
	a.newSession(w, r, uid)
}
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid input")
		return
	}
	var uid, pw string
	e := a.db.QueryRowContext(r.Context(), "SELECT id,password_hash FROM users WHERE email=$1", strings.ToLower(strings.TrimSpace(x.Email))).Scan(&uid, &pw)
	if e != nil || bcrypt.CompareHashAndPassword([]byte(pw), []byte(x.Password)) != nil {
		fail(w, 401, "Invalid credentials")
		return
	}
	a.newSession(w, r, uid)
}
func (a *API) newSession(w http.ResponseWriter, r *http.Request, uid string) {
	t := secret(32)
	_, e := a.db.ExecContext(r.Context(), "INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,now()+interval '7 days')", hash(t), uid)
	if e != nil {
		fail(w, 500, "Could not establish session")
		return
	}
	a.cookie(w, t, 7*24*3600)
	jsonOut(w, 200, M{"ok": true})
}
func (a *API) projects(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), "SELECT id,name,description,created_at FROM projects WHERE owner_id=$1 ORDER BY created_at DESC", u)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var i, n, d string
		var t time.Time
		if rows.Scan(&i, &n, &d, &t) == nil {
			data = append(data, M{"id": i, "name": n, "description": d, "created_at": t})
		}
	}
	jsonOut(w, 200, data)
}
func (a *API) createProject(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid request")
		return
	}
	x.Name = strings.TrimSpace(x.Name)
	if len(x.Name) < 2 || len(x.Name) > 80 || len(x.Description) > 500 {
		fail(w, 400, "Enter a project name (2–80 characters)")
		return
	}
	i := id()
	_, e := a.db.ExecContext(r.Context(), "INSERT INTO projects(id,owner_id,name,description) VALUES($1,$2,$3,$4)", i, u, x.Name, x.Description)
	if e != nil {
		fail(w, 500, "Could not create project")
		return
	}
	a.audit(r.Context(), u, i, "project.created", "project", i, x.Name)
	jsonOut(w, 201, M{"id": i, "name": x.Name})
}
func (a *API) servers(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT s.id,s.project_id,s.name,s.hostname,s.os,s.netbird_ip,s.netbird_status,s.last_seen,s.agent_token_hash IS NOT NULL,p.name,s.cpu_percent,s.memory_percent,s.disk_percent,s.agent_version FROM servers s JOIN projects p ON s.project_id=p.id WHERE p.owner_id=$1 ORDER BY s.created_at DESC`, u)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var i, p, n, h, osName, ip, net, project string
		var seen sql.NullTime
		var connected bool
		var cpu, memory, disk float64
		var agentVersion string
		if rows.Scan(&i, &p, &n, &h, &osName, &ip, &net, &seen, &connected, &project, &cpu, &memory, &disk, &agentVersion) == nil {
			online := seen.Valid && time.Since(seen.Time) < 45*time.Second
			data = append(data, M{"id": i, "project_id": p, "project_name": project, "name": n, "hostname": h, "os": osName, "netbird_ip": ip, "netbird_status": net, "online": online, "enrolled": connected, "last_seen": seen.Time, "cpu": cpu, "memory": memory, "disk": disk, "agent_version": agentVersion})
		}
	}
	jsonOut(w, 200, data)
}
func (a *API) createServer(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		ProjectID string `json:"project_id"`
		Name      string `json:"name"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid request")
		return
	}
	x.Name = strings.TrimSpace(x.Name)
	if !a.ownsProject(r.Context(), u, x.ProjectID) || len(x.Name) < 2 || len(x.Name) > 80 {
		fail(w, 400, "Invalid project or server name")
		return
	}
	sid := id()
	tok := "she_" + secret(24)
	_, e := a.db.ExecContext(r.Context(), "INSERT INTO servers(id,project_id,name,enrollment_hash,enrollment_expires) VALUES($1,$2,$3,$4,now()+interval '15 minutes')", sid, x.ProjectID, x.Name, hash(tok))
	if e != nil {
		fail(w, 500, "Could not create server")
		return
	}
	a.audit(r.Context(), u, x.ProjectID, "server.created", "server", sid, x.Name)
	jsonOut(w, 201, M{"id": sid, "name": x.Name, "enrollment_token": tok, "expires_in_minutes": 15})
}
func (a *API) regenerate(w http.ResponseWriter, r *http.Request, u string) {
	sid := strings.TrimSuffix(pathID(r.URL.Path, "/api/servers/"), "/enrollment")
	if !a.ownsServer(r.Context(), u, sid) {
		fail(w, 404, "Server not found")
		return
	}
	t := "she_" + secret(24)
	var exists bool
	e := a.db.QueryRowContext(r.Context(), "UPDATE servers SET enrollment_hash=$1,enrollment_expires=now()+interval '15 minutes' WHERE id=$2 AND agent_token_hash IS NULL RETURNING true", hash(t), sid).Scan(&exists)
	if e != nil {
		fail(w, 409, "Server is already connected")
		return
	}
	jsonOut(w, 200, M{"enrollment_token": t, "expires_in_minutes": 15})
}
func (a *API) enroll(w http.ResponseWriter, r *http.Request) {
	var x struct {
		Token    string `json:"token"`
		Hostname string `json:"hostname"`
		OS       string `json:"os"`
	}
	if read(r, &x) != nil || !strings.HasPrefix(x.Token, "she_") {
		fail(w, 400, "Invalid enrollment")
		return
	}
	if len(x.Hostname) > 255 || len(x.OS) > 100 {
		fail(w, 400, "Invalid metadata")
		return
	}
	agent := "sha_" + secret(32)
	var sid string
	e := a.db.QueryRowContext(r.Context(), `UPDATE servers SET agent_token_hash=$1,enrollment_hash=NULL,enrollment_expires=NULL,hostname=$2,os=$3,last_seen=now() WHERE enrollment_hash=$4 AND enrollment_expires>now() AND agent_token_hash IS NULL RETURNING id`, hash(agent), x.Hostname, x.OS, hash(x.Token)).Scan(&sid)
	if e != nil {
		fail(w, 401, "Token invalid, expired or already used")
		return
	}
	jsonOut(w, 200, M{"server_id": sid, "agent_token": agent})
}
func (a *API) agentHandler(w http.ResponseWriter, r *http.Request) {
	sid, ok := a.agentAuth(w, r)
	if !ok {
		return
	}
	switch {
	case r.URL.Path == "/api/agent/heartbeat" && r.Method == "POST":
		var x struct {
			Hostname      string  `json:"hostname"`
			OS            string  `json:"os"`
			NetBirdIP     string  `json:"netbird_ip"`
			NetBirdStatus string  `json:"netbird_status"`
			CPU           float64 `json:"cpu"`
			Memory        float64 `json:"memory"`
			Disk          float64 `json:"disk"`
			AgentVersion  string  `json:"agent_version"`
		}
		if read(r, &x) != nil {
			fail(w, 400, "Invalid heartbeat")
			return
		}
		if len(x.Hostname) > 255 || len(x.OS) > 100 || len(x.NetBirdIP) > 80 || len(x.NetBirdStatus) > 30 || len(x.AgentVersion) > 60 || x.CPU < 0 || x.CPU > 100 || x.Memory < 0 || x.Memory > 100 || x.Disk < 0 || x.Disk > 100 {
			fail(w, 400, "Invalid heartbeat metadata")
			return
		}
		_, _ = a.db.ExecContext(r.Context(), "UPDATE servers SET hostname=$1,os=$2,netbird_ip=$3,netbird_status=$4,last_seen=now(),cpu_percent=$6,memory_percent=$7,disk_percent=$8,agent_version=$9 WHERE id=$5", x.Hostname, x.OS, x.NetBirdIP, x.NetBirdStatus, sid, x.CPU, x.Memory, x.Disk, x.AgentVersion)
		_, _ = a.db.ExecContext(r.Context(), `INSERT INTO telemetry(server_id,cpu,memory,disk) SELECT $1,$2,$3,$4 WHERE NOT EXISTS (SELECT 1 FROM telemetry WHERE server_id=$1 AND recorded_at>now()-interval '1 minute')`, sid, x.CPU, x.Memory, x.Disk)
		jsonOut(w, 200, M{"ok": true})
	case r.URL.Path == "/api/agent/jobs/next" && r.Method == "GET":
		var i, app, kind string
		var payload json.RawMessage
		e := a.db.QueryRowContext(r.Context(), `UPDATE jobs SET status='running',started_at=now() WHERE id=(SELECT id FROM jobs WHERE server_id=$1 AND status='queued' ORDER BY created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING id,app_id,kind,payload`, sid).Scan(&i, &app, &kind, &payload)
		if e == sql.ErrNoRows {
			jsonOut(w, 200, M{"job": nil})
			return
		}
		if e != nil {
			fail(w, 500, "Job queue unavailable")
			return
		}
		jsonOut(w, 200, M{"job": M{"id": i, "app_id": app, "kind": kind, "payload": json.RawMessage(payload)}})
	case strings.HasPrefix(r.URL.Path, "/api/agent/jobs/") && strings.HasSuffix(r.URL.Path, "/complete") && r.Method == "POST":
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
		var app, kind string
		e := a.db.QueryRowContext(r.Context(), `UPDATE jobs SET status=$1,output=$2,finished_at=now() WHERE id=$3 AND server_id=$4 AND status='running' RETURNING app_id,kind`, status, x.Output, i, sid).Scan(&app, &kind)
		if e != nil {
			fail(w, 409, "Unknown or already completed job")
			return
		}
		if kind == "deploy" || kind == "restart" || kind == "start" || kind == "stop" {
			s := "running"
			if kind == "stop" {
				s = "stopped"
			}
			errText := ""
			if !x.Success {
				s = "failed"
				errText = x.Output
			}
			_, _ = a.db.ExecContext(r.Context(), "UPDATE apps SET status=$1,last_error=$2 WHERE id=$3", s, errText, app)
		}
		jsonOut(w, 200, M{"ok": true})
	default:
		fail(w, 404, "Agent route not found")
	}
}

var imagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/@:-]{1,220}$`)

func (a *API) apps(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT a.id,a.project_id,a.server_id,a.name,a.image,a.host_port,a.container_port,a.status,a.last_error,s.name,p.name FROM apps a JOIN projects p ON a.project_id=p.id JOIN servers s ON s.id=a.server_id WHERE p.owner_id=$1 ORDER BY a.created_at DESC`, u)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var i, p, s, n, im, status, errText, sn, pn string
		var hp, cp int
		if rows.Scan(&i, &p, &s, &n, &im, &hp, &cp, &status, &errText, &sn, &pn) == nil {
			data = append(data, M{"id": i, "project_id": p, "server_id": s, "project_name": pn, "server_name": sn, "name": n, "image": im, "host_port": hp, "container_port": cp, "status": status, "last_error": errText})
		}
	}
	jsonOut(w, 200, data)
}
func (a *API) createApp(w http.ResponseWriter, r *http.Request, u string) {
	var x struct {
		ProjectID     string `json:"project_id"`
		ServerID      string `json:"server_id"`
		Name          string `json:"name"`
		Image         string `json:"image"`
		HostPort      int    `json:"host_port"`
		ContainerPort int    `json:"container_port"`
	}
	if read(r, &x) != nil {
		fail(w, 400, "Invalid request")
		return
	}
	x.Name = strings.TrimSpace(x.Name)
	x.Image = strings.TrimSpace(x.Image)
	if len(x.Name) < 2 || len(x.Name) > 60 || !imagePattern.MatchString(x.Image) || x.HostPort < 1024 || x.HostPort > 65535 || x.ContainerPort < 1 || x.ContainerPort > 65535 {
		fail(w, 400, "Invalid app name, image or port")
		return
	}
	var match int
	if a.db.QueryRowContext(r.Context(), "SELECT 1 FROM servers s JOIN projects p ON s.project_id=p.id WHERE s.id=$1 AND p.id=$2 AND p.owner_id=$3 AND s.agent_token_hash IS NOT NULL", x.ServerID, x.ProjectID, u).Scan(&match) != nil {
		fail(w, 400, "Select a connected server in this project")
		return
	}
	appID, jobID := id(), id()
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Could not start deployment")
		return
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(r.Context(), "INSERT INTO apps(id,project_id,server_id,name,image,host_port,container_port,status) VALUES($1,$2,$3,$4,$5,$6,$7,'queued')", appID, x.ProjectID, x.ServerID, x.Name, x.Image, x.HostPort, x.ContainerPort)
	if e != nil {
		fail(w, 409, "App name or host port already used on this server")
		return
	}
	payload, _ := json.Marshal(M{"image": x.Image, "host_port": x.HostPort, "container_port": x.ContainerPort})
	_, e = tx.ExecContext(r.Context(), "INSERT INTO jobs(id,server_id,app_id,kind,payload) VALUES($1,$2,$3,'deploy',$4)", jobID, x.ServerID, appID, payload)
	if e != nil {
		fail(w, 500, "Could not queue deployment")
		return
	}
	if tx.Commit() != nil {
		fail(w, 500, "Could not commit deployment")
		return
	}
	a.audit(r.Context(), u, x.ProjectID, "app.deploy_queued", "application", appID, x.Image)
	jsonOut(w, 201, M{"id": appID, "job_id": jobID, "status": "queued"})
}
func (a *API) action(w http.ResponseWriter, r *http.Request, u string) {
	appID := strings.TrimSuffix(pathID(r.URL.Path, "/api/apps/"), "/actions")
	if !a.ownsApp(r.Context(), u, appID) {
		fail(w, 404, "App not found")
		return
	}
	var x struct {
		Kind string `json:"kind"`
	}
	if read(r, &x) != nil || (x.Kind != "logs") {
		fail(w, 400, "Use the change approval workflow for operations; logs are allowed")
		return
	}
	var server string
	_ = a.db.QueryRowContext(r.Context(), "SELECT server_id FROM apps WHERE id=$1", appID).Scan(&server)
	i := id()
	_, e := a.db.ExecContext(r.Context(), "INSERT INTO jobs(id,server_id,app_id,kind) VALUES($1,$2,$3,$4)", i, server, appID, x.Kind)
	if e != nil {
		fail(w, 500, "Could not queue action")
		return
	}
	jsonOut(w, 201, M{"job_id": i, "status": "queued"})
}
func (a *API) jobs(w http.ResponseWriter, r *http.Request, u string) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT j.id,j.app_id,j.kind,j.status,j.output,j.created_at,a.name,s.name FROM jobs j JOIN apps a ON j.app_id=a.id JOIN servers s ON s.id=j.server_id JOIN projects p ON a.project_id=p.id WHERE p.owner_id=$1 ORDER BY j.created_at DESC LIMIT 60`, u)
	if e != nil {
		fail(w, 500, "Database error")
		return
	}
	defer rows.Close()
	data := []M{}
	for rows.Next() {
		var i, app, kind, status, output, n, s string
		var t time.Time
		if rows.Scan(&i, &app, &kind, &status, &output, &t, &n, &s) == nil {
			data = append(data, M{"id": i, "app_id": app, "kind": kind, "status": status, "output": output, "created_at": t, "app_name": n, "server_name": s})
		}
	}
	jsonOut(w, 200, data)
}
func main() {
	dsn := env("DATABASE_URL", "postgres://stakraf:stakraf_dev@localhost:5432/stakraf?sslmode=disable")
	db, e := sql.Open("pgx", dsn)
	if e != nil {
		log.Fatal(e)
	}
	db.SetMaxOpenConns(12)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		e = db.PingContext(ctx)
		if e == nil {
			break
		}
		if ctx.Err() != nil {
			log.Fatal("postgres unavailable: ", e)
		}
		time.Sleep(time.Second)
	}
	schema, e := sqlFiles.ReadFile("schema.sql")
	if e != nil {
		log.Fatalf("read schema: %v", e)
	}
	// Send the complete script so PostgreSQL parses comments and quoted SQL.
	// With no parameters, pgx uses the simple protocol and supports the script.
	if _, e = db.Exec(string(schema)); e != nil {
		log.Fatalf("migration error: %v", e)
	}
	origin := os.Getenv("APP_ORIGIN")
	secure := strings.HasPrefix(origin, "https://")
	a := &API{db: db, origin: origin, secure: secure}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/", a.handler)
	mux.HandleFunc("/mcp", a.handler)
	srv := &http.Server{Addr: ":" + env("PORT", "8080"), Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	port, err := strconv.Atoi(env("PORT", "8080"))
	if err != nil || port < 1 {
		log.Fatal("invalid PORT")
	}
	log.Printf("Stakraf API listening on :%d", port)
	log.Fatal(srv.ListenAndServe())
}
