//go:build darwin || linux

package session

import (
	"errors"
	"io"
	"os"
	"time"
)

// Bridge connects stdio to the server's socket, starting a detached
// server when none runs. An SSH client runs it as
// `mojian-server stdio`, so its byte stream carries the v4 frames to the
// daemon without any forwarded socket.
func Bridge() error { return bridge(os.Stdin, os.Stdout) }

func bridge(in io.Reader, out io.Writer) error {
	sock := SocketPath()
	if !ping(sock) {
		if err := Spawn(); err != nil {
			return err
		}
		deadline := time.Now().Add(5 * time.Second)
		for !ping(sock) {
			if time.Now().After(deadline) {
				return errors.New("session: the server did not start")
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	conn, err := dial(sock)
	if err != nil {
		return err
	}
	defer conn.Close()
	// The client's stdin ends when it closes the connection; half-close
	// so the server still finishes what it already queued.
	go func() {
		_, _ = io.Copy(conn, in)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
			return
		}
		_ = conn.Close()
	}()
	_, err = io.Copy(out, conn)
	return err
}

// ping reports whether a server answers at sock.
func ping(sock string) bool {
	conn, err := dialTimeout(sock, time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
