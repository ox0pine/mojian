package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Queue sizes bound what one connection holds: control replies are few,
// a terminal stream is a burst of output, and an input queue is what a
// peer may fall behind on before its stream is dropped.
const (
	ctrlQueue   = 64
	streamQueue = 4096
	inQueue     = 64
)

// mux multiplexes frames over one connection. A single reader goroutine
// runs the peer's frames through onFrame and a single writer goroutine
// sends queued frames, so a stalled stream only ever drops that stream.
type mux struct {
	rw  io.ReadWriteCloser
	r   *bufio.Reader
	wmu sync.Mutex

	mu      sync.Mutex
	streams map[uint32]*muxStream
	order   []*muxStream
	cursor  int
	nextID  uint32

	ctrl   chan frame
	wake   chan struct{}
	quit   chan struct{}
	once   sync.Once
	closed bool
	err    error
}

func newMux(rw io.ReadWriteCloser, r *bufio.Reader) *mux {
	return &mux{
		rw: rw, r: r,
		streams: map[uint32]*muxStream{},
		ctrl:    make(chan frame, ctrlQueue),
		wake:    make(chan struct{}, 1),
		quit:    make(chan struct{}),
	}
}

// start runs the reader and writer loops. onFrame is called from the
// reader goroutine and must not block.
func (m *mux) start(onFrame func(frame)) {
	go m.readLoop(onFrame)
	go m.writeLoop()
}

func (m *mux) readLoop(onFrame func(frame)) {
	for {
		f, err := readFrame(m.r)
		if err != nil {
			m.shutdown(err)
			return
		}
		if f.typ == frameError {
			var e struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			_ = json.Unmarshal(f.payload, &e)
			if e.Message == "" {
				e.Message = "session: the peer reported an error"
			}
			m.shutdown(errors.New(e.Message))
			return
		}
		onFrame(f)
	}
}

// shutdown ends the connection once: every stream fails with err, the
// wake and reader loops see quit, and the underlying connection closes.
func (m *mux) shutdown(err error) {
	m.once.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.err = err
		streams := make([]*muxStream, 0, len(m.streams))
		for _, s := range m.streams {
			streams = append(streams, s)
		}
		m.mu.Unlock()
		close(m.quit)
		_ = m.rw.Close()
		for _, s := range streams {
			s.finish(err)
		}
	})
}

// errLocked reports why the mux ended.
func (m *mux) errLocked() error {
	if m.err != nil {
		return m.err
	}
	return io.EOF
}

// error reports why the mux ended, as a read would.
func (m *mux) error() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.errLocked()
}

// writeControl queues one control frame; it never waits behind stream
// data. It returns once the frame is queued, not once it is written.
func (m *mux) writeControl(typ byte, payload []byte) error {
	return m.writeFrame(frame{typ: typ, payload: payload})
}

// writeFrame queues any frame for the writer goroutine.
func (m *mux) writeFrame(f frame) error {
	return m.writeFrameContext(context.Background(), f)
}

func (m *mux) writeFrameContext(ctx context.Context, f frame) error {
	if err, closed := m.closedError(); closed {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.ctrl <- f:
		// If shutdown raced the send, tell the caller not to wait for a
		// writer that has already exited.
		if err, closed := m.closedError(); closed {
			return err
		}
		return nil
	default:
	}
	select {
	case m.ctrl <- f:
		if err, closed := m.closedError(); closed {
			return err
		}
		return nil
	case <-m.quit:
		return m.error()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *mux) closedError() (error, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		return nil, false
	}
	return m.errLocked(), true
}

func (m *mux) writeLoop() {
	for {
		select {
		case f := <-m.ctrl:
			if m.write(f) != nil {
				return
			}
			continue
		default:
		}
		select {
		case f := <-m.ctrl:
			if m.write(f) != nil {
				return
			}
		case <-m.wake:
			for {
				// A busy stream must not starve control replies while its
				// queue keeps refilling between writes.
				select {
				case f := <-m.ctrl:
					if m.write(f) != nil {
						return
					}
					continue
				default:
				}
				f, ok := m.nextData()
				if !ok {
					break
				}
				if m.write(f) != nil {
					return
				}
			}
		case <-m.quit:
			return
		}
	}
}

// nextData takes one queued payload, rotating through the streams so a
// busy one cannot starve the others. A finished stream drains its queue
// and then leaves the rotation, so its close marker still reaches the
// peer after the output before it.
func (m *mux) nextData() (frame, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for n := len(m.order); n > 0 && len(m.order) > 0; n-- {
		if m.cursor >= len(m.order) {
			m.cursor = 0
		}
		s := m.order[m.cursor]
		if s.finished && len(s.out) == 0 {
			m.order = append(m.order[:m.cursor], m.order[m.cursor+1:]...)
			if m.cursor >= len(m.order) {
				m.cursor = 0
			}
			continue
		}
		select {
		case sf := <-s.out:
			if sf.close {
				m.order = append(m.order[:m.cursor], m.order[m.cursor+1:]...)
				if m.cursor >= len(m.order) {
					m.cursor = 0
				}
				return frame{typ: frameClose, stream: s.id}, true
			}
			m.cursor++
			if m.cursor >= len(m.order) {
				m.cursor = 0
			}
			return frame{typ: frameData, stream: s.id, payload: sf.data}, true
		default:
			m.cursor++
		}
	}
	return frame{}, false
}

// write sends one frame. It holds wmu so frames never interleave.
func (m *mux) write(f frame) error {
	m.wmu.Lock()
	err := writeFrame(m.rw, f)
	m.wmu.Unlock()
	if err != nil {
		m.shutdown(err)
	}
	return err
}

// wakeup tells the writer goroutine that a stream has data queued.
func (m *mux) wakeup() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// newStreamID allocates the next stream id of this connection.
func (m *mux) newStreamID() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	return m.nextID
}

// openStream registers a stream before its first frame is queued, so a
// frame that races the stream's own open reply still finds it.
func (m *mux) openStream(id uint32) *muxStream {
	s := &muxStream{
		m: m, id: id,
		out:  make(chan streamFrame, streamQueue),
		in:   make(chan []byte, inQueue),
		done: make(chan struct{}),
	}
	m.mu.Lock()
	if m.closed {
		s.err = m.errLocked()
		s.finished = true
		s.once.Do(func() { close(s.done) })
		m.mu.Unlock()
		return s
	}
	m.streams[id] = s
	m.order = append(m.order, s)
	m.mu.Unlock()
	return s
}

func (m *mux) stream(id uint32) *muxStream {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streams[id]
}

// removeStream stops routing frames of a finished stream. Its writer
// entry stays until its queue is drained, so a close marker still
// reaches the peer after the output queued before it.
func (m *mux) removeStream(id uint32) {
	m.mu.Lock()
	delete(m.streams, id)
	m.mu.Unlock()
	m.wakeup()
}

// streamFrame is one item of a stream's out queue. A close marker
// travels behind every queued payload, so the peer reads the output
// before it reads the end of the stream.
type streamFrame struct {
	data  []byte
	close bool
}

// muxStream is one terminal's byte stream: data queued to the peer on
// out, data read from the peer on in.
type muxStream struct {
	m   *mux
	id  uint32
	out chan streamFrame
	in  chan []byte

	outMu sync.Mutex
	buf   []byte
	mu    sync.Mutex
	err   error
	// finished is read by the writer under the mux's lock.
	finished  bool
	done      chan struct{}
	once      sync.Once
	closeOnce sync.Once
}

// Read returns the peer's bytes, then the stream's error or io.EOF.
func (s *muxStream) Read(p []byte) (int, error) {
	for {
		if len(s.buf) > 0 {
			n := copy(p, s.buf)
			s.buf = s.buf[n:]
			return n, nil
		}
		select {
		case b := <-s.in:
			s.buf = b
			continue
		default:
		}
		select {
		case b := <-s.in:
			s.buf = b
		case <-s.done:
			select {
			case b := <-s.in:
				s.buf = b
				continue
			default:
			}
			s.mu.Lock()
			err := s.err
			s.mu.Unlock()
			if err == nil {
				return 0, io.EOF
			}
			return 0, err
		}
	}
}

// Write queues p for the peer. The payload is copied because callers
// reuse their buffers.
func (s *muxStream) Write(p []byte) (int, error) {
	select {
	case <-s.done:
		return 0, s.readErr()
	default:
	}
	if !s.enqueue(append([]byte(nil), p...)) {
		_ = s.closeWithError(errors.New("session: stream is too slow"))
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

// enqueue queues one payload without blocking; false means the stream's
// queue is full and the stream, not the connection, must end.
func (s *muxStream) enqueue(p []byte) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	s.outMu.Lock()
	defer s.outMu.Unlock()
	select {
	case <-s.done:
		return false
	default:
	}
	// Keep one slot for Close. A slow stream must never have to wait for
	// space in the unrelated control queue to tell its peer it ended.
	if len(s.out) >= cap(s.out)-1 {
		return false
	}
	select {
	case s.out <- streamFrame{data: p}:
		s.m.wakeup()
		return true
	default:
		return false
	}
}

// push queues bytes read from the peer, under the same rule as enqueue.
func (s *muxStream) push(p []byte) bool {
	select {
	case <-s.done:
		return false
	default:
	}
	select {
	case s.in <- p:
		return true
	default:
		return false
	}
}

func (s *muxStream) readErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	return io.EOF
}

// Close tells the peer the stream ended, after the output already
// queued, then finishes it locally. The close is sent even when a peer
// error already finished the local side.
func (s *muxStream) Close() error { return s.closeWithError(nil) }

func (s *muxStream) closeWithError(streamErr error) error {
	var err error
	s.closeOnce.Do(func() {
		if closeErr, closed := s.m.closedError(); closed {
			err = closeErr
			s.finish(streamErr)
			return
		}
		// nextData uses the same lock. Once a stream is marked finished
		// it may leave the rotation at any moment, so a late close must
		// use the control queue rather than an unreachable out queue.
		s.outMu.Lock()
		s.m.mu.Lock()
		queued := false
		if !s.finished {
			select {
			case s.out <- streamFrame{close: true}:
				queued = true
			default:
			}
		}
		s.m.mu.Unlock()
		s.outMu.Unlock()
		if queued {
			s.m.wakeup()
		} else {
			err = s.m.writeFrame(frame{typ: frameClose, stream: s.id})
		}
		s.finish(streamErr)
	})
	return err
}

// finish ends the stream without telling the peer; the caller that owns
// the peer's state does that with Close or with a frame.
func (s *muxStream) finish(err error) {
	s.once.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
		s.m.mu.Lock()
		s.finished = true
		s.m.mu.Unlock()
		s.m.removeStream(s.id)
	})
}
