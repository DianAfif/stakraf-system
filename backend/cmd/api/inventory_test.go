package main

import "testing"

func TestValidateInventory(t *testing.T) {
	if !validContainerReports([]ContainerReport{{AppID: "0123456789abcdef01234567", Status: "running"}}) {
		t.Fatal("valid inventory rejected")
	}
	for _, items := range [][]ContainerReport{
		{{AppID: "foreign/path", Status: "running"}},
		{{AppID: "0123456789abcdef01234567", Status: "invalid"}},
		{{AppID: "0123456789abcdef01234567", Status: "running"}, {AppID: "0123456789abcdef01234567", Status: "stopped"}},
	} {
		if validContainerReports(items) {
			t.Fatal("invalid inventory accepted")
		}
	}
}
