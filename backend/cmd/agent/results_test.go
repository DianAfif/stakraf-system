package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompletionRetry(t *testing.T) {
	for _, tc := range []struct {
		name          string
		status, calls int
		wantError     bool
	}{
		{"transient", 503, 3, false}, {"conflicting", 409, 1, true}, {"unauthorized", 401, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/agent/jobs/test/complete" || r.Method != "POST" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				n := calls.Add(1)
				if tc.status == 503 && n >= 3 {
					w.WriteHeader(200)
					return
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			c := client{base: srv.URL, http: srv.Client()}
			err := c.submitResult(context.Background(), Job{ID: "test"}, true, "complete", time.Millisecond)
			if (err != nil) != tc.wantError {
				t.Fatalf("unexpected result: %v", err)
			}
			if got := int(calls.Load()); got != tc.calls {
				t.Fatalf("got %d requests, want %d", got, tc.calls)
			}
		})
	}
}

func TestCompletionRetryRespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { cancel(); w.WriteHeader(503) }))
	defer srv.Close()
	c := client{base: srv.URL, http: srv.Client()}
	start := time.Now()
	if err := c.submitResult(ctx, Job{ID: "test"}, false, "failed", time.Minute); err == nil {
		t.Fatal("expected cancellation")
	}
	if time.Since(start) > time.Second {
		t.Fatal("retry ignored cancellation")
	}
}
