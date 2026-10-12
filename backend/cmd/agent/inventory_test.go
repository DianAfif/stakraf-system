package main

import "testing"

func TestManagedInventory(t *testing.T) {
	items, complete := parseInventory("{\"Names\":\"unrelated\",\"State\":\"running\"}\n{\"Names\":\"stakraf-0123456789abcdef01234567\",\"State\":\"exited\"}")
	if !complete || len(items) != 1 || items[0].Status != "stopped" {
		t.Fatalf("unexpected inventory %#v, complete=%t", items, complete)
	}
	if _, ok := parseInventory("not JSON"); ok {
		t.Fatal("invalid inventory marked complete")
	}
	if items, ok := parseInventory(""); !ok || len(items) != 0 {
		t.Fatal("empty successful inventory should be complete")
	}
}
