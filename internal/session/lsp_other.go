//go:build windows

package session

import (
	"encoding/json"
	"errors"
	"io"
)

func (s *Server) serveLSP(ms *muxStream, o openRequest) {
	_ = ms.Close()
}

// StartLSP opens a bidirectional stdio stream to a language server command
// running on a remote Linux session server in dir (usable from Windows too).
func (c *Client) StartLSP(dir string, args []string) (io.ReadWriteCloser, error) {
	if c == nil || c.mux == nil {
		return nil, errors.New("session: not connected")
	}
	id := c.mux.newStreamID()
	ms := c.mux.openStream(id)
	b, _ := json.Marshal(openRequest{Kind: "lsp", Dir: dir, Args: args})
	if err := c.mux.writeFrame(frame{typ: frameOpen, stream: id, payload: b}); err != nil {
		ms.finish(err)
		return nil, err
	}
	return ms, nil
}
