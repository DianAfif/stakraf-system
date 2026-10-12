package main

import (
	"net"
	"sync"
	"time"
)

type authWindow struct {
	start time.Time
	count int
}
type authLimiter struct {
	mu      sync.Mutex
	windows map[string]authWindow
}

func (l *authLimiter) allow(address string, now time.Time) bool {
	ip, _, err := net.SplitHostPort(address)
	if err != nil {
		ip = address
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.windows == nil {
		l.windows = map[string]authWindow{}
	}
	if len(l.windows) >= 4096 {
		for key, window := range l.windows {
			if now.Sub(window.start) >= time.Minute {
				delete(l.windows, key)
			}
		}
	}
	window, exists := l.windows[ip]
	if !exists && len(l.windows) >= 4096 {
		return false
	}
	if now.Sub(window.start) >= time.Minute {
		window = authWindow{start: now}
	}
	if window.count >= 20 {
		return false
	}
	window.count++
	l.windows[ip] = window
	return true
}
