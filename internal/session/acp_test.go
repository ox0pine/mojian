//go:build darwin || linux || windows

package session

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mojian/internal/acp"
)

func newPipeServerClient(t *testing.T) (*Client, func()) {
	t.Helper()
	srvConn, cliConn := net.Pipe()
	srv := &Server{
		sessions: map[string]*session{},
		started:  time.Now(),
		quit:     make(chan struct{}),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.serveConn(srvConn)
	}()
	c, err := NewClient(cliConn)
	if err != nil {
		_ = srvConn.Close()
		t.Fatal(err)
	}
	cleanup := func() {
		_ = c.Close()
		_ = srvConn.Close()
		<-done
	}
	return c, cleanup
}

func TestServerSearchAndACPAgentsOverPipe(t *testing.T) {
	c, cleanup := newPipeServerClient(t)
	defer cleanup()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("line one\nfind me here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := c.SearchContext(context.Background(), dir, SearchOptions{Query: "find me"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 1 || len(res.Matches) != 1 || res.Matches[0].Line != 2 {
		t.Fatalf("unexpected search result: %+v", res)
	}

	// ACPAgents returns without error (empty or populated depending on PATH).
	if _, err := c.ACPAgentsContext(context.Background()); err != nil {
		t.Fatalf("ACPAgentsContext failed: %v", err)
	}

	// StartACP with an unknown agent cleanly streams a KindFailed event.
	events := make(chan acp.Event, 4)
	sess, err := c.StartACP(context.Background(), dir, "nonexistent-agent-xyz", "hello", func(ev acp.Event) {
		events <- ev
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	select {
	case ev := <-events:
		if ev.Kind != acp.KindFailed || ev.Err == nil {
			t.Fatalf("expected KindFailed for unknown agent, got %+v", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for remote ACP failure event")
	}
}
