package remotehost

import (
	"testing"
	"time"

	"mojian/internal/remote"
	"mojian/internal/session"
)

func TestSetConnClosesReplacedConnection(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MOJIAN_DIR", dir)
	t.Setenv("MOJIAN_RUNTIME_DIR", dir)
	done := make(chan error, 1)
	go func() { done <- session.Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for !session.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("the session server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Cleanup(func() {
		if err := session.Stop(); err != nil {
			t.Error(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("the session server did not stop")
		}
	})
	oldClient, err := session.Connect()
	if err != nil {
		t.Fatal(err)
	}
	newClient, err := session.Connect()
	if err != nil {
		_ = oldClient.Close()
		t.Fatal(err)
	}
	s := New()
	old := &remote.Conn{Host: "box", Client: oldClient}
	current := &remote.Conn{Host: "box", Client: newClient}
	s.SetConn(old)
	if s.SetConn(current) {
		t.Fatal("a different connection was reported as the same one")
	}
	select {
	case <-oldClient.Closed():
	case <-time.After(3 * time.Second):
		t.Fatal("the replaced connection was not closed")
	}
	select {
	case <-newClient.Closed():
		t.Fatal("the replacement connection was closed")
	default:
	}
	if !s.SetConn(current) {
		t.Fatal("reinstalling the same connection changed its identity")
	}
	s.CloseAll()
}
