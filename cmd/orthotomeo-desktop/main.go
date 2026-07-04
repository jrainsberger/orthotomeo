// Command orthotomeo-desktop is T28's offline, no-browser-juggling native
// launcher, matching the house Footsteps desktop pattern: start the T27 web
// server, open the default browser to it, show a minimal status window, and
// stop the server cleanly on close.
//
// Built on Gio (gioui.org), not Fyne: Fyne's default desktop driver pulls in
// go-gl/glfw, which requires cgo and a real C toolchain - contrary to this
// ticket's original assumption that the whole build (engine, HTTP, desktop)
// would stay C-toolchain-free like the pure-Go modernc.org/sqlite driver
// already does. Gio talks to the OS windowing/graphics APIs directly via
// syscalls rather than cgo bindings, so `CGO_ENABLED=0 go build` succeeds
// for this binary too - the property T28 actually wanted.
//
// Renders NO scripture here - it is a lifecycle shell only (status label +
// two buttons); the browser owns all Greek/Hebrew rendering, same as T27
// already established. That's what keeps this "thin wrapper" thin and
// sidesteps any GUI toolkit's complex-script/RTL weaknesses entirely,
// rather than working around them.
//
// Build on Windows with the GUI linker flag, or a plain `go build` attaches
// a console window alongside the status window (every Go binary defaults to
// the "console" PE subsystem - this is a Windows/cmd-linker property, not
// anything specific to Gio or this program):
//
//	go build -ldflags -H=windowsgui -o orthotomeo-desktop.exe ./cmd/orthotomeo-desktop
//
// This flag is Windows-only; a plain `go build` is correct on macOS/Linux,
// where a GUI process never gets an attached terminal in the first place.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func main() {
	dbPath := flag.String("db", "data/orthotomeo.db", "path to the built orthotomeo DB (cmd/build's output)")
	flag.Parse()

	srv, err := startServer(*dbPath)
	if err != nil {
		log.Fatalf("start server: %v", err)
	}

	if err := openBrowser(srv.url()); err != nil {
		log.Printf("open browser: %v (visit %s manually)", err, srv.url())
	}

	go func() {
		w := new(app.Window)
		w.Option(app.Title("orthotomeo"), app.Size(unit.Dp(380), unit.Dp(180)))
		if err := run(w, srv); err != nil {
			log.Printf("window: %v", err)
		}
		// On close: cancel the server context, wait for a clean shutdown
		// (port released, no orphan process) - T28's own acceptance
		// criterion - before the process actually exits.
		if err := srv.shutdown(context.Background()); err != nil {
			log.Printf("shutdown: %v", err)
		}
		os.Exit(0)
	}()
	app.Main()
}

// run is the window's event loop - Gio's own idiomatic shape (a for-loop
// over w.Event(), switching on FrameEvent to redraw and DestroyEvent to
// exit) rather than Fyne's callback-based API. Returns when the window is
// closed (by the Quit button, via ActionClose, or the OS window controls).
func run(w *app.Window, srv *runningServer) error {
	th := material.NewTheme()
	var ops op.Ops
	quitBtn := new(widget.Clickable)
	openBtn := new(widget.Clickable)
	status := "orthotomeo running\n" + srv.url()

	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			if quitBtn.Clicked(gtx) {
				w.Perform(system.ActionClose)
			}
			if openBtn.Clicked(gtx) {
				if err := openBrowser(srv.url()); err != nil {
					status += "\n(couldn't open browser: " + err.Error() + ")"
				}
			}

			layout.UniformInset(unit.Dp(14)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceEvenly}.Layout(gtx,
					layout.Rigid(material.Label(th, unit.Sp(14), status).Layout),
					layout.Rigid(material.Button(th, openBtn, "Open in browser").Layout),
					layout.Rigid(material.Button(th, quitBtn, "Quit").Layout),
				)
			})

			e.Frame(gtx.Ops)
		}
	}
}
