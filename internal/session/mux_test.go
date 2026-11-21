package session

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestFrameRoundTrip(t *testing.T) {
	for _, want := range []frame{
		{typ: frameData, stream: 7, payload: []byte("hello")},
		{typ: frameHello, stream: 0, payload: nil},
		{typ: frameClose, stream: 4294967295},
	} {
		var buf bytes.Buffer
		if err := writeFrame(&buf, want); err != nil {
			t.Fatal(err)
		}
		got, err := readFrame(bufio.NewReader(&buf))
		if err != nil {
			t.Fatal(err)
		}
		if got.typ != want.typ || got.stream != want.stream || !bytes.Equal(got.payload, want.payload) {
			t.Fatalf("round trip %+v, want %+v", got, want)
		}
	}
}

func TestMuxOversizeFrame(t *testing.T) {
	var buf bytes.Buffer
	var hdr [frameHeaderSize]byte
	binary.BigEndian.PutUint32(hdr[0:4], maxFrame+1)
	buf.Write(hdr[:])
	if _, err := readFrame(bufio.NewReader(&buf)); err == nil || !strings.Contains(err.Error(), "frame too long") {
		t.Fatalf("oversize frame returned %v", err)
	}
}

// newTestMuxes returns two multiplexers talking over one pipe, each
// routing the peer's data to the stream of that id.
func newTestMuxes(t *testing.T) (*mux, *mux) {
	t.Helper()
	a, b := net.Pipe()
	makeMux := func(conn net.Conn) *mux {
		m := newMux(conn, bufio.NewReaderSize(conn, 1<<16))
		m.start(func(f frame) {
			switch f.typ {
			case frameData:
				if s := m.stream(f.stream); s != nil {
					if !s.push(f.payload) {
						_ = s.closeWithError(errors.New("test: stream is too slow"))
					}
				}
			case frameClose:
				if s := m.stream(f.stream); s != nil {
					s.finish(io.EOF)
				}
			}
		})
		return m
	}
	ma, mb := makeMux(a), makeMux(b)
	t.Cleanup(func() {
		ma.shutdown(io.EOF)
		mb.shutdown(io.EOF)
	})
	return ma, mb
}

func readStream(t *testing.T, s *muxStream) (string, error) {
	t.Helper()
	type result struct {
		text string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 64)
		n, err := s.Read(buf)
		ch <- result{string(buf[:n]), err}
	}()
	select {
	case r := <-ch:
		return r.text, r.err
	case <-time.After(3 * time.Second):
		t.Fatal("a stream read blocked")
		return "", nil
	}
}

func TestMuxTwoStreamsAreIndependent(t *testing.T) {
	ma, mb := newTestMuxes(t)
	s1a, s2a := ma.openStream(1), ma.openStream(2)
	s1b, s2b := mb.openStream(1), mb.openStream(2)

	if _, err := s1a.Write([]byte("one")); err != nil {
		t.Fatal(err)
	}
	if _, err := s2a.Write([]byte("two")); err != nil {
		t.Fatal(err)
	}
	if got, err := readStream(t, s1b); err != nil || got != "one" {
		t.Fatalf("first stream read %q %v", got, err)
	}
	if got, err := readStream(t, s2b); err != nil || got != "two" {
		t.Fatalf("second stream read %q %v", got, err)
	}

	// Ending the first stream leaves the second one flowing.
	if err := s1a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readStream(t, s1b); !errors.Is(err, io.EOF) {
		t.Fatalf("ended stream read %v, want EOF", err)
	}
	if _, err := s2a.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if got, err := readStream(t, s2b); err != nil || got != "more" {
		t.Fatalf("second stream after the first ended: %q %v", got, err)
	}
}

func TestMuxCloseWithErrorPrecedesRotationRemoval(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	m := newMux(a, bufio.NewReader(a))
	defer m.shutdown(io.EOF)
	s := m.openStream(1)
	streamErr := errors.New("test: stream overflow")
	if err := s.closeWithError(streamErr); err != nil {
		t.Fatal(err)
	}
	f, ok := m.nextData()
	if !ok || f.typ != frameClose || f.stream != s.id {
		t.Fatalf("close marker was lost before finish: %+v %v", f, ok)
	}
	if !errors.Is(s.readErr(), streamErr) {
		t.Fatalf("local read error = %v, want %v", s.readErr(), streamErr)
	}
}

func TestMuxCloseAfterFinishNotifiesPeer(t *testing.T) {
	ma, mb := newTestMuxes(t)
	a := ma.openStream(1)
	b := mb.openStream(1)
	a.finish(errors.New("test: local overflow"))
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readStream(t, b); !errors.Is(err, io.EOF) {
		t.Fatalf("peer read after late close: %v", err)
	}
}

func TestMuxClosedRejectsWritesAndStreams(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	m := newMux(a, bufio.NewReader(a))
	m.shutdown(io.EOF)
	if err := m.writeControl(frameRequest, []byte("request")); err == nil {
		t.Fatal("a closed mux queued a control request")
	}
	s := m.openStream(1)
	select {
	case <-s.done:
	default:
		t.Fatal("a stream opened on a closed mux is still running")
	}
	if _, err := s.Write([]byte("input")); err == nil {
		t.Fatal("a stream on a closed mux accepted input")
	}
	if err := s.Close(); err == nil {
		t.Fatal("a stream on a closed mux reported a successful close")
	}
	if len(m.ctrl) != 0 {
		t.Fatal("a closed mux retained frames for a writer that exited")
	}
}

func TestMuxSlowStreamIsClosedNotTheConnection(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	m := newMux(a, bufio.NewReaderSize(a, 1<<16))
	// Nobody reads b, so the writer stalls on the pipe and the streams'
	// queues fill.
	m.start(func(frame) {})
	defer m.shutdown(io.EOF)

	slow, fast := m.openStream(1), m.openStream(2)
	var overflow error
	for i := 0; i < streamQueue+3; i++ {
		if _, err := slow.Write([]byte("x")); err != nil {
			overflow = err
			break
		}
	}
	if !errors.Is(overflow, io.ErrShortWrite) {
		t.Fatalf("the slow stream never overflowed: %v", overflow)
	}
	select {
	case <-slow.done:
	default:
		t.Fatal("the overflowing stream was not finished")
	}
	if m.stream(1) != nil {
		t.Fatal("the finished stream is still routed")
	}
	select {
	case <-m.quit:
		t.Fatal("the overflow ended the whole connection")
	default:
	}
	if _, err := fast.Write([]byte("y")); err != nil {
		t.Fatalf("the other stream stopped: %v", err)
	}

	// Drain the blocked writer and verify that the overflow reached the
	// peer, and that another stream still uses the same connection.
	if err := b.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReader(b)
	sawClose, sawFast := false, false
	for !sawClose || !sawFast {
		f, err := readFrame(r)
		if err != nil {
			t.Fatalf("reading frames after overflow: %v", err)
		}
		sawClose = sawClose || f.typ == frameClose && f.stream == slow.id
		sawFast = sawFast || f.typ == frameData && f.stream == fast.id && string(f.payload) == "y"
	}
}
