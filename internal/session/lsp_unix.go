//go:build darwin || linux

package session

import (
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"syscall"
)

// serveLSP spawns a language server process in o.Dir and bridges its stdio
// to the multiplexed stream ms until either side closes.
func (s *Server) serveLSP(ms *muxStream, o openRequest) {
	defer ms.Close()
	if len(o.Args) == 0 {
		return
	}
	bin, err := exec.LookPath(o.Args[0])
	if err != nil {
		return
	}
	cmd := exec.Command(bin, o.Args[1:]...)
	cmd.Dir = o.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return
	}
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(stdin, ms)
		_ = stdin.Close()
		close(done)
	}()
	_, _ = io.Copy(ms, stdout)
	_ = stdout.Close()
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	_ = cmd.Wait()
	_ = ms.Close()
	<-done
}

// StartLSP opens a bidirectional stdio stream to a language server command
// running on the session server in dir.
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
