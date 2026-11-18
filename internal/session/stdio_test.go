//go:build darwin || linux

package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The bridge is what an SSH client runs on the host: a pipe in, a pipe
// out, and the server's socket behind them.
func TestBridgeCarriesTheHandshake(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MOJIAN_DIR", dir)
	t.Setenv("MOJIAN_RUNTIME_DIR", dir)
	done := make(chan error, 1)
	go func() { done <- Serve() }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(SocketPath()); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the server did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	var in bytes.Buffer
	hello, _ := json.Marshal(Hello{Version: ProtocolVersion, Role: "client"})
	if err := writeFrame(&in, frame{typ: frameHello, payload: hello}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := bridge(&in, &out); err != nil {
		t.Fatalf("bridge: %v", err)
	}
	f, err := readFrame(bufio.NewReader(&out))
	if err != nil {
		t.Fatalf("reading the server's answer: %v", err)
	}
	if f.typ != frameHello {
		t.Fatalf("first frame is type %d, want the hello", f.typ)
	}
	var answer Hello
	if err := json.Unmarshal(f.payload, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Version != ProtocolVersion || answer.Role != "server" {
		t.Fatalf("server hello %+v", answer)
	}

	if c, err := Connect(); err == nil {
		_ = c.Shutdown()
		_ = c.Close()
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the server did not stop")
	}
}
