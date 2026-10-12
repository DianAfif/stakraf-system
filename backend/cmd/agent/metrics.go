package main

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func statCPU() (idle, total uint64, ok bool) {
	b, e := os.ReadFile("/proc/stat")
	if e != nil {
		return
	}
	parts := strings.Fields(strings.SplitN(string(b), "\n", 2)[0])
	if len(parts) < 5 || parts[0] != "cpu" {
		return
	}
	for i, f := range parts[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		if i == 3 || i == 4 {
			idle += v
		}
	}
	return idle, total, true
}
func memoryUsage() float64 {
	b, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return 0
	}
	var total, available uint64
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	if total == 0 {
		return 0
	}
	return clamp((float64(total-available) / float64(total)) * 100)
}
func diskUsage() float64 {
	var s syscall.Statfs_t
	if syscall.Statfs("/", &s) != nil || s.Blocks == 0 {
		return 0
	}
	return clamp((1 - float64(s.Bavail)/float64(s.Blocks)) * 100)
}
func clamp(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 100 {
		return 100
	}
	return x
}
func serverMetrics() (float64, float64, float64) {
	startIdle, startTotal, ok := statCPU()
	time.Sleep(120 * time.Millisecond)
	endIdle, endTotal, ok2 := statCPU()
	cpu := 0.0
	if ok && ok2 && endTotal > startTotal && endIdle >= startIdle {
		cpu = clamp(100 * (1 - float64(endIdle-startIdle)/float64(endTotal-startTotal)))
	}
	return cpu, memoryUsage(), diskUsage()
}
