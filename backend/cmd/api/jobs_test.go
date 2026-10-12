package main

import "testing"

func TestRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		kind, status string
		allowed      bool
	}{
		{"deploy", "failed", true}, {"deploy", "cancelled", true}, {"deploy", "timed_out", false},
		{"deploy", "running", false}, {"deploy", "success", false}, {"logs", "timed_out", true},
		{"logs", "failed", true}, {"restart", "failed", false}, {"stop", "failed", false}, {"start", "cancelled", false},
	} {
		if got := retryAllowed(tc.kind, tc.status); got != tc.allowed {
			t.Errorf("%s/%s: got %t, want %t", tc.kind, tc.status, got, tc.allowed)
		}
	}
}
