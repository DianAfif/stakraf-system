package main

import (
	"testing"
	"time"
)

func TestAuthLimitWindow(t *testing.T) {
	var limit authLimiter
	now := time.Now()
	for i := 0; i < 20; i++ {
		if !limit.allow("127.0.0.1:1000", now) {
			t.Fatal("early limit")
		}
	}
	if limit.allow("127.0.0.1:2000", now) {
		t.Fatal("port change bypassed limit")
	}
	if !limit.allow("127.0.0.1:2000", now.Add(time.Minute)) {
		t.Fatal("window did not reset")
	}
}
