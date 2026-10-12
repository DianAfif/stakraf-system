package main

import (
	"strings"
	"testing"
)

func TestReleaseValidation(t *testing.T) {
	valid := releaseInput{Image: "ghcr.io/acme/app@sha256:" + strings.Repeat("a", 64), RequestID: "request-1", Commit: strings.Repeat("b", 40)}
	if !validRelease(valid) {
		t.Fatal("digest rejected")
	}
	for _, image := range []string{"nginx:latest", "x@sha256:abc", "https://example.com/app", "--privileged"} {
		v := valid
		v.Image = image
		if validRelease(v) {
			t.Fatal("unsafe release accepted")
		}
	}
	valid.RequestID = "short"
	if validRelease(valid) {
		t.Fatal("short id accepted")
	}
}
func TestReleaseAgentCompatibility(t *testing.T) {
	for _, v := range []string{"0.5.0", "0.6.1", "1.0.0"} {
		if !releaseAgentSupported(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"0.4.0", "smoke", "", "0.5", "unknown"} {
		if releaseAgentSupported(v) {
			t.Fatal(v)
		}
	}
}
