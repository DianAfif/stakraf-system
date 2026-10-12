package main

import (
	"strings"
	"testing"
)

func TestSecretAndHash(t *testing.T) {
	a := secret(24)
	b := secret(24)
	if a == b || len(a) != 48 || hash(a) == a || hash(a) == hash(b) {
		t.Fatal("token uniqueness/hash failed")
	}
}
func TestImageValidation(t *testing.T) {
	for _, v := range []string{"nginx:alpine", "ghcr.io/example/app:v1", "registry.example.com:5000/app@sha256:abc"} {
		if !imagePattern.MatchString(v) {
			t.Errorf("unexpected reject: %s", v)
		}
	}
	for _, v := range []string{"bad image", "hello;rm -rf /", "-foo", strings.Repeat("a", 222)} {
		if imagePattern.MatchString(v) {
			t.Errorf("unexpected accept: %s", v)
		}
	}
}
