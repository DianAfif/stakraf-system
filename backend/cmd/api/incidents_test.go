package main

import (
	"strings"
	"testing"
)

func textPointer(s string) *string { return &s }
func TestIncidentUpdateValidation(t *testing.T) {
	for _, status := range []string{"open", "acknowledged", "investigating", "monitoring", "resolved"} {
		if !validIncidentUpdate(incidentUpdate{Version: 1, Status: textPointer(status)}) {
			t.Fatal(status)
		}
	}
	for _, x := range []incidentUpdate{
		{Version: 0, Status: textPointer("open")},
		{Version: 1, Status: textPointer("deleted")},
		{Version: 1, Severity: textPointer("urgent")},
		{Version: 1, Responder: textPointer(strings.Repeat("a", 81))},
		{Version: 1, Impact: textPointer(strings.Repeat("a", 2001))},
		{Version: 1},
	} {
		if validIncidentUpdate(x) {
			t.Fatal("invalid patch accepted")
		}
	}
}
