package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseRestoresPreviousContainerOnRunFailure(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$@" >> "$RELEASE_TEST_CALLS"
case "$1" in
 container) exit 1;;
 inspect) echo true;;
 run) echo launch-failed; exit 1;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("RELEASE_TEST_CALLS", calls)
	t.Setenv("STAKRAF_BIND_IP", "127.0.0.1")
	j := Job{ID: "job", AppID: "app", Kind: "deploy"}
	j.Payload.Image = "nginx@sha256:" + strings.Repeat("a", 64)
	j.Payload.HostPort = 8088
	j.Payload.ContainerPort = 8080
	j.Payload.Replace = true
	ok, out := runRelease(context.Background(), j)
	if ok || !strings.Contains(out, "previous container restored") {
		t.Fatalf("unexpected result %v %s", ok, out)
	}
	raw, _ := os.ReadFile(calls)
	if !strings.Contains(string(raw), "rename stakraf-app-previous-job stakraf-app") || !strings.Contains(string(raw), "start stakraf-app") {
		t.Fatal("old container not restored")
	}
}
func TestReleaseRejectsRestartLoop(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$1" in
 container) exit 1;;
 inspect) if [ "$3" = "{{.State.Running}}" ]; then echo true; else echo "restarting 3"; fi;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("STAKRAF_BIND_IP", "127.0.0.1")
	j := Job{ID: "loop", AppID: "app", Kind: "deploy"}
	j.Payload.Image = "nginx@sha256:" + strings.Repeat("a", 64)
	j.Payload.HostPort = 8088
	j.Payload.ContainerPort = 8080
	ok, out := runRelease(context.Background(), j)
	if ok || !strings.Contains(out, "previous container restored") {
		t.Fatalf("restart loop accepted: %v %s", ok, out)
	}
}
