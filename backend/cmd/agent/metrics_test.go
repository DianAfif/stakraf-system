package main

import "testing"

func TestMetricClamp(t *testing.T) {
	if clamp(-5) != 0 || clamp(105) != 100 || clamp(41.7) != 41.7 {
		t.Fatal("metric clamp invalid")
	}
}
