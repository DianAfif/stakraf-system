package main

import (
	"context"
	"encoding/json"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

type containerReport struct {
	AppID  string `json:"app_id"`
	Status string `json:"status"`
}

var managedName = regexp.MustCompile(`^stakraf-([a-f0-9]{24})$`)

func parseInventory(data string) ([]containerReport, bool) {
	reports := []containerReport{}
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if line == "" {
			continue
		}
		var item struct {
			Names string
			State string
		}
		if json.Unmarshal([]byte(line), &item) != nil {
			return reports, false
		}
		match := managedName.FindStringSubmatch(item.Names)
		if match == nil {
			continue
		}
		state := item.State
		if state == "exited" {
			state = "stopped"
		}
		if !oneOfState(state) {
			return reports, false
		}
		reports = append(reports, containerReport{AppID: match[1], Status: state})
		if len(reports) > 200 {
			return reports[:200], false
		}
	}
	return reports, true
}
func oneOfState(s string) bool {
	for _, state := range []string{"running", "stopped", "restarting", "created", "dead", "removing"} {
		if s == state {
			return true
		}
	}
	return false
}
func dockerInventory() (bool, string, string, []containerReport, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return false, "", "Docker daemon unavailable or connector service account lacks access.", nil, false
	}
	data, err := exec.CommandContext(ctx, "docker", "ps", "-a", "--filter", "name=stakraf-", "--format", "{{json .}}").Output()
	if err != nil {
		return false, strings.TrimSpace(string(version)), "Could not read managed Docker inventory.", nil, false
	}
	reports, complete := parseInventory(string(data))
	return true, strings.TrimSpace(string(version)), "", reports, complete
}
