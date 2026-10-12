package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunJobRejectsUnsafeImage(t *testing.T) {
	j := Job{ID: "job1", AppID: "app1", Kind: "deploy"}
	j.Payload.Image = "nginx:latest; rm -rf /"
	j.Payload.HostPort = 8081
	j.Payload.ContainerPort = 80
	ok, out := runJob(j)
	if ok || !strings.Contains(out, "Invalid deployment") {
		t.Fatalf("expected rejection, got ok=%v output=%s", ok, out)
	}
}
func TestRunJobWithMockedDocker(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "docker")
	script := "#!/bin/sh\ncase \"$1\" in\n  container) exit 1;;\n  pull) echo pulled;;\n  run) echo fake-container-id;;\n  inspect) echo 'running 0';;\n  restart) echo restarted;;\n  logs) echo 'test output';;\nesac\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("STAKRAF_BIND_IP", "127.0.0.1")
	j := Job{ID: "job1", AppID: "test-app", Kind: "deploy"}
	j.Payload.Image = "nginx:alpine"
	j.Payload.HostPort = 8081
	j.Payload.ContainerPort = 80
	ok, out := runJob(j)
	if !ok || !strings.Contains(out, "Container running") {
		t.Fatalf("mocked deployment failed: %t %s", ok, out)
	}
	j.Kind = "restart"
	ok, out = runJob(j)
	if !ok || !strings.Contains(out, "restarted") {
		t.Fatalf("restart failed: %t %s", ok, out)
	}
	j.Kind = "logs"
	ok, out = runJob(j)
	if !ok || !strings.Contains(out, "test output") {
		t.Fatalf("logs failed: %t %s", ok, out)
	}
}
