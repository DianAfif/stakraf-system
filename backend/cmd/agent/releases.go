package main

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var releaseDigest = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/:-]{1,180}@sha256:[a-f0-9]{64}$`)

// Image releases retain the previous container until the replacement is running.
// This is a single-host stop/start upgrade with downtime, not blue/green.
func runRelease(ctx context.Context, j Job) (bool, string) {
	if !releaseDigest.MatchString(j.Payload.Image) || j.Payload.HostPort < 1024 || j.Payload.HostPort > 65535 || j.Payload.ContainerPort < 1 || j.Payload.ContainerPort > 65535 {
		return false, "Invalid immutable release specification"
	}
	bind := getenv("STAKRAF_BIND_IP", "127.0.0.1")
	if bind != "127.0.0.1" && bind != "::1" && getenv("STAKRAF_ALLOW_NONLOCAL_BIND", "") != "true" {
		return false, "Non-loopback binding requires explicit configuration"
	}
	name, backup := "stakraf-"+j.AppID, "stakraf-"+j.AppID+"-previous-"+j.ID
	if _, e := docker(ctx, "container", "inspect", backup); e == nil {
		return false, "Previous attempt requires manual inspection; backup container already exists"
	}
	if out, e := docker(ctx, "pull", j.Payload.Image); e != nil {
		return false, "Image pull failed; existing application untouched: " + out
	}
	state, err := docker(ctx, "inspect", "--format", "{{.State.Running}}", name)
	existing := err == nil
	wasRunning := strings.TrimSpace(state) == "true"
	if existing {
		if out, e := docker(ctx, "rename", name, backup); e != nil {
			return false, "Could not retain previous container: " + out
		}
		if out, e := docker(ctx, "stop", backup); e != nil {
			_, _ = docker(ctx, "rename", backup, name)
			return false, "Could not stop previous container: " + out
		}
	}
	rollback := func(detail string) (bool, string) {
		recovery, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = docker(recovery, "rm", "-f", name)
		if !existing {
			return false, detail + "; no previous container was available"
		}
		if out, e := docker(recovery, "rename", backup, name); e != nil {
			return false, detail + "; RESTORATION FAILED, inspect backup " + backup + ": " + out
		}
		if wasRunning {
			if out, e := docker(recovery, "start", name); e != nil {
				return false, detail + "; previous container restored but could not start: " + out
			}
		}
		return false, detail + "; previous container restored"
	}
	port := bind + ":" + strconv.Itoa(j.Payload.HostPort) + ":" + strconv.Itoa(j.Payload.ContainerPort)
	if out, e := docker(ctx, "run", "-d", "--name", name, "--restart", "unless-stopped", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "128", "--memory", "512m", "--cpus", "1", "-p", port, j.Payload.Image); e != nil {
		return rollback("Replacement launch failed: " + out)
	}
	select {
	case <-ctx.Done():
		return rollback("Release execution timed out")
	case <-time.After(2 * time.Second):
	}
	state, err = docker(ctx, "inspect", "--format", "{{.State.Status}} {{.RestartCount}}", name)
	if err != nil || strings.TrimSpace(state) != "running 0" {
		logs, _ := docker(ctx, "logs", "--tail", "50", name)
		return rollback("Replacement exited after launch: " + logs)
	}
	if existing {
		if _, e := docker(ctx, "rm", backup); e != nil {
			return true, "Release running; previous stopped container could not be removed. Inspect " + backup
		}
	}
	return true, "Release running at " + bind + ":" + strconv.Itoa(j.Payload.HostPort) + "; container startup checked, application health not verified"
}
