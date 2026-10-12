package main

import "testing"

func TestEnvFallback(t *testing.T) {
	if getenv("STAKRAF_UNUSED_FOR_TEST", "fallback") != "fallback" {
		t.Fatal("fallback")
	}
}
