// Stakraf Agent runs on an operator-authorized Linux host. It requires Docker.
// It polls for a fixed set of operations; it never executes AI-generated shell commands.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type State struct {
	ServerID string `json:"server_id"`
	Token    string `json:"agent_token"`
}
type Job struct {
	ID      string `json:"id"`
	AppID   string `json:"app_id"`
	Kind    string `json:"kind"`
	Payload struct {
		Image         string `json:"image"`
		HostPort      int    `json:"host_port"`
		ContainerPort int    `json:"container_port"`
		Replace       bool   `json:"replace"`
	} `json:"payload"`
}
type client struct {
	base  string
	token string
	http  *http.Client
}

type apiError struct {
	status int
	detail string
}

func (e *apiError) Error() string { return fmt.Sprintf("API %d: %s", e.status, e.detail) }

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func (c *client) call(ctx context.Context, method, path string, input any, output any) error {
	var body io.Reader
	if input != nil {
		b, e := json.Marshal(input)
		if e != nil {
			return e
		}
		body = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if e != nil {
		return e
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer res.Body.Close()
	b, e := io.ReadAll(io.LimitReader(res.Body, 16384))
	if e != nil {
		return e
	}
	if res.StatusCode >= 300 {
		return &apiError{status: res.StatusCode, detail: string(b)}
	}
	if output != nil {
		return json.Unmarshal(b, output)
	}
	return nil
}
func docker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, e := cmd.CombinedOutput()
	s := string(out)
	if len(s) > 6000 {
		s = s[len(s)-6000:]
	}
	if e != nil {
		return s, fmt.Errorf("docker %s: %w: %s", args[0], e, s)
	}
	return s, nil
}
func runJob(j Job) (bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	name := "stakraf-" + j.AppID
	switch j.Kind {
	case "deploy":
		if j.Payload.Replace {
			return runRelease(ctx, j)
		}
		if j.Payload.HostPort < 1024 || j.Payload.HostPort > 65535 || j.Payload.ContainerPort < 1 || j.Payload.ContainerPort > 65535 || strings.ContainsAny(j.Payload.Image, " \r\n\t\x00") || len(j.Payload.Image) > 230 {
			return false, "Invalid deployment specification"
		}
		if _, e := docker(ctx, "container", "inspect", name); e == nil {
			return false, "Container already exists; Stakraf does not replace deployed apps automatically"
		}
		if out, e := docker(ctx, "pull", j.Payload.Image); e != nil {
			return false, out + "\n" + e.Error()
		}
		// By default, bind only to loopback to avoid exposing an app to the public internet.
		bind := getenv("STAKRAF_BIND_IP", "127.0.0.1")
		if bind != "127.0.0.1" && bind != "::1" && getenv("STAKRAF_ALLOW_NONLOCAL_BIND", "") != "true" {
			return false, "Non-loopback binding requires STAKRAF_ALLOW_NONLOCAL_BIND=true"
		}
		port := bind + ":" + strconv.Itoa(j.Payload.HostPort) + ":" + strconv.Itoa(j.Payload.ContainerPort)
		out, e := docker(ctx, "run", "-d", "--name", name, "--restart", "unless-stopped", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--memory", "512m", "--cpus", "1", "-p", port, j.Payload.Image)
		if e != nil {
			return false, out + "\n" + e.Error()
		}
		time.Sleep(2 * time.Second)
		state, e := docker(ctx, "inspect", "--format", "{{.State.Status}} {{.RestartCount}}", name)
		if e != nil || strings.TrimSpace(state) != "running 0" {
			logs, _ := docker(ctx, "logs", "--tail", "50", name)
			return false, "Container exited after launch: " + logs
		}
		return true, "Container running at " + bind + ":" + strconv.Itoa(j.Payload.HostPort) + " (application health not yet verified)"
	case "stop":
		out, e := docker(ctx, "stop", name)
		return e == nil, out
	case "start":
		out, e := docker(ctx, "start", name)
		return e == nil, out
	case "restart":
		out, e := docker(ctx, "restart", name)
		return e == nil, out
	case "logs":
		out, e := docker(ctx, "logs", "--tail", "80", name)
		return e == nil, out
	default:
		return false, "Unsupported job kind"
	}
}
func netbird() (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netbird", "status")
	b, e := cmd.CombinedOutput()
	if e != nil {
		return getenv("STAKRAF_NETBIRD_IP", ""), "disconnected"
	}
	s := strings.ToLower(string(b))
	if strings.Contains(s, "disconnected") || strings.Contains(s, "not connected") {
		return "", "disconnected"
	}
	if strings.Contains(s, "connected") {
		return getenv("STAKRAF_NETBIRD_IP", ""), "connected"
	}
	return "", "unknown"
}

// Heartbeats run independently so an image pull cannot make a live host look
// offline. The client is safe for concurrent requests after enrollment.
func (c *client) heartbeatLoop(ctx context.Context, host string, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		ip, nb := netbird()
		cpu, mem, disk := serverMetrics()
		ready, version, detail, containers, complete := dockerInventory()
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := c.call(requestCtx, "POST", "/api/agent/heartbeat", map[string]any{"hostname": host, "os": runtime.GOOS + "/" + runtime.GOARCH, "netbird_ip": ip, "netbird_status": nb, "cpu": cpu, "memory": mem, "disk": disk, "agent_version": "0.5.0", "docker_ready": ready, "docker_version": version, "docker_error": detail, "containers": containers, "inventory_complete": complete}, nil)
		cancel()
		if err != nil {
			log.Printf("heartbeat: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (c *client) submitResult(ctx context.Context, j Job, ok bool, out string, delay time.Duration) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = c.call(ctx, "POST", "/api/agent/jobs/"+j.ID+"/complete", map[string]any{"success": ok, "output": out}, nil)
		if err == nil {
			return nil
		}
		var response *apiError
		if errors.As(err, &response) && response.status >= 400 && response.status < 500 && response.status != 429 {
			return err
		}
		if attempt == 4 {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
func main() {
	enrollOnly := flag.Bool("enroll-only", false, "Enroll and store the connector credential, then exit")
	checkOnly := flag.Bool("check", false, "Check console authentication and Docker readiness, then exit")
	versionOnly := flag.Bool("version", false, "Print the connector version")
	flag.Parse()
	if *versionOnly {
		fmt.Println("stakraf-agent 0.5.0")
		return
	}
	base := strings.TrimRight(getenv("STAKRAF_API_URL", ""), "/")
	if base == "" {
		log.Fatal("Set STAKRAF_API_URL to the reachable HTTPS API URL")
	}
	if !strings.HasPrefix(base, "https://") && !strings.HasPrefix(base, "http://localhost:") && !strings.HasPrefix(base, "http://127.0.0.1:") && getenv("STAKRAF_ALLOW_HTTP", "") != "true" {
		log.Fatal("HTTPS required; STAKRAF_ALLOW_HTTP=true is only for trusted lab networks")
	}
	stateFile := getenv("STAKRAF_STATE_FILE", "./stakraf-agent-state.json")
	c := &client{base: base, http: &http.Client{Timeout: 15 * time.Second}}
	host, _ := os.Hostname()
	var state State
	if b, e := os.ReadFile(stateFile); e == nil {
		if json.Unmarshal(b, &state) != nil || state.Token == "" {
			log.Fatal("Invalid existing state file")
		}
	} else {
		tok := os.Getenv("STAKRAF_ENROLLMENT_TOKEN")
		if tok == "" {
			log.Fatal("Set STAKRAF_ENROLLMENT_TOKEN during first enrollment")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		e = c.call(ctx, "POST", "/api/agent/enroll", map[string]string{"token": tok, "hostname": host, "os": runtime.GOOS + "/" + runtime.GOARCH}, &state)
		cancel()
		if e != nil {
			log.Fatal("Enrollment failed: ", e)
		}
		if e := os.MkdirAll(filepath.Dir(stateFile), 0700); e != nil {
			log.Fatal(e)
		}
		b, _ := json.MarshalIndent(state, "", "  ")
		if e := os.WriteFile(stateFile, b, 0600); e != nil {
			log.Fatal("Cannot persist credentials: ", e)
		}
		log.Printf("Enrolled server %s. Keep %s private", state.ServerID, stateFile)
	}
	c.token = state.Token
	if *enrollOnly {
		return
	}
	if *checkOnly {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := c.call(ctx, "GET", "/api/agent/status", nil, nil); err != nil {
			log.Fatal("Console authentication: ", err)
		}
		ready, version, detail, _, _ := dockerInventory()
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"console": base, "server_id": state.ServerID, "authenticated": true, "docker_ready": ready, "docker_version": version, "docker_error": detail})
		if !ready {
			os.Exit(1)
		}
		return
	}
	go c.heartbeatLoop(context.Background(), host, 10*time.Second)
	tick := time.NewTicker(6 * time.Second)
	defer tick.Stop()
	for {
		var resp struct {
			Job *Job `json:"job"`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		e := c.call(ctx, "GET", "/api/agent/jobs/next", nil, &resp)
		cancel()
		if e != nil {
			log.Printf("poll: %v", e)
		} else if resp.Job != nil {
			log.Printf("Running %s job %s", resp.Job.Kind, resp.Job.ID)
			ok, out := runJob(*resp.Job)
			ctx, cancel = context.WithTimeout(context.Background(), 55*time.Second)
			e = c.submitResult(ctx, *resp.Job, ok, out, 2*time.Second)
			cancel()
			if e != nil {
				log.Printf("Unable to submit job result: %v", e)
			} else {
				log.Printf("Job %s success=%t", resp.Job.ID, ok)
			}
		}
		<-tick.C
	}
}
