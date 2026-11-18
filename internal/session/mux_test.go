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
						s.finish(errors.New("test: stream is too slow"))
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
}
