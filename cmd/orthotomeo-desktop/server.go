// This file holds the non-GUI lifecycle logic (start/stop the T27 HTTP
// server, open the default browser) - factored out of main.go so it's
// testable without a display/windowing system, which `go test` in CI
// doesn't have. Fyne itself renders no scripture and does none of this
// (T28's own scope) - this file is the "starts the server, opens the
// browser" half; main.go is purely the status window.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/httpapi"
)

const loopbackHost = "127.0.0.1"

// runningServer is a started T27 HTTP server plus everything needed to
// shut it down cleanly and know where it's listening.
type runningServer struct {
	engine *engine.Engine
	http   *http.Server
	ln     net.Listener
}

// startServer opens the engine read-only and starts httpapi's handler on
// an ephemeral loopback port (":0" - the OS picks a free one), matching
// cmd/orthotomeo-web's same "no --host flag, loopback only" discipline:
// there is no parameter here that could ever bind beyond 127.0.0.1.
func startServer(dbPath string) (*runningServer, error) {
	e, err := engine.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open engine: %w", err)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort(loopbackHost, "0"))
	if err != nil {
		e.Close()
		return nil, fmt.Errorf("listen: %w", err)
	}

	srv := &http.Server{Handler: httpapi.New(e).Handler()}
	go srv.Serve(ln) //nolint:errcheck // Serve's only non-nil return after Shutdown is ErrServerClosed, expected

	return &runningServer{engine: e, http: srv, ln: ln}, nil
}

// port returns the OS-assigned loopback port the server is actually
// listening on.
func (r *runningServer) port() string {
	return fmt.Sprintf("%d", r.ln.Addr().(*net.TCPAddr).Port)
}

// url is the *.localhost address T28's scope specifies (RFC 6761: any
// ".localhost" host resolves to loopback) rather than a bare
// "127.0.0.1:port" - matches the house *.localhost test-domain convention
// and Footsteps' own "footsteps.localhost" precedent.
func (r *runningServer) url() string {
	return "http://orthotomeo.localhost:" + r.port() + "/"
}

// shutdown stops the HTTP server cleanly (in-flight requests get up to 5s
// to finish, then the listener is released) and closes the engine's DB
// handle - "port released, no orphan process" is T28's own acceptance
// criterion, verified directly by TestShutdownReleasesThePort.
func (r *runningServer) shutdown(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := r.http.Shutdown(shutdownCtx)
	r.engine.Close()
	return err
}

// openBrowser launches the OS default browser at url - the one place this
// binary shells out, and only ever with a URL this process itself just
// constructed (never user input), so there's no injection surface.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
