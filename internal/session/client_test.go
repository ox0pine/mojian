//go:build darwin || linux

package session

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"testing"
	"time"
)

func newUnstartedTestClient(t *testing.T) *Client {
	t.Helper()
	a, b := net.Pipe()
	m := newMux(a, bufio.NewReader(a))
	c := &Client{
		mux: m, pending: map[int64]chan Response{},
		assemblies: map[int64]*responseAssembly{}, closed: make(chan struct{}),
	}
	t.Cleanup(func() {
		m.shutdown(io.EOF)
		_ = b.Close()
	})
	return c
}

func waitForPending(t *testing.T, c *Client, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.pending)
		c.mu.Unlock()
		if n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pending requests = %d, want %d", n, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func newClientWithHandler(t *testing.T, handler func(*mux, frame)) (*Client, *mux) {
	t.Helper()
	clientSide, serverSide := net.Pipe()
	serverMux := newMux(serverSide, bufio.NewReaderSize(serverSide, 1<<20))
	go func() {
		f, err := readFrame(serverMux.r)
		if err != nil || f.typ != frameHello {
			_ = serverSide.Close()
			return
		}
		b, _ := json.Marshal(Hello{Version: ProtocolVersion, Role: "server"})
		if err := writeFrame(serverSide, frame{typ: frameHello, payload: b}); err != nil {
			_ = serverSide.Close()
			return
		}
		serverMux.start(func(f frame) { handler(serverMux, f) })
	}()
	c, err := NewClient(clientSide)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close()
		serverMux.shutdown(io.EOF)
	})
	return c, serverMux
}

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

func TestNewClientReturnsProtocolErrorFrameVersion(t *testing.T) {
	clientSide, serverSide := net.Pipe()
	defer serverSide.Close()
	go func() {
		r := bufio.NewReader(serverSide)
		if _, err := readFrame(r); err != nil {
			return
		}
		serverVersion := ProtocolVersion + 3
		b, _ := json.Marshal(map[string]string{
			"code":    "protocol",
			"message": fmt.Sprintf("server protocol %d, client wants %d", serverVersion, ProtocolVersion-1),
		})
		_ = writeFrame(serverSide, frame{typ: frameError, payload: b})
	}()
	_, err := NewClient(clientSide)
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) {
		t.Fatalf("handshake error %T %v, want ProtocolError", err, err)
	}
	if protocolErr.Server != ProtocolVersion+3 || protocolErr.Client != ProtocolVersion {
		t.Fatalf("protocol error: %+v", protocolErr)
	}
}

func TestStopV4UsesFramedShutdown(t *testing.T) { testStopFramedVersion(t, 4) }
func TestStopV5UsesFramedShutdown(t *testing.T) { testStopFramedVersion(t, 5) }

func testStopFramedVersion(t *testing.T, version int) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("MOJIAN_DIR", dir)
	t.Setenv("MOJIAN_RUNTIME_DIR", dir)
	path := SocketPath()
	ln, err := listen(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan error, 1)
	go func() {
		defer removeSocket(path)
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		r := bufio.NewReader(conn)
		f, err := readFrame(r)
		if err != nil {
			done <- err
			return
		}
		var hello Hello
		if err := json.Unmarshal(f.payload, &hello); err != nil {
			done <- err
			return
		}
		if f.typ != frameHello || hello.Version != ProtocolVersion {
			done <- fmt.Errorf("initial hello: type %d, version %d", f.typ, hello.Version)
			return
		}
		message, _ := json.Marshal(map[string]string{
			"code":    "protocol",
			"message": fmt.Sprintf("server protocol %d, client wants %d", version, ProtocolVersion),
		})
		if err := writeFrame(conn, frame{typ: frameError, payload: message}); err != nil {
			done <- err
			return
		}
		_ = conn.Close()

		conn, err = ln.Accept()
		if err != nil {
			done <- err
			return
		}
		r = bufio.NewReader(conn)
		f, err = readFrame(r)
		if err != nil {
			done <- err
			return
		}
		hello = Hello{}
		if err := json.Unmarshal(f.payload, &hello); err != nil {
			done <- err
			return
		}
		if f.typ != frameHello || hello.Version != version {
			done <- fmt.Errorf("replacement hello: type %d, version %d", f.typ, hello.Version)
			return
		}
		answer, _ := json.Marshal(Hello{Version: version, Role: "server"})
		if err := writeFrame(conn, frame{typ: frameHello, payload: answer}); err != nil {
			done <- err
			return
		}
		f, err = readFrame(r)
		if err != nil {
			done <- err
			return
		}
		var req Request
		if err := json.Unmarshal(f.payload, &req); err != nil {
			done <- err
			return
		}
		if f.typ != frameRequest || req.Op != "shutdown" {
			done <- fmt.Errorf("protocol %d request: type %d, op %q", version, f.typ, req.Op)
			return
		}
		response, _ := json.Marshal(Response{ID: req.ID})
		if err := writeFrame(conn, frame{typ: frameResponse, payload: response}); err != nil {
			done <- err
			return
		}
		_ = conn.Close()
		done <- nil
	}()
	if err := Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("protocol %d server did not receive framed shutdown", version)
	}
}

func TestClientCallContextCancelsBlockedWriteAndDropsLateResponse(t *testing.T) {
	c := newUnstartedTestClient(t)
	for range ctrlQueue {
		c.mux.ctrl <- frame{typ: frameData}
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := c.ListContext(ctx)
		result <- err
	}()
	waitForPending(t, c, 1)
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation did not interrupt the queued control write")
	}
	waitForPending(t, c, 0)
	c.onFrame(frame{typ: frameResponse, payload: []byte(`{"id":1,"data":[]}`)})
	for len(c.mux.ctrl) > 0 {
		<-c.mux.ctrl
	}

	// Reusing the number after the late response must not resurrect or
	// remove the cancelled request's replacement.
	c.nextID = 0
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	go func() {
		_, err := c.ListContext(secondCtx)
		result <- err
	}()
	waitForPending(t, c, 1)
	cancelSecond()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second cancelled call returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("second call did not cancel")
	}
}

func TestClientDiscardCallKeepsReplacement(t *testing.T) {
	c := newUnstartedTestClient(t)
	first := make(chan Response, 1)
	second := make(chan Response, 1)
	assembly := &responseAssembly{owner: second}
	c.pending[7] = second
	c.assemblies[7] = assembly
	c.discardCall(7, first)
	if c.pending[7] != second || c.assemblies[7] != assembly {
		t.Fatal("old request cleanup deleted its replacement")
	}
}

func TestClientResponsePartReassemblyLimit(t *testing.T) {
	c := newUnstartedTestClient(t)
	owner := make(chan Response, 1)
	c.pending[9] = owner
	c.assemblies[9] = &responseAssembly{owner: owner, data: make([]byte, 1)}
	payload := make([]byte, responsePartHeaderSize+1)
	binary.BigEndian.PutUint64(payload[0:8], 9)
	binary.BigEndian.PutUint32(payload[8:12], 0)
	binary.BigEndian.PutUint32(payload[12:16], maxResponse+1)
	c.onFrame(frame{typ: frameResponsePart, payload: payload})
	select {
	case res := <-owner:
		if res.Error == "" {
			t.Fatal("oversized reassembly returned a successful response")
		}
	default:
		t.Fatal("oversized reassembly left its caller waiting")
	}
	if c.pending[9] != nil || c.assemblies[9] != nil {
		t.Fatal("oversized reassembly was not cleaned up")
	}
}

func TestClientLargeResponseRoundTripKeepsStream(t *testing.T) {
	data := bytes.Repeat([]byte("x"), maxFrame+1024)
	data[0], data[len(data)-1] = '"', '"'
	c, serverMux := newClientWithHandler(t, func(m *mux, f frame) {
		if f.typ != frameRequest {
			return
		}
		var req Request
		if json.Unmarshal(f.payload, &req) != nil {
			return
		}
		_ = writeResponse(m, Response{ID: req.ID, Data: data})
	})
	var got json.RawMessage
	if err := c.call(Request{Op: "large"}, &got); err != nil {
		t.Fatalf("large response: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("reassembled data length = %d, want %d", len(got), len(data))
	}
	clientStream := c.mux.openStream(77)
	serverStream := serverMux.openStream(77)
	if _, err := serverStream.Write([]byte("still-open")); err != nil {
		t.Fatal(err)
	}
	if text, err := readStream(t, clientStream); err != nil || text != "still-open" {
		t.Fatalf("stream after large response: %q %v", text, err)
	}
}
