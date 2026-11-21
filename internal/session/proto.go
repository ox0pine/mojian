// Package session keeps terminal sessions in a server process of the app
// itself, so that quitting Mojian detaches from them instead of ending
// them.
package session

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"time"

	"mojian/internal/filekind"
)

// ProtocolVersion changes when the wire protocol does. Version 4 framed
// every message and multiplexed control requests and terminal streams
// over one connection. Version 5 adds chunked control responses. Version 6
// adds project search, session working directories, and remote ACP tasks.
const ProtocolVersion = 6

// The frame types. A connection starts with a frameHello each way; after
// that the client sends requests, opens streams and writes data, and the
// server answers and pushes both.
const (
	frameHello        byte = 1
	frameRequest      byte = 2
	frameResponse     byte = 3
	frameOpen         byte = 4
	frameData         byte = 5
	frameClose        byte = 6
	frameError        byte = 7
	frameResponsePart byte = 8
)

// frameHeaderSize is the u32 payload length, u8 type and u32 stream of
// every frame, all big-endian.
const frameHeaderSize = 9

// maxFrame bounds one frame's payload, so a peer cannot make the server
// or the app allocate without bound. maxResponse bounds the reassembled
// control response; terminal streams are not subject to either total.
const (
	maxFrame    = 64 << 20
	maxResponse = 256 << 20

	// A response fragment carries an int64 request ID and uint32 offset
	// and total sizes before its slice of the response's JSON bytes.
	responsePartHeaderSize = 16
)

type frame struct {
	typ     byte
	stream  uint32
	payload []byte
}

// writeFrame writes one frame without copying its payload.
func writeFrame(w io.Writer, f frame) error {
	var hdr [frameHeaderSize]byte
	binary.BigEndian.PutUint32(hdr[0:4], uint32(len(f.payload)))
	hdr[4] = f.typ
	binary.BigEndian.PutUint32(hdr[5:9], f.stream)
	if err := writeAll(w, hdr[:]); err != nil {
		return err
	}
	return writeAll(w, f.payload)
}

// writeAll writes p, retrying the short writes a socket may return.
func writeAll(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, err := w.Write(p)
		p = p[n:]
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

// readFrame reads one frame, refusing a payload longer than maxFrame.
func readFrame(r *bufio.Reader) (frame, error) {
	var hdr [frameHeaderSize]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[0:4])
	if n > maxFrame {
		return frame{}, errors.New("session: frame too long")
	}
	f := frame{typ: hdr[4], stream: binary.BigEndian.Uint32(hdr[5:9])}
	if n > 0 {
		f.payload = make([]byte, n)
		if _, err := io.ReadFull(r, f.payload); err != nil {
			return frame{}, err
		}
	}
	return f, nil
}

// writeResponse sends a response in bounded frames. Small responses keep
// the version 4 representation; larger ones carry their JSON in ordered
// fragments with the request ID in every frame.
func writeResponse(m *mux, res Response) error {
	b, err := json.Marshal(res)
	if err != nil {
		b, _ = json.Marshal(Response{ID: res.ID, Error: "session: could not encode response"})
	}
	if len(b) > maxResponse {
		b, _ = json.Marshal(Response{
			ID: res.ID, Error: fmt.Sprintf("session: response is larger than %d MiB", maxResponse>>20),
		})
	}
	if len(b) <= maxFrame {
		return m.writeControl(frameResponse, b)
	}
	for offset := 0; offset < len(b); {
		n := min(len(b)-offset, maxFrame-responsePartHeaderSize)
		payload := make([]byte, responsePartHeaderSize+n)
		binary.BigEndian.PutUint64(payload[0:8], uint64(res.ID))
		binary.BigEndian.PutUint32(payload[8:12], uint32(offset))
		binary.BigEndian.PutUint32(payload[12:16], uint32(len(b)))
		copy(payload[responsePartHeaderSize:], b[offset:offset+n])
		if err := m.writeControl(frameResponsePart, payload); err != nil {
			return err
		}
		offset += n
	}
	return nil
}

// ProtocolError says a running server speaks another protocol version.
type ProtocolError struct{ Server, Client int }

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("session: server protocol %d, app needs %d", e.Server, e.Client)
}

// Request is one line of a control connection.
type Request struct {
	ID     int64          `json:"id"`
	Op     string         `json:"op"`
	SID    string         `json:"sid,omitempty"`
	Cols   int            `json:"cols,omitempty"`
	Rows   int            `json:"rows,omitempty"`
	Create *CreateOptions `json:"create,omitempty"`
	Path   string         `json:"path,omitempty"`
	To     string         `json:"to,omitempty"`
	// Fresh asks listTree to bypass the server's listing cache.
	Fresh bool     `json:"fresh,omitempty"`
	Data  []byte   `json:"data,omitempty"`
	Dir   string   `json:"dir,omitempty"`
	Args  []string `json:"args,omitempty"`
	Env   []string `json:"env,omitempty"`
	// Timeout is how long a command may run, in milliseconds.
	Timeout int            `json:"timeout,omitempty"`
	Search  *SearchOptions `json:"search,omitempty"`
}

// SearchOptions describes a project-wide text search.
type SearchOptions struct {
	Query         string `json:"query"`
	CaseSensitive bool   `json:"caseSensitive,omitempty"`
	Regex         bool   `json:"regex,omitempty"`
	Limit         int    `json:"limit,omitempty"`
}

// SearchMatch is one matching line inside a project file.
type SearchMatch struct {
	Rel  string `json:"rel"`
	Line int    `json:"line"` // 1-based line number
	Col  int    `json:"col"`  // 0-based byte offset within the original line
	End  int    `json:"end"`  // 0-based byte end offset within the original line
	Text string `json:"text"` // trimmed line excerpt
}

// SearchResult is the bounded result of a project search.
type SearchResult struct {
	Matches   []SearchMatch `json:"matches,omitempty"`
	Files     int           `json:"files,omitempty"`
	Truncated bool          `json:"truncated,omitempty"`
}

// Response answers a Request with the same ID.
type Response struct {
	ID    int64           `json:"id"`
	Error string          `json:"error,omitempty"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// CreateOptions starts a session.
type CreateOptions struct {
	Command []string `json:"command,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Env     []string `json:"env,omitempty"`
	Cols    int      `json:"cols,omitempty"`
	Rows    int      `json:"rows,omitempty"`
}

// SessionInfo describes a session.
type SessionInfo struct {
	ID       string    `json:"id"`
	PID      int       `json:"pid"`
	Shell    string    `json:"shell"`
	Dir      string    `json:"dir,omitempty"`
	Created  time.Time `json:"created"`
	Cols     int       `json:"cols"`
	Rows     int       `json:"rows"`
	Title    string    `json:"title,omitempty"`
	Exited   bool      `json:"exited"`
	ExitCode int       `json:"exitCode"`
}

// FileEntry describes a path in a remote project.
type FileEntry struct {
	Rel  string        `json:"rel"`
	Dir  bool          `json:"dir,omitempty"`
	Size int64         `json:"size,omitempty"`
	Mod  time.Time     `json:"mod,omitempty"`
	Kind filekind.Kind `json:"kind,omitempty"`
	// Hash is the content hash of an asset, so a viewer cache is keyed by
	// what the file contains rather than by its size and timestamp.
	Hash string `json:"hash,omitempty"`
}

// ExecResult is what a command run on the server left.
type ExecResult struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Code   int    `json:"code,omitempty"`
}

// Hello is the first frame each way: the client's says which protocol it
// wants, the server's answers with the running version.
type Hello struct {
	Version int       `json:"version"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	// Role is "client" or "server", so a log or a probe can tell them
	// apart.
	Role string `json:"role,omitempty"`
}

// Dir returns the directory of the server's socket and log.
func Dir() string {
	if d := os.Getenv("MOJIAN_DIR"); d != "" {
		return d
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "Mojian")
}

// RuntimeDir is where the server keeps its socket and lock. A remote
// server sets MOJIAN_RUNTIME_DIR to a short path in the user's runtime
// directory, which the bridge on the host can reach without the socket
// living in the home directory; locally it is the data directory.
func RuntimeDir() string {
	if d := os.Getenv("MOJIAN_RUNTIME_DIR"); d != "" {
		return d
	}
	return Dir()
}

// SocketPathIn returns the socket a server started with dir would bind,
// so a remote bridge and the server agree even when the path is long.
func SocketPathIn(dir string, uid ...int) string {
	p := filepath.Join(dir, "sessions.sock")
	if len(p) < 100 {
		return p
	}
	id := os.Getuid()
	if len(uid) > 0 {
		id = uid[0]
	}
	h := fnv.New32a()
	h.Write([]byte(p))
	name := fmt.Sprintf("mojian-%x.sock", h.Sum32())
	if id >= 0 {
		name = fmt.Sprintf("mojian-%d-%x.sock", id, h.Sum32())
	}
	return filepath.Join(os.TempDir(), name)
}
