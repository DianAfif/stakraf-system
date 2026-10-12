package main

import "testing"

func TestOneOf(t *testing.T) {
	if !oneOf("approved", "approved", "rejected") || oneOf("attack", "approved", "rejected") {
		t.Fatal("decision value validation failed")
	}
}
