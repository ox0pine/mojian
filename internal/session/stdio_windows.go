package session

import "errors"

// Bridge is not needed on Windows: the app reaches a remote server
// through ssh, and the bridge runs on the host's side.
func Bridge() error { return errors.New("session: the stdio bridge runs on macOS and Linux") }
