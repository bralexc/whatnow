package main

import (
	"context"
	"net"
	"net/http"
	"slices"
	"testing"
	"time"
)

func TestServeHTTPStopsActiveRequestsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // A long-lived request like the event stream.
	})}
	defer srv.Close()
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, srv, ln) }()
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + ln.Addr().String())
		if resp != nil {
			resp.Body.Close()
		}
		requestDone <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the server")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the active request")
	}
	if err := <-requestDone; err != nil {
		t.Fatalf("active request did not finish cleanly: %v", err)
	}
	// The port must be reusable after shutdown, including on Windows.
	reopened, err := net.Listen("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("listener still held after shutdown: %v", err)
	}
	reopened.Close()
}

func TestListenUnlessTakenListensOnAFreeAddress(t *testing.T) {
	ln, err := listenUnlessTaken("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if ln == nil {
		t.Fatal("got no listener on a free address")
	}
	ln.Close()
}

func TestListenUnlessTakenStandsAsideWhenTheAddressIsHeld(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	ln, err := listenUnlessTaken(held.Addr().String())
	if err != nil {
		t.Fatalf("got %v, want no error", err)
	}
	if ln != nil {
		ln.Close()
		t.Fatal("got a listener on a held address")
	}
}

func TestListenUnlessTakenReportsOtherFailures(t *testing.T) {
	if _, err := listenUnlessTaken("not-an-address"); err == nil {
		t.Fatal("got no error for a bad address")
	}
}

func TestCommand(t *testing.T) {
	cases := []struct {
		args     []string
		wantCmd  string
		wantArgs []string
	}{
		{nil, "tui", nil},
		{[]string{"-db", "x.db"}, "tui", []string{"-db", "x.db"}},
		{[]string{"serve", "-addr", "127.0.0.1:1"}, "serve", []string{"-addr", "127.0.0.1:1"}},
		{[]string{"tui"}, "tui", []string{}},
		{[]string{"nope"}, "nope", []string{}},
	}
	for _, c := range cases {
		cmd, args := command(c.args)
		if cmd != c.wantCmd || !slices.Equal(args, c.wantArgs) {
			t.Errorf("command(%q) = %q, %q; want %q, %q", c.args, cmd, args, c.wantCmd, c.wantArgs)
		}
	}
}
