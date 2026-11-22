package app

import (
	"encoding/base64"
	"io"
	"strings"
	"sync"
)

// oscNotification is a desktop notification emitted by a program running
// inside a terminal via OSC 9 (iTerm2/ConEmu), OSC 99 (Kitty), or OSC 777
// (rxvt-unicode/Ghostty).
type oscNotification struct {
	Title string
	Body  string
}

const (
	maxOSCPayloadBytes = 8 << 10
	maxOSC99Pending    = 16
	maxOSC99ChunkBytes = 16 << 10
)

type oscState uint8

const (
	oscGround oscState = iota
	oscEsc
	oscBody
	oscBodyEsc
	oscSkip
	oscSkipEsc
)

type osc99Partial struct {
	title strings.Builder
	body  strings.Builder
}

// oscNotifyScanner scans a terminal byte stream for OSC 9, OSC 99, and
// OSC 777 desktop notification sequences across arbitrary Read boundaries.
type oscNotifyScanner struct {
	state   oscState
	buf     []byte
	pending map[string]*osc99Partial
	order   []string
	onNote  func(oscNotification)
}

func newOSCNotifyScanner(onNote func(oscNotification)) *oscNotifyScanner {
	return &oscNotifyScanner{
		onNote: onNote,
	}
}

// Feed processes bytes from a PTY stream without altering them.
func (s *oscNotifyScanner) Feed(p []byte) {
	if s == nil || s.onNote == nil {
		return
	}
	for _, b := range p {
		switch s.state {
		case oscGround:
			if b == 0x1b {
				s.state = oscEsc
			}
		case oscEsc:
			if b == ']' {
				s.buf = s.buf[:0]
				s.state = oscBody
			} else if b != 0x1b {
				s.state = oscGround
			}
		case oscBody:
			switch b {
			case 0x07: // BEL terminates OSC
				s.dispatch(string(s.buf))
				s.buf = s.buf[:0]
				s.state = oscGround
			case 0x1b:
				s.state = oscBodyEsc
			case 0x18, 0x1a: // CAN / SUB cancels OSC
				s.buf = s.buf[:0]
				s.state = oscGround
			default:
				if len(s.buf) < maxOSCPayloadBytes {
					s.buf = append(s.buf, b)
				} else {
					s.buf = s.buf[:0]
					s.state = oscSkip
				}
			}
		case oscBodyEsc:
			switch b {
			case '\\': // ST (ESC \) terminates OSC
				s.dispatch(string(s.buf))
				s.buf = s.buf[:0]
				s.state = oscGround
			case ']': // New OSC aborts previous
				s.buf = s.buf[:0]
				s.state = oscBody
			case 0x1b:
				// stay in oscBodyEsc? ESC aborts previous OSC and starts a fresh ESC
				s.buf = s.buf[:0]
				s.state = oscEsc
			default:
				s.buf = s.buf[:0]
				s.state = oscGround
			}
		case oscSkip:
			switch b {
			case 0x07, 0x18, 0x1a:
				s.state = oscGround
			case 0x1b:
				s.state = oscSkipEsc
			}
		case oscSkipEsc:
			switch b {
			case '\\':
				s.state = oscGround
			case ']':
				s.buf = s.buf[:0]
				s.state = oscBody
			case 0x1b:
				s.state = oscEsc
			default:
				s.state = oscGround
			}
		}
	}
}

func (s *oscNotifyScanner) dispatch(raw string) {
	semi := strings.IndexByte(raw, ';')
	if semi <= 0 {
		return
	}
	code, rest := raw[:semi], raw[semi+1:]
	switch code {
	case "9":
		// ConEmu / Windows Terminal progress bars use OSC 9;4;state;progress.
		if strings.HasPrefix(rest, "4;") || rest == "4" {
			return
		}
		body := cleanOSCText(rest)
		if body == "" {
			return
		}
		s.onNote(oscNotification{Body: body})
	case "777":
		// OSC 777;notify;<title>;<body>
		sub, after, ok := strings.Cut(rest, ";")
		if !ok || !strings.EqualFold(sub, "notify") {
			return
		}
		title, body, _ := strings.Cut(after, ";")
		title, body = cleanOSCText(title), cleanOSCText(body)
		if title == "" && body == "" {
			return
		}
		s.onNote(oscNotification{Title: title, Body: body})
	case "99":
		s.dispatchOSC99(rest)
	}
}

func (s *oscNotifyScanner) dispatchOSC99(rest string) {
	meta, payload, ok := strings.Cut(rest, ";")
	if !ok {
		return
	}
	id := ""
	part := "title"
	done := true
	encoded := false
	for _, kv := range strings.Split(meta, ":") {
		if kv == "" {
			continue
		}
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		switch k {
		case "i":
			id = v
		case "p":
			if v != "" {
				part = v
			}
		case "d":
			done = v != "0"
		case "e":
			encoded = v == "1"
		}
	}
	if part != "title" && part != "body" {
		// Query/close/icon/etc. parts do not carry notification text.
		return
	}
	if encoded {
		if dec, err := base64.StdEncoding.DecodeString(payload); err == nil {
			payload = string(dec)
		} else if dec, err := base64.RawStdEncoding.DecodeString(payload); err == nil {
			payload = string(dec)
		} else {
			return
		}
	}
	if id == "" && done {
		text := cleanOSCText(payload)
		if text == "" {
			return
		}
		if part == "body" {
			s.onNote(oscNotification{Body: text})
		} else {
			s.onNote(oscNotification{Title: text})
		}
		return
	}
	if s.pending == nil {
		s.pending = map[string]*osc99Partial{}
	}
	entry, exists := s.pending[id]
	if !exists {
		if len(s.order) >= maxOSC99Pending {
			oldest := s.order[0]
			s.order = s.order[1:]
			delete(s.pending, oldest)
		}
		entry = &osc99Partial{}
		s.pending[id] = entry
		s.order = append(s.order, id)
	}
	appendBounded := func(b *strings.Builder, chunk string) {
		if b.Len() >= maxOSC99ChunkBytes {
			return
		}
		rem := maxOSC99ChunkBytes - b.Len()
		if len(chunk) > rem {
			chunk = chunk[:rem]
		}
		b.WriteString(chunk)
	}
	if part == "body" {
		appendBounded(&entry.body, payload)
	} else {
		appendBounded(&entry.title, payload)
	}
	if !done {
		return
	}
	delete(s.pending, id)
	for i, oid := range s.order {
		if oid == id {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	title, body := cleanOSCText(entry.title.String()), cleanOSCText(entry.body.String())
	if title == "" && body == "" {
		return
	}
	s.onNote(oscNotification{Title: title, Body: body})
}

func cleanOSCText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		return string(r[:199]) + "…"
	}
	return s
}

// terminalStream is the surface of *session.Stream passed to terminal.New.
type terminalStream interface {
	io.ReadWriteCloser
	Resize(cols, rows int) error
}

// oscNotifyConn wraps a terminalStream to inspect incoming PTY output for
// OSC 9 / 99 / 777 notifications without modifying the stream.
type oscNotifyConn struct {
	conn    terminalStream
	mu      sync.Mutex
	scanner *oscNotifyScanner
}

func newOSCNotifyConn(conn terminalStream, onNote func(oscNotification)) *oscNotifyConn {
	return &oscNotifyConn{
		conn:    conn,
		scanner: newOSCNotifyScanner(onNote),
	}
}

func (c *oscNotifyConn) Read(p []byte) (int, error) {
	n, err := c.conn.Read(p)
	if n > 0 {
		c.mu.Lock()
		c.scanner.Feed(p[:n])
		c.mu.Unlock()
	}
	return n, err
}

func (c *oscNotifyConn) Write(p []byte) (int, error) { return c.conn.Write(p) }
func (c *oscNotifyConn) Resize(cols, rows int) error { return c.conn.Resize(cols, rows) }
func (c *oscNotifyConn) Close() error                { return c.conn.Close() }
