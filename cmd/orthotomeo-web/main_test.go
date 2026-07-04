package main

import (
	"strings"
	"testing"
)

// TestListenLoopbackBindsOnlyToLoopback is T27's explicit security
// acceptance criterion ("assert the bind address") - not a code-inspection
// check, a real net.Listen call whose resulting Addr() must be loopback.
func TestListenLoopbackBindsOnlyToLoopback(t *testing.T) {
	ln, err := listenLoopback("0") // port 0 = OS-assigned ephemeral port
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	addr := ln.Addr().String()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Errorf("listener bound to %q, want a 127.0.0.1 address", addr)
	}
}
