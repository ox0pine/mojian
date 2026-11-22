//go:build darwin || linux || windows

package session

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"mojian/internal/filekind"

	"github.com/egoist/mygo/plugins/terminal"
)

const (
	scrollback  = 8 << 20
	resyncDelay = 150 * time.Millisecond
	// readFileLimit bounds a file sent in one request, and
	// writeFileLimit one written in one.
	readFileLimit  = 32 << 20
	writeFileLimit = 32 << 20
)

var errServerShutdown = errors.New("session: server is shutting down")

// Server owns the sessions and their pseudo-terminals.
type Server struct {
	mu       sync.Mutex
	sessions map[string]*session
	order    []string
	started  time.Time
	quit     chan struct{}
	once     sync.Once
	kills    sync.WaitGroup
	// trees caches listTree results; a file operation clears it and a
	// short TTL picks up changes made outside the server.
	trees map[string]treeCacheEntry
}

// treeCacheTTL bounds how long a listing is reused. The app's explicit
// refresh asks for a fresh listing, so this only serves repeated listing
// calls such as a focus refresh or opening the sidebar.
const treeCacheTTL = 5 * time.Second

type treeCacheEntry struct {
	entries []FileEntry
	at      time.Time
}

// Serve runs the session server until it is shut down.
func Serve() error {
	dir := RuntimeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := lockFile(lock); err != nil {
		return errors.New("session: a server already runs")
	}
	sock := SocketPath()
	removeSocket(sock)
	ln, err := listen(sock)
	if err != nil {
		return err
	}
	s := &Server{
		sessions: map[string]*session{}, order: nil, started: time.Now(),
		quit: make(chan struct{}),
	}
	go func() {
		<-s.quit
		_ = ln.Close()
	}()
	go s.pruneLoop()
	log.Printf("session: serving on %s (pid %d)", sock, os.Getpid())
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.quit:
				s.stopSessions()
				removeSocket(sock)
				return nil
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return err
		}
		go s.serveConn(conn)
	}
}

func (s *Server) shutdown() {
	s.once.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		close(s.quit)
	})
}

// stopSessions detaches every registered session and starts all final
// kills before waiting. A kill request that already detached its session
// is included in the same wait group; one that arrives later finds no
// session and cannot add work after the wait starts.
func (s *Server) stopSessions() {
	s.mu.Lock()
	all := make([]*session, 0, len(s.sessions))
	for id, ss := range s.sessions {
		all = append(all, ss)
		delete(s.sessions, id)
	}
	s.order = nil
	s.kills.Add(len(all))
	s.mu.Unlock()
	for _, ss := range all {
		go s.killSession(ss)
	}
	s.kills.Wait()
}

func (s *Server) killSession(ss *session) {
	defer s.kills.Done()
	ss.kill()
}

// pruneLoop forgets exited sessions; the server itself runs until it is
// stopped explicitly.
func (s *Server) pruneLoop() {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-t.C:
		}
		s.pruneExited()
	}
}

// pruneExited forgets sessions whose shell has finished, so a dead
// terminal does not hide a live session behind the same id.
func (s *Server) pruneExited() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, ss := range s.sessions {
		if !ss.hasExited() {
			continue
		}
		delete(s.sessions, id)
		s.order = removeString(s.order, id)
	}
}

// serveConn answers a connection's hello and then hands every frame to
// dispatch. One connection carries the control requests and all streams.
func (s *Server) serveConn(conn net.Conn) {
	r := bufio.NewReaderSize(conn, 1<<20)
	f, err := readFrame(r)
	if err != nil || f.typ != frameHello {
		_ = conn.Close()
		return
	}
	var hello Hello
	if json.Unmarshal(f.payload, &hello) != nil {
		_ = conn.Close()
		return
	}
	if hello.Version != ProtocolVersion {
		msg, _ := json.Marshal(map[string]string{
			"code":    "protocol",
			"message": fmt.Sprintf("server protocol %d, client wants %d", ProtocolVersion, hello.Version),
		})
		_ = writeFrame(conn, frame{typ: frameError, payload: msg})
		_ = conn.Close()
		return
	}
	m := newMux(conn, r)
	reply, _ := json.Marshal(Hello{
		Version: ProtocolVersion, PID: os.Getpid(), Started: s.started, Role: "server",
	})
	// The handshake reply goes out directly: it is the first frame, and
	// writing it here keeps a client that closes at once from racing the
	// writer loop for it.
	if err := writeFrame(conn, frame{typ: frameHello, payload: reply}); err != nil {
		_ = conn.Close()
		return
	}
	m.start(func(f frame) { s.dispatch(m, f) })
}

func (s *Server) dispatch(m *mux, f frame) {
	switch f.typ {
	case frameRequest:
		var req Request
		if json.Unmarshal(f.payload, &req) != nil {
			// A request the server cannot read leaves the caller waiting
			// for an answer that will never come: end the connection,
			// which reports the lost connection to every pending call.
			m.shutdown(errors.New("session: malformed request"))
			return
		}
		go func(req Request) {
			_ = writeResponse(m, s.request(req))
		}(req)
	case frameOpen:
		var o openRequest
		if json.Unmarshal(f.payload, &o) != nil {
			return
		}
		if o.Kind == "lsp" {
			ms := m.openStream(f.stream)
			b, _ := json.Marshal(openReply{OK: true})
			_ = m.writeFrame(frame{typ: frameOpen, stream: f.stream, payload: b})
			go s.serveLSP(ms, o)
			return
		}
		if o.Kind == "watch" {
			ms := m.openStream(f.stream)
			b, _ := json.Marshal(openReply{OK: true})
			_ = m.writeFrame(frame{typ: frameOpen, stream: f.stream, payload: b})
			go s.serveWatch(ms, o)
			return
		}
		ss, err := s.session(o.SID)
		reply := openReply{OK: err == nil}
		var ms *muxStream
		if err != nil {
			reply.Error = err.Error()
		} else {
			// Register the stream before the ack, so a data frame that
			// follows at once still finds it.
			ms = m.openStream(f.stream)
		}
		b, _ := json.Marshal(reply)
		_ = m.writeFrame(frame{typ: frameOpen, stream: f.stream, payload: b})
		if ms != nil {
			go ss.attach(ms, o.Cols, o.Rows)
		}
	case frameData:
		ms := m.stream(f.stream)
		if ms == nil {
			return
		}
		if !ms.push(f.payload) {
			_ = ms.closeWithError(errors.New("session: stream is too slow"))
		}
	case frameClose:
		if ms := m.stream(f.stream); ms != nil {
			ms.finish(io.EOF)
		}
	case frameError:
		m.shutdown(errors.New("session: the client reported an error"))
	}
}

func (s *Server) request(req Request) Response {
	data, err := s.do(req)
	res := Response{ID: req.ID}
	if err != nil {
		res.Error = err.Error()
		if errors.Is(err, os.ErrNotExist) {
			res.Code = responseNotExist
		}
	} else if data != nil {
		res.Data, _ = json.Marshal(data)
	}
	return res
}

func (s *Server) session(id string) (*session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ss := s.sessions[id]; ss != nil {
		return ss, nil
	}
	return nil, fmt.Errorf("no session %q", id)
}

func (s *Server) do(req Request) (any, error) {
	switch req.Op {
	case "list":
		s.pruneExited()
		s.mu.Lock()
		all := make([]*session, 0, len(s.order))
		for _, id := range s.order {
			all = append(all, s.sessions[id])
		}
		s.mu.Unlock()
		out := make([]SessionInfo, len(all))
		for i, ss := range all {
			out[i] = ss.info()
		}
		return out, nil
	case "listTree":
		return s.listTree(req.Path, req.Fresh)
	case "search":
		opts := SearchOptions{}
		if req.Search != nil {
			opts = *req.Search
		}
		return SearchDir(context.Background(), req.Path, opts)
	case "readFile":
		info, err := os.Stat(req.Path)
		if err != nil {
			return nil, err
		}
		if info.Size() > readFileLimit {
			return nil, fmt.Errorf("%s is larger than %d MB", filepath.Base(req.Path), readFileLimit>>20)
		}
		return os.ReadFile(req.Path)
	case "writeFile":
		if len(req.Data) > writeFileLimit {
			return nil, fmt.Errorf("file is larger than %d MB", writeFileLimit>>20)
		}
		if err := writeFileAtomic(req.Path, req.Data); err != nil {
			return nil, err
		}
		s.invalidateTrees()
		return nil, nil
	case "stat":
		return statEntry(req.Path)
	case "copy":
		if req.Path == "" || req.To == "" {
			return nil, errors.New("copy needs paths")
		}
		err := copyTree(req.Path, req.To)
		if err == nil {
			s.invalidateTrees()
		}
		return nil, err
	case "exec":
		out, err := execCommand(req)
		s.invalidateTrees()
		return out, err
	case "mkdir":
		err := os.MkdirAll(req.Path, 0o755)
		if err == nil {
			s.invalidateTrees()
		}
		return nil, err
	case "rename":
		if req.Path == "" || req.To == "" {
			return nil, errors.New("rename needs paths")
		}
		err := os.Rename(req.Path, req.To)
		if err == nil {
			s.invalidateTrees()
		}
		return nil, err
	case "remove":
		err := os.RemoveAll(req.Path)
		if err == nil {
			s.invalidateTrees()
		}
		return nil, err
	case "listDirs":
		entries, err := os.ReadDir(req.Path)
		if err != nil {
			return nil, err
		}
		var dirs []FileEntry
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			dirs = append(dirs, FileEntry{Rel: filepath.Join(req.Path, e.Name()), Dir: true, Mod: info.ModTime()})
		}
		return dirs, nil
	case "create":
		s.mu.Lock()
		select {
		case <-s.quit:
			s.mu.Unlock()
			return nil, errServerShutdown
		default:
		}
		s.mu.Unlock()
		o := CreateOptions{}
		if req.Create != nil {
			o = *req.Create
		}
		id := newID()
		ss, err := newSession(id, o)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		select {
		case <-s.quit:
			s.mu.Unlock()
			// Shutdown may have taken its final session snapshot while the
			// PTY was being created. Do not register a session it cannot kill.
			ss.kill()
			return nil, errServerShutdown
		default:
		}
		s.sessions[id] = ss
		s.order = append(s.order, id)
		s.mu.Unlock()
		return ss.info(), nil
	case "kill":
		// Do not hold the control connection for the shell's interrupt
		// grace period; the session is detached at once and killed in
		// the background. Detaching and adding to kills are atomic with
		// the final shutdown snapshot, so Serve cannot miss this process.
		s.mu.Lock()
		ss := s.sessions[req.SID]
		if ss != nil {
			delete(s.sessions, req.SID)
			s.order = removeString(s.order, req.SID)
			s.kills.Add(1)
		}
		s.mu.Unlock()
		if ss != nil {
			go s.killSession(ss)
		}
		return nil, nil
	case "resize":
		ss, err := s.session(req.SID)
		if err != nil {
			return nil, err
		}
		ss.resize(req.Cols, req.Rows)
		return nil, nil
	case "shutdown":
		s.shutdown()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown op %q", req.Op)
	}
}

func removeString(xs []string, x string) []string {
	out := xs[:0]
	for _, s := range xs {
		if s != x {
			out = append(out, s)
		}
	}
	return out
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type session struct {
	id      string
	shell   string
	dir     string
	created time.Time
	proc    ptyProcess

	mu         sync.Mutex
	vt         *terminal.Terminal
	lastTitle  string
	clients    map[*attached]struct{}
	cols, rows int
	exited     bool
	code       int
	done       chan struct{}
	resync     *time.Timer
}

type attached struct {
	st *muxStream
}

func (a *attached) send(p []byte) bool { return a.st.enqueue(p) }

// finish ends the stream and tells the client, so its read sees EOF
// instead of waiting for the connection to die.
func (a *attached) finish() { _ = a.st.Close() }

func newSession(id string, o CreateOptions) (*session, error) {
	argv, name, err := commandArgs(o.Command)
	if err != nil {
		return nil, err
	}
	cols, rows := o.Cols, o.Rows
	if cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	dir := o.Dir
	if dir == "" {
		dir, _ = os.UserHomeDir()
	}
	proc, err := startPTY(argv, dir, sessionEnv(o.Env), cols, rows)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	vt, err := terminal.New(terminal.Options{Conn: discard{pr}, Scrollback: scrollback})
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		_ = proc.Kill()
		_ = proc.Close()
		_, _ = proc.Wait()
		return nil, err
	}
	vt.Resize(cols, rows)
	s := &session{
		id: id, shell: name, dir: dir, created: time.Now(), proc: proc,
		vt: vt, clients: map[*attached]struct{}{},
		cols: cols, rows: rows, done: make(chan struct{}),
	}
	readDone := make(chan struct{})
	go func() {
		s.read()
		close(readDone)
	}()
	go func() {
		code, _ := proc.Wait()
		select {
		case <-readDone:
		case <-time.After(200 * time.Millisecond):
			_ = proc.Close()
			<-readDone
		}
		s.mu.Lock()
		s.exited, s.code = true, code
		if s.vt != nil {
			s.lastTitle = s.vt.Title()
		}
		closedVT := s.vt
		s.vt = nil
		for a := range s.clients {
			a.finish()
		}
		s.clients = map[*attached]struct{}{}
		s.mu.Unlock()
		close(s.done)
		_ = pw.Close()
		if closedVT != nil {
			closedVT.Close()
		}
	}()
	return s, nil
}

func commandArgs(command []string) ([]string, string, error) {
	if len(command) > 0 {
		path, err := exec.LookPath(command[0])
		if err != nil {
			return nil, "", err
		}
		return append([]string{path}, command[1:]...), filepath.Base(command[0]), nil
	}
	return defaultShell()
}

func sessionEnv(extra []string) []string {
	var env []string
	hasLang := false
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if runtime.GOOS == "windows" {
			k = strings.ToUpper(k)
		}
		switch {
		case k == "TERM", k == "COLORTERM", k == "TERM_PROGRAM", k == "TERM_PROGRAM_VERSION",
			k == "TERM_SESSION_ID", k == "TMUX", k == "TMUX_PANE", k == "STY", k == "WINDOWID",
			k == "COLUMNS", k == "LINES", k == "SHLVL", k == "OLDPWD", k == "PWD", k == "_",
			strings.HasPrefix(k, "ITERM_"), strings.HasPrefix(k, "GHOSTTY_"), strings.HasPrefix(k, "KITTY_"),
			strings.HasPrefix(k, "VSCODE_"), strings.HasPrefix(k, "WEZTERM_"), strings.HasPrefix(k, "ALACRITTY_"),
			strings.HasPrefix(k, "MOJIAN_"):
			continue
		case k == "LANG" || k == "LC_ALL" || k == "LC_CTYPE":
			hasLang = true
		}
		env = append(env, kv)
	}
	env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor", "TERM_PROGRAM=Mojian")
	if !hasLang {
		env = append(env, "LANG=en_US.UTF-8")
	}
	return append(env, extra...)
}

func (s *session) read() {
	buf := make([]byte, 64<<10)
	for {
		n, err := s.proc.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.vt.Feed(data)
			for a := range s.clients {
				if !a.send(data) {
					a.finish()
					delete(s.clients, a)
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (s *session) attach(ms *muxStream, cols, rows int) {
	a := &attached{st: ms}
	s.mu.Lock()
	exited := s.exited || s.vt == nil
	var snap []byte
	var resized bool
	if !exited {
		if cols > 0 && rows > 0 && (cols != s.cols || rows != s.rows) {
			s.cols, s.rows = cols, rows
			s.vt.Resize(cols, rows)
			resized = true
		}
		snap = s.vt.Snapshot()
		if title := s.vt.Title(); title != "" {
			snap = append([]byte("\x1b]2;"+title+"\x07"), snap...)
		}
	}
	// Queue the snapshot before joining the clients, so live output
	// cannot reach the stream ahead of the screen it belongs on.
	full := len(snap) > 0 && !a.send(snap)
	if !exited && !full {
		s.clients[a] = struct{}{}
	}
	s.mu.Unlock()
	if full {
		_ = ms.closeWithError(errors.New("session: stream is too slow"))
		return
	}
	if exited {
		a.finish()
		return
	}
	if resized {
		_ = s.proc.Resize(cols, rows)
		s.mu.Lock()
		if !s.exited {
			if s.resync == nil {
				s.resync = time.AfterFunc(resyncDelay, s.resyncScreen)
			} else {
				s.resync.Reset(resyncDelay)
			}
		}
		s.mu.Unlock()
	}
	buf := make([]byte, 32<<10)
	for {
		n, err := ms.Read(buf)
		if n > 0 {
			s.input(buf[:n])
		}
		if err != nil {
			break
		}
	}
	s.mu.Lock()
	_, joined := s.clients[a]
	delete(s.clients, a)
	s.mu.Unlock()
	if joined {
		a.finish()
	}
}

func (s *session) input(p []byte) {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		_, _ = s.proc.Write(p)
	}
}

func (s *session) resize(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	s.mu.Lock()
	exited := s.exited || s.vt == nil
	changed := !exited && (cols != s.cols || rows != s.rows)
	if changed {
		s.cols, s.rows = cols, rows
		s.vt.Resize(cols, rows)
		if s.resync == nil {
			s.resync = time.AfterFunc(resyncDelay, s.resyncScreen)
		} else {
			s.resync.Reset(resyncDelay)
		}
	}
	s.mu.Unlock()
	if changed && !exited {
		_ = s.proc.Resize(cols, rows)
	}
}

func (s *session) resyncScreen() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited || s.vt == nil || len(s.clients) == 0 {
		return
	}
	snap := s.vt.Snapshot()
	if !bytes.Contains(snap, []byte("\x1b[?1049h")) {
		return
	}
	msg := append([]byte("\x1b[?1049h\x1b[H\x1b[2J"), snap...)
	for a := range s.clients {
		if !a.send(msg) {
			a.finish()
			delete(s.clients, a)
		}
	}
}

func (s *session) kill() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if !exited {
		_ = s.proc.Interrupt()
	}
	timer := time.NewTimer(time.Second)
	select {
	case <-s.done:
		_ = timer.Stop()
	case <-timer.C:
		_ = s.proc.Kill()
		<-s.done
	}
	_ = s.proc.Close()
}

// hasExited reports whether the session's program has finished.
func (s *session) hasExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *session) info() SessionInfo {
	s.mu.Lock()
	title := s.lastTitle
	if s.vt != nil {
		title = s.vt.Title()
	}
	in := SessionInfo{
		ID: s.id, Shell: s.shell, Dir: s.dir, Created: s.created,
		Cols: s.cols, Rows: s.rows, Title: title,
		Exited: s.exited, ExitCode: s.code,
	}
	s.mu.Unlock()
	in.PID = s.proc.Pid()
	return in
}

type discard struct{ r *io.PipeReader }

func (d discard) Read(p []byte) (int, error)  { return d.r.Read(p) }
func (d discard) Write(p []byte) (int, error) { return len(p), nil }
func (d discard) Close() error                { return d.r.Close() }

func fileHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// invalidateTrees drops every cached listing; a file operation may have
// changed one root, and there are only a few.
func (s *Server) invalidateTrees() {
	s.mu.Lock()
	s.trees = nil
	s.mu.Unlock()
}

// listTree walks a project, reusing a recent result unless fresh is set.
func (s *Server) listTree(root string, fresh bool) ([]FileEntry, error) {
	if !fresh {
		s.mu.Lock()
		cached, ok := s.trees[root]
		s.mu.Unlock()
		if ok && time.Since(cached.at) < treeCacheTTL {
			return append([]FileEntry(nil), cached.entries...), nil
		}
	}
	entries, err := walkTree(root)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.trees == nil {
		s.trees = map[string]treeCacheEntry{}
	}
	s.trees[root] = treeCacheEntry{entries: entries, at: time.Now()}
	s.mu.Unlock()
	return entries, nil
}

func walkTree(root string) ([]FileEntry, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	var out []FileEntry
	ign := filekind.NewGitIgnore(root)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				// An unreadable root must not look like an empty project:
				// that would wipe the tree while the files still exist.
				return err
			}
			return nil
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if filekind.SkipDir(d.Name()) {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			if ign.DirIgnored(filepath.ToSlash(rel)) {
				return filepath.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			out = append(out, FileEntry{Rel: filepath.ToSlash(rel), Dir: true, Mod: info.ModTime()})
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		kind, err := filekind.ClassifyFile(path)
		if err != nil || kind == filekind.Other {
			return nil
		}
		e := FileEntry{
			Rel: filepath.ToSlash(rel), Size: info.Size(), Mod: info.ModTime(), Kind: kind,
		}
		if kind == filekind.Asset {
			e.Hash = fileHash(path)
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

func statEntry(path string) (FileEntry, error) {
	linkInfo, err := os.Lstat(path)
	if err != nil {
		return FileEntry{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if linkInfo.Mode()&os.ModeSymlink != 0 && errors.Is(err, os.ErrNotExist) {
			// A broken link still occupies its directory entry. Report the
			// link itself so a case-only rename cannot silently overwrite it.
			return FileEntry{
				Rel: path, Dir: false, Size: linkInfo.Size(), Mod: linkInfo.ModTime(), Kind: filekind.Other,
			}, nil
		}
		return FileEntry{}, err
	}
	e := FileEntry{Rel: path, Dir: info.IsDir(), Size: info.Size(), Mod: info.ModTime()}
	if !info.IsDir() {
		e.Kind, _ = filekind.ClassifyFile(path)
		if e.Kind == filekind.Asset {
			e.Hash = fileHash(path)
		}
	}
	return e, nil
}

// copyTree copies a file, symlink or folder on the server, keeping its
// mode or link target, so a paste does not carry the bytes through the app.
func copyTree(from, to string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	source, err := resolveCopyPath(from)
	if err != nil {
		return err
	}
	target, err := resolveCopyPath(to)
	if err != nil {
		return err
	}
	rel, relErr := filepath.Rel(source, target)
	inside := relErr == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
	if relErr == nil && (rel == "." || info.IsDir() && inside) {
		return fmt.Errorf("cannot copy %q to %q: source and destination overlap", from, to)
	}
	return copyTreeNode(from, to, info)
}

// resolveCopyPath resolves symlinks in the existing part of path and keeps
// its nonexistent suffix. This catches a destination below the source even
// when an existing parent of that destination is reached through a symlink.
func resolveCopyPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(abs)
	if parent == abs {
		return abs, nil
	}
	resolvedParent, err := resolveCopyPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolvedParent, filepath.Base(abs)), nil
}

func copyTreeNode(from, to string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(target, to)
	}
	if info.IsDir() {
		if err := os.MkdirAll(to, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(from)
		if err != nil {
			return err
		}
		for _, e := range entries {
			child := filepath.Join(from, e.Name())
			childInfo, err := os.Lstat(child)
			if err != nil {
				return err
			}
			if err := copyTreeNode(child, filepath.Join(to, e.Name()), childInfo); err != nil {
				return err
			}
		}
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// writeFileAtomic replaces path without leaving a half-written file, and
// keeps the mode of a file that was already there.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".mojian-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
