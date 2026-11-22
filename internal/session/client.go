//go:build darwin || linux || windows

package session

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sync"
	"time"
)

// Client is a framed, multiplexed connection to the session server: one
// control channel and every terminal stream share it.
type Client struct {
	mux   *mux
	hello Hello

	mu         sync.Mutex
	nextID     int64
	pending    map[int64]chan Response
	assemblies map[int64]*responseAssembly
	closed     chan struct{}
}

type responseAssembly struct {
	owner    chan Response
	data     []byte
	total    int
	received int
}

type notExistError struct{ message string }

func (e *notExistError) Error() string { return e.message }
func (e *notExistError) Unwrap() error { return os.ErrNotExist }

// Connect connects to the server, starting one in the background when
// none runs.
func Connect() (*Client, error) {
	return connectAt(SocketPath(), true)
}

// ConnectAt connects to a server at an explicit socket, without starting
// one.
func ConnectAt(path string) (*Client, error) {
	return connectAt(path, false)
}

// NewClient starts the framed protocol on rw: it sends this build's
// hello and requires the server's. rw is closed when this returns an
// error or the connection later ends.
func NewClient(rw io.ReadWriteCloser) (*Client, error) {
	r := bufio.NewReaderSize(rw, 1<<20)
	b, _ := json.Marshal(Hello{Version: ProtocolVersion, Role: "client"})
	if err := writeFrame(rw, frame{typ: frameHello, payload: b}); err != nil {
		_ = rw.Close()
		return nil, err
	}
	f, err := readHandshake(r, rw)
	if err != nil {
		_ = rw.Close()
		return nil, err
	}
	if f.typ == frameError {
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(f.payload, &e)
		_ = rw.Close()
		if e.Code == "protocol" {
			server := 0
			var wanted int
			_, _ = fmt.Sscanf(e.Message, "server protocol %d, client wants %d", &server, &wanted)
			return nil, &ProtocolError{Server: server, Client: ProtocolVersion}
		}
		if e.Message != "" {
			return nil, errors.New(e.Message)
		}
		return nil, errors.New("session: the server refused the connection")
	}
	if f.typ != frameHello {
		_ = rw.Close()
		return nil, fmt.Errorf("session: unexpected handshake frame %d", f.typ)
	}
	var h Hello
	if err := json.Unmarshal(f.payload, &h); err != nil {
		_ = rw.Close()
		return nil, err
	}
	if h.Version != ProtocolVersion {
		_ = rw.Close()
		return nil, &ProtocolError{Server: h.Version, Client: ProtocolVersion}
	}
	m := newMux(rw, r)
	c := &Client{
		mux: m, hello: h,
		pending: map[int64]chan Response{}, assemblies: map[int64]*responseAssembly{},
		closed: make(chan struct{}),
	}
	m.start(c.onFrame)
	go func() {
		<-m.quit
		c.mu.Lock()
		for id, ch := range c.pending {
			ch <- Response{ID: id, Error: "session server connection lost"}
			delete(c.pending, id)
		}
		clear(c.assemblies)
		c.mu.Unlock()
		close(c.closed)
	}()
	return c, nil
}

// readHandshake reads the server's hello, giving up on a peer that
// speaks another protocol and waits for a newline that never comes.
func readHandshake(r *bufio.Reader, rw io.Closer) (frame, error) {
	type result struct {
		f   frame
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := readFrame(r)
		ch <- result{f, err}
	}()
	select {
	case res := <-ch:
		return res.f, res.err
	case <-time.After(5 * time.Second):
		_ = rw.Close()
		return frame{}, errors.New("session: the server did not answer the handshake")
	}
}

func connectAt(path string, spawn bool) (*Client, error) {
	conn, err := dialAt(path, spawn)
	if err == nil {
		var c *Client
		if c, err = NewClient(conn); err == nil {
			return c, nil
		}
	}
	if !spawn {
		return nil, err
	}
	// Replace only a confirmed older server. A handshake or network
	// failure is not permission to stop a healthy v5 server.
	for attempt := 0; attempt < 3; attempt++ {
		if err = stopIncompatible(path, err); err != nil {
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
		conn, derr := dialAt(path, true)
		if derr != nil {
			err = derr
			continue
		}
		var c *Client
		if c, err = NewClient(conn); err == nil {
			return c, nil
		}
	}
	return nil, err
}

// dialAt connects, starting a local server first when spawn is set.
func dialAt(path string, spawn bool) (net.Conn, error) {
	conn, err := dial(path)
	if err != nil && spawn {
		deadline := time.Now().Add(8 * time.Second)
		for {
			// An older server may still hold the lock while it exits: ask
			// for another and keep trying, instead of failing on its
			// "already runs" once.
			_ = Spawn()
			for i := 0; i < 10; i++ {
				time.Sleep(30 * time.Millisecond)
				conn, err = dial(path)
				if err == nil {
					return conn, nil
				}
			}
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("session: connecting to the server: %w", err)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// stopIncompatible stops only a server whose older protocol was confirmed
// by its framed version error or by a pre-v4 JSON hello.
func stopIncompatible(path string, connectErr error) error {
	var protocolErr *ProtocolError
	if errors.As(connectErr, &protocolErr) {
		if protocolErr.Server < 4 || protocolErr.Server >= ProtocolVersion {
			return connectErr
		}
		if err := framedShutdown(path, protocolErr.Server); err != nil {
			return fmt.Errorf("session: stopping protocol %d server: %w", protocolErr.Server, err)
		}
	} else {
		if !legacyProtocol(path) {
			return connectErr
		}
		if err := legacyShutdown(path); err != nil {
			return fmt.Errorf("session: stopping legacy server: %w", err)
		}
	}
	return waitStopped(path)
}

// framedShutdown speaks the selected framed protocol through shutdown.
// Versions 4 and later have the same hello, request and small response
// frames; response chunks are irrelevant to this one small response.
func framedShutdown(path string, version int) error {
	conn, err := dial(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	r := bufio.NewReaderSize(conn, 1<<20)
	hello, _ := json.Marshal(Hello{Version: version, Role: "client"})
	if err := writeFrame(conn, frame{typ: frameHello, payload: hello}); err != nil {
		return err
	}
	f, err := readFrame(r)
	if err != nil {
		return err
	}
	if f.typ != frameHello {
		return fmt.Errorf("session: unexpected handshake frame %d", f.typ)
	}
	var answer Hello
	if err := json.Unmarshal(f.payload, &answer); err != nil {
		return err
	}
	if answer.Version != version {
		return fmt.Errorf("session: server changed protocol from %d to %d", version, answer.Version)
	}
	request, _ := json.Marshal(Request{ID: 1, Op: "shutdown"})
	if err := writeFrame(conn, frame{typ: frameRequest, payload: request}); err != nil {
		return err
	}
	f, err = readFrame(r)
	if err != nil {
		return err
	}
	if f.typ != frameResponse {
		return fmt.Errorf("session: unexpected shutdown response frame %d", f.typ)
	}
	var response Response
	if err := json.Unmarshal(f.payload, &response); err != nil {
		return err
	}
	if response.ID != 1 {
		return fmt.Errorf("session: shutdown response has id %d", response.ID)
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

// legacyProtocol explicitly probes the pre-v4 one-line JSON protocol. An
// arbitrary handshake failure must not turn into a shutdown request.
func legacyProtocol(path string) bool {
	response, err := legacyRequest(path, Request{ID: 1, Op: "hello"})
	if err != nil || response.Error != "" {
		return false
	}
	var hello Hello
	if json.Unmarshal(response.Data, &hello) != nil {
		return false
	}
	return hello.Version > 0 && hello.Version < 4
}

// legacyShutdown stops a pre-v4 server at path, whose protocol is one
// JSON line per request. It is the only thing that build understands.
func legacyShutdown(path string) error {
	response, err := legacyRequest(path, Request{ID: 1, Op: "shutdown"})
	if err != nil {
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func legacyRequest(path string, req Request) (Response, error) {
	conn, err := dial(path)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return Response{}, err
	}
	request, _ := json.Marshal(req)
	if err := writeAll(conn, append(request, '\n')); err != nil {
		return Response{}, err
	}
	line, err := bufio.NewReaderSize(conn, 64<<10).ReadSlice('\n')
	if err != nil {
		return Response{}, err
	}
	var response Response
	if err := json.Unmarshal(line, &response); err != nil {
		return Response{}, err
	}
	if response.ID != req.ID {
		return Response{}, fmt.Errorf("session: legacy response has id %d", response.ID)
	}
	return response, nil
}

// waitStopped waits until the old listener has gone. Serve removes its
// socket only after every session has exited, so this also confirms that
// shutdown did not merely accept the request and leave processes behind.
func waitStopped(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if runtime.GOOS == "windows" {
			conn, err := dialTimeout(path, 50*time.Millisecond)
			if err != nil {
				return nil
			}
			_ = conn.Close()
		} else {
			_, err := os.Stat(path)
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
		}
		if time.Now().After(deadline) {
			return errors.New("session: server is still running after shutdown")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Closed closes when the connection, and every stream on it, is gone.
func (c *Client) Closed() <-chan struct{} { return c.closed }

// onFrame routes one frame from the server. It never blocks.
func (c *Client) onFrame(f frame) {
	switch f.typ {
	case frameResponse:
		var res Response
		if json.Unmarshal(f.payload, &res) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[res.ID]
		delete(c.pending, res.ID)
		delete(c.assemblies, res.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- res
		}
	case frameResponsePart:
		c.onResponsePart(f.payload)
	case frameOpen:
		s := c.mux.stream(f.stream)
		if s == nil {
			return
		}
		var reply openReply
		if json.Unmarshal(f.payload, &reply) != nil {
			s.finish(errors.New("session: bad open reply"))
			return
		}
		if !reply.OK {
			s.finish(errors.New(reply.Error))
		}
	case frameData:
		if s := c.mux.stream(f.stream); s != nil {
			if !s.push(f.payload) {
				_ = s.closeWithError(errors.New("session: stream is too slow"))
			}
		}
	case frameClose:
		if s := c.mux.stream(f.stream); s != nil {
			s.finish(io.EOF)
		}
	}
}

func (c *Client) onResponsePart(payload []byte) {
	if len(payload) < responsePartHeaderSize {
		return
	}
	id := int64(binary.BigEndian.Uint64(payload[0:8]))
	offset := uint64(binary.BigEndian.Uint32(payload[8:12]))
	total := uint64(binary.BigEndian.Uint32(payload[12:16]))
	n := uint64(len(payload) - responsePartHeaderSize)

	c.mu.Lock()
	owner := c.pending[id]
	if owner == nil {
		delete(c.assemblies, id)
		c.mu.Unlock()
		return
	}
	if total > maxResponse {
		delete(c.pending, id)
		delete(c.assemblies, id)
		c.mu.Unlock()
		owner <- Response{ID: id, Error: "session: response is too large"}
		return
	}
	valid := total > 0 && offset < total && n > 0 && n <= total-offset
	a := c.assemblies[id]
	if a != nil && a.owner != owner {
		delete(c.assemblies, id)
		a = nil
	}
	if !valid || (a == nil && offset != 0) || (a != nil && (a.total != int(total) || a.received != int(offset))) {
		delete(c.pending, id)
		delete(c.assemblies, id)
		c.mu.Unlock()
		owner <- Response{ID: id, Error: "session: malformed response fragment"}
		return
	}
	if a == nil {
		c.mu.Unlock()
		data := make([]byte, int(total))
		c.mu.Lock()
		if c.pending[id] != owner {
			c.mu.Unlock()
			return
		}
		a = &responseAssembly{owner: owner, data: data, total: int(total)}
		c.assemblies[id] = a
	}
	c.mu.Unlock()

	copy(a.data[int(offset):int(offset+n)], payload[responsePartHeaderSize:])
	c.mu.Lock()
	if c.assemblies[id] != a || c.pending[id] != owner {
		c.mu.Unlock()
		return
	}
	a.received += int(n)
	complete := a.received == a.total
	if complete {
		delete(c.pending, id)
		delete(c.assemblies, id)
	}
	c.mu.Unlock()
	if !complete {
		return
	}
	var res Response
	if json.Unmarshal(a.data, &res) != nil || res.ID != id {
		res = Response{ID: id, Error: "session: malformed response"}
	}
	owner <- res
}

func (c *Client) call(req Request, out any) error {
	return c.callContext(context.Background(), req, out)
}

func (c *Client) callContext(ctx context.Context, req Request, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ch := make(chan Response, 1)
	c.mu.Lock()
	select {
	case <-c.closed:
		c.mu.Unlock()
		return errors.New("session: connection to the server lost")
	default:
	}
	c.nextID++
	req.ID = c.nextID
	b, _ := json.Marshal(req)
	c.pending[req.ID] = ch
	c.mu.Unlock()
	defer c.discardCall(req.ID, ch)
	if err := c.mux.writeFrameContext(ctx, frame{typ: frameRequest, payload: b}); err != nil {
		return err
	}
	timer := time.NewTimer(60 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	select {
	case res := <-ch:
		if err := ctx.Err(); err != nil {
			return err
		}
		if res.Error != "" {
			if res.Code == responseNotExist {
				return &notExistError{message: res.Error}
			}
			return errors.New(res.Error)
		}
		if out != nil && len(res.Data) > 0 {
			return json.Unmarshal(res.Data, out)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("session: %s timed out", req.Op)
	}
}

// discardCall removes only the request that owns ch. A late response or
// cleanup must never remove a newer request that happens to reuse its ID.
func (c *Client) discardCall(id int64, ch chan Response) {
	c.mu.Lock()
	if c.pending[id] == ch {
		delete(c.pending, id)
	}
	if a := c.assemblies[id]; a != nil && a.owner == ch {
		delete(c.assemblies, id)
	}
	c.mu.Unlock()
}

// Hello reports the handshake the server answered, without a round trip.
func (c *Client) Hello() (Hello, error) { return c.hello, nil }

func (c *Client) List() (out []SessionInfo, err error) {
	return c.ListContext(context.Background())
}
func (c *Client) ListContext(ctx context.Context) (out []SessionInfo, err error) {
	err = c.callContext(ctx, Request{Op: "list"}, &out)
	return
}
func (c *Client) Create(o CreateOptions) (out SessionInfo, err error) {
	return c.CreateContext(context.Background(), o)
}
func (c *Client) CreateContext(ctx context.Context, o CreateOptions) (out SessionInfo, err error) {
	err = c.callContext(ctx, Request{Op: "create", Create: &o}, &out)
	return
}
func (c *Client) Kill(sid string) error { return c.call(Request{Op: "kill", SID: sid}, nil) }
func (c *Client) Resize(sid string, cols, rows int) error {
	return c.call(Request{Op: "resize", SID: sid, Cols: cols, Rows: rows}, nil)
}
func (c *Client) Shutdown() error { return c.call(Request{Op: "shutdown"}, nil) }

// Close drops the connection. The server and its terminals keep running.
func (c *Client) Close() error {
	c.mux.shutdown(nil)
	return nil
}

// Ready reports whether a server answers at the standard path.
func Ready() bool {
	conn, err := dialTimeout(SocketPath(), 100*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// Stop asks the server at the standard socket to shut down. A missing
// socket means no server is running, which is already the wanted state.
func Stop() error {
	path := SocketPath()
	c, err := ConnectAt(path)
	if err == nil {
		shutdownErr := c.Shutdown()
		_ = c.Close()
		if shutdownErr != nil {
			return shutdownErr
		}
		return waitStopped(path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return stopIncompatible(path, err)
}

// Stream is a terminal's byte stream attached to a session.
type Stream struct {
	c   *Client
	ms  *muxStream
	sid string

	mu   sync.Mutex
	cols int
	rows int
}

// openRequest asks the server to attach a new stream to a session or start
// a specialized stream (such as LSP or project watch) when Kind is set.
type openRequest struct {
	SID  string   `json:"sid,omitempty"`
	Cols int      `json:"cols,omitempty"`
	Rows int      `json:"rows,omitempty"`
	Kind string   `json:"kind,omitempty"`
	Dir  string   `json:"dir,omitempty"`
	Args []string `json:"args,omitempty"`
}

// openReply answers an openRequest on the same stream.
type openReply struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Stream attaches to the session sid. It returns at once; the server's
// reply and the screen arrive on the stream.
func (c *Client) Stream(sid string, cols, rows int) *Stream {
	id := c.mux.newStreamID()
	ms := c.mux.openStream(id)
	s := &Stream{c: c, ms: ms, sid: sid, cols: cols, rows: rows}
	b, _ := json.Marshal(openRequest{SID: sid, Cols: cols, Rows: rows})
	if err := c.mux.writeFrame(frame{typ: frameOpen, stream: id, payload: b}); err != nil {
		ms.finish(err)
	}
	return s
}

func (s *Stream) Read(p []byte) (int, error)  { return s.ms.Read(p) }
func (s *Stream) Write(p []byte) (int, error) { return s.ms.Write(p) }

// Resize tells the session the terminal's size when it changed.
func (s *Stream) Resize(cols, rows int) error {
	s.mu.Lock()
	changed := cols != s.cols || rows != s.rows
	s.cols, s.rows = cols, rows
	s.mu.Unlock()
	if !changed {
		return nil
	}
	return s.c.Resize(s.sid, cols, rows)
}

// Close detaches; the session keeps running.
func (s *Stream) Close() error { return s.ms.Close() }

func (c *Client) ListTree(root string) (out []FileEntry, err error) {
	return c.ListTreeContext(context.Background(), root)
}
func (c *Client) ListTreeContext(ctx context.Context, root string) (out []FileEntry, err error) {
	err = c.callContext(ctx, Request{Op: "listTree", Path: root}, &out)
	return
}

// ListTreeFresh is ListTree that bypasses the server's listing cache.
func (c *Client) ListTreeFresh(root string) (out []FileEntry, err error) {
	return c.ListTreeFreshContext(context.Background(), root)
}
func (c *Client) ListTreeFreshContext(ctx context.Context, root string) (out []FileEntry, err error) {
	err = c.callContext(ctx, Request{Op: "listTree", Path: root, Fresh: true}, &out)
	return
}
func (c *Client) ReadFile(path string) (data []byte, err error) {
	return c.ReadFileContext(context.Background(), path)
}
func (c *Client) ReadFileContext(ctx context.Context, path string) (data []byte, err error) {
	err = c.callContext(ctx, Request{Op: "readFile", Path: path}, &data)
	return
}
func (c *Client) WriteFile(path string, data []byte) error {
	return c.WriteFileContext(context.Background(), path, data)
}
func (c *Client) WriteFileContext(ctx context.Context, path string, data []byte) error {
	return c.callContext(ctx, Request{Op: "writeFile", Path: path, Data: data}, nil)
}
func (c *Client) Mkdir(path string) error { return c.call(Request{Op: "mkdir", Path: path}, nil) }
func (c *Client) Rename(from, to string) error {
	return c.call(Request{Op: "rename", Path: from, To: to}, nil)
}
func (c *Client) Remove(path string) error { return c.call(Request{Op: "remove", Path: path}, nil) }
func (c *Client) Stat(path string) (entry FileEntry, err error) {
	err = c.call(Request{Op: "stat", Path: path}, &entry)
	return
}
func (c *Client) Copy(from, to string) error {
	return c.call(Request{Op: "copy", Path: from, To: to}, nil)
}
func (c *Client) Exec(dir string, args, env []string, timeout time.Duration) (res ExecResult, err error) {
	return c.ExecStdin(dir, args, env, "", timeout)
}
func (c *Client) ExecStdin(dir string, args, env []string, stdin string, timeout time.Duration) (res ExecResult, err error) {
	err = c.call(Request{
		Op: "exec", Dir: dir, Args: args, Env: env, Stdin: stdin,
		Timeout: int(timeout / time.Millisecond),
	}, &res)
	return
}
func (c *Client) ListDirs(path string) (out []FileEntry, err error) {
	return c.ListDirsContext(context.Background(), path)
}
func (c *Client) ListDirsContext(ctx context.Context, path string) (out []FileEntry, err error) {
	err = c.callContext(ctx, Request{Op: "listDirs", Path: path}, &out)
	return
}
func (c *Client) Search(root string, opts SearchOptions) (out SearchResult, err error) {
	return c.SearchContext(context.Background(), root, opts)
}
func (c *Client) SearchContext(ctx context.Context, root string, opts SearchOptions) (out SearchResult, err error) {
	err = c.callContext(ctx, Request{Op: "search", Path: root, Search: &opts}, &out)
	return
}
