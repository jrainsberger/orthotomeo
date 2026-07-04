// Command orthotomeo-web serves the browser-facing seam (T27): read-only
// GET JSON endpoints plus the embedded static reading-view UI, over
// httpapi.Server. Like orthotomeo-mcp, it never imports database/sql or
// orthotomeo/store directly - engine is the only DB-touching import.
//
// Security: the listen address is hardcoded to the 127.0.0.1 loopback
// interface - there is deliberately no --host flag, so there is no flag
// combination that could ever bind this to 0.0.0.0 or a LAN-reachable
// interface. A loopback read-only server needs no authentication; if this
// binary is ever repurposed to listen beyond loopback, that decision needs
// its own auth design, not a config toggle on this one.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jrainsberger/orthotomeo/engine"
	"github.com/jrainsberger/orthotomeo/httpapi"
)

const loopbackHost = "127.0.0.1"

// listenLoopback is the one place a TCP address is ever constructed for
// this server - factored out from main so a test can assert the actual
// resulting listener is bound to loopback, not just that the source code
// contains the string "127.0.0.1" somewhere.
func listenLoopback(port string) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(loopbackHost, port))
}

func main() {
	dbPath := flag.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB (cmd/build's output)")
	port := flag.String("port", "8420", "loopback port to listen on")
	flag.Parse()

	e, err := engine.Open(*dbPath)
	if err != nil {
		log.Fatalf("open engine: %v", err)
	}
	defer e.Close()

	ln, err := listenLoopback(*port)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	srv := &http.Server{Handler: httpapi.New(e).Handler()}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()

	log.Printf("orthotomeo-web listening on http://%s (loopback only)", ln.Addr())
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
