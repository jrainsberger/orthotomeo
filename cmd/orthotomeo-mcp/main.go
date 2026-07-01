// Command orthotomeo-mcp exposes the engine facade (T25) over MCP,
// stdio transport, read-only (T20). It never generates SQL and never sees
// a *sql.DB - the engine package is the only import that touches the
// database at all (grep for "database/sql" or "orthotomeo/store" in this
// directory: none). The MCP server IS the deterministic engine; the LLM
// client on the other end of the transport is the analysis layer.
package main

import (
	"context"
	"flag"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jrainsberger/orthotomeo/engine"
)

func main() {
	dbPath := flag.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB (cmd/build's output)")
	flag.Parse()

	e, err := engine.Open(*dbPath)
	if err != nil {
		log.Fatalf("open engine: %v", err)
	}
	defer e.Close()

	server := mcp.NewServer(&mcp.Implementation{Name: "orthotomeo", Version: "0.1.0"}, nil)
	registerTools(server, e)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
