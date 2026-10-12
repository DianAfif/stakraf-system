package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPToolsPermissions(t *testing.T) {
	if len(mcpTools(false)) != 10 || len(mcpTools(true)) != 11 {
		t.Fatal("unexpected catalog")
	}
	for _, tool := range mcpTools(false) {
		if tool["name"] == "propose_change" || tool["annotations"].(M)["readOnlyHint"] != true {
			t.Fatal("read key has write capability")
		}
		if tool["inputSchema"].(M)["additionalProperties"] != false {
			t.Fatal("schema must reject extra arguments")
		}
		if mcpQuery(tool["name"].(string)) == "" {
			t.Fatal("missing query")
		}
	}
	encoded, _ := json.Marshal(mcpTools(true))
	if strings.Contains(string(encoded), "approve_change") {
		t.Fatal("MCP cannot approve changes")
	}
}
func TestMCPTransportGuards(t *testing.T) {
	a := &API{origin: "https://console.stakraf.com"}
	for _, tc := range []struct {
		method, origin string
		status         int
	}{
		{"GET", "", 405}, {"POST", "https://attacker.invalid", 403}, {"POST", "", 401},
	} {
		r := httptest.NewRequest(tc.method, "/mcp", strings.NewReader("{}"))
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		a.mcp(w, r)
		if w.Code != tc.status {
			t.Fatalf("got %d want %d", w.Code, tc.status)
		}
	}
}

func TestMCPProposalSchemaValidation(t *testing.T) {
	tool := mcpTools(true)[10]
	props := tool["inputSchema"].(M)["properties"].(M)
	for _, tc := range []struct {
		field, value string
		valid        bool
	}{
		{"kind", "restart", true}, {"kind", "shell", false}, {"kind", "", false},
		{"reason", "Observed an unhealthy application", true}, {"reason", "short", false},
		{"request_id", "unique-request-1", true}, {"request_id", "short", false},
		{"app_id", strings.Repeat("a", 101), false},
	} {
		if mcpArgumentValid(props[tc.field].(M), tc.value) != tc.valid {
			t.Fatalf("unexpected validation for %s", tc.field)
		}
	}
}
