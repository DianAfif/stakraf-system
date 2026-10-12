package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAgentEnrollResponseToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agent/enroll" || r.Method != "POST" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"server_id":"server-1","agent_token":"sha_test_token"}`))
	}))
	defer srv.Close()
	c := client{base: srv.URL, http: &http.Client{Timeout: time.Second}}
	var state State
	if err := c.call(context.Background(), "POST", "/api/agent/enroll", map[string]string{"token": "she_demo"}, &state); err != nil {
		t.Fatal(err)
	}
	if state.Token != "sha_test_token" || !strings.HasPrefix(state.Token, "sha_") {
		t.Fatalf("unexpected agent state %#v", state)
	}
}
