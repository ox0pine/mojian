//go:build darwin || linux

package session

import (
	"bufio"
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestConnectAtMissingSocketReturnsError(t *testing.T) {
	if _, err := ConnectAt(filepath.Join(t.TempDir(), "missing.sock")); err == nil {
		t.Fatal("ConnectAt returned a client for a missing socket")
	}
}

// A stream must ride the connection the client already has, not dial the
// server a second time.
func TestStreamUsesTheConnection(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()

	frames := make(chan frame, 8)
	go func() {
		r := bufio.NewReader(serverSide)
		f, err := readFrame(r)
		if err != nil || f.typ != frameHello {
			close(frames)
			return
		}
		b, _ := json.Marshal(Hello{Version: ProtocolVersion, Role: "server"})
		if err := writeFrame(serverSide, frame{typ: frameHello, payload: b}); err != nil {
			close(frames)
			return
		}
		for {
			f, err := readFrame(r)
			if err != nil {
				close(frames)
				return
			}
			frames <- f
		}
	}()

	c, err := NewClient(clientSide)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if hello, err := c.Hello(); err != nil || hello.Version != ProtocolVersion {
		t.Fatalf("client hello: %+v %v", hello, err)
	}
	s := c.Stream("sid", 20, 10)
	defer s.Close()

	select {
	case f, ok := <-frames:
		if !ok {
			t.Fatal("the server side of the connection closed")
		}
		if f.typ != frameOpen {
			t.Fatalf("frame type %d, want the stream open", f.typ)
		}
		var o openRequest
		if err := json.Unmarshal(f.payload, &o); err != nil {
			t.Fatal(err)
		}
		if o.SID != "sid" || o.Cols != 20 || o.Rows != 10 {
			t.Fatalf("open request %+v", o)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stream did not open on the client's connection")
	}
}
