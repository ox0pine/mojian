//go:build !darwin && !linux

package detach

// FromTerminal does nothing where Mojian has no native window and no
// session to leave; the browser build keeps its launcher.
func FromTerminal() bool { return false }
