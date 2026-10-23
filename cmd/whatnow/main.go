// Command whatnow runs the terminal companion, which serves the web UI too, or the server alone.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // Standalone Windows installations have no IANA timezone database.

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jameynakama/whatnow/internal/app"
	"github.com/jameynakama/whatnow/internal/httpapi"
	"github.com/jameynakama/whatnow/internal/settings"
	"github.com/jameynakama/whatnow/internal/tui"
	"github.com/jameynakama/whatnow/internal/web"
)

// Version is stamped in at release with -ldflags "-X main.Version=...".
var Version = "dev"

// newClient makes the model client. The e2e build swaps in the fake.
var newClient = settings.NewClient

const defaultAddr = "127.0.0.1:7777"

const usage = "usage: whatnow [-addr 127.0.0.1:7777] [-db PATH]        the terminal view, serving the web UI if nothing else is\n       whatnow serve [-addr 127.0.0.1:7777] [-db PATH]  the web UI only\n       whatnow tui [-addr 127.0.0.1:7777] [-db PATH]    same as bare whatnow\n       whatnow version"

func main() {
	cmd, args := command(os.Args[1:])
	flags := flag.NewFlagSet(cmd, flag.ExitOnError)
	flags.Usage = func() { fmt.Fprintln(os.Stderr, usage) }
	dbPath := flags.String("db", "", "database path (default <user config dir>/whatnow/whatnow.db)")
	addr := flags.String("addr", defaultAddr, "listen address; keep it on loopback")
	var err error
	switch cmd {
	case "serve":
		flags.Parse(args)
		err = serve(*addr, *dbPath)
	case "tui":
		flags.Parse(args)
		err = runTUI(*addr, *dbPath)
	case "version":
		fmt.Println(Version)
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		log.Fatal(err)
	}
}

// command splits off the subcommand. With none, or only flags, it's the TUI.
func command(args []string) (string, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "tui", args
	}
	return args[0], args[1:]
}

func open(dbPath string) (*app.App, error) {
	now := time.Now
	if path := os.Getenv("WHATNOW_DEMO_CLOCK"); path != "" {
		now = demoClock(path)
	}
	return app.Open(app.Options{DBPath: dbPath, NewClient: newClient, Now: now, Loc: time.Local})
}

func serve(addr, dbPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveContext(ctx, addr, dbPath)
}

func serveContext(ctx context.Context, addr, dbPath string) error {
	a, err := open(dbPath)
	if err != nil {
		return err
	}
	defer a.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	log.Printf("whatnow listening on http://%s (db %s)", ln.Addr(), a.DBPath)
	return serveHTTP(ctx, newServer(a, addr), ln)
}

// Cancel active requests (including SSE) before waiting for HTTP shutdown, then let the
// caller close the background workers and SQLite. Ctrl+C also reaches this path on Windows.
func serveHTTP(ctx context.Context, srv *http.Server, ln net.Listener) error {
	defer srv.Close()
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			srv.Close()
			<-done
			return err
		}
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func newServer(a *app.App, addr string) *http.Server {
	handler := httpapi.New(httpapi.Deps{
		Service:      a.Service,
		Hub:          a.Hub,
		DB:           a.DB,
		Loc:          a.Loc,
		Now:          a.Now,
		OnCapture:    a.Cleanup.Enqueue,
		Clean:        a.Cleanup.Clean,
		NewClient:    newClient,
		Planner:      a.Planner,
		Agent:        a.Agent,
		Integrations: a.Integrations,
		Static:       web.Handler(),
	})
	// No WriteTimeout: it would cut off the long-lived SSE stream.
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
}

// listenUnlessTaken listens on addr, or returns a nil listener when something already holds it.
func listenUnlessTaken(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if addressInUse(err) {
		return nil, nil
	}
	return ln, err
}

func runTUI(addr, dbPath string) error {
	a, err := open(dbPath)
	if err != nil {
		return err
	}
	defer a.Close()
	// Background work logs as it goes. Under the TUI, those lines would land on the screen.
	logs, err := tea.LogToFile(filepath.Join(filepath.Dir(a.DBPath), "tui.log"), "")
	if err != nil {
		return err
	}
	defer logs.Close()

	// With no server running, the TUI serves the web UI itself, so the address it shows works. Sharing
	// one process also shares the hub, so each side sees the other's changes live. When a server
	// already holds the address, the TUI points at it instead.
	ln, err := listenUnlessTaken(addr)
	if err != nil {
		return err
	}
	if ln != nil {
		srv := newServer(a, addr)
		go func() {
			if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
				log.Printf("web server stopped: %v", err)
			}
		}()
		defer srv.Close()
		log.Printf("whatnow listening on http://%s (db %s)", addr, a.DBPath)
	}
	return tui.Run(a, "http://"+addr)
}
