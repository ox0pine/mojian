//go:build !darwin && !linux && !windows

package acp

import "os/exec"

// processTree retains direct-process termination on other platforms.
type processTree struct {
	cmd *exec.Cmd
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	return &processTree{cmd: cmd}, nil
}

func (p *processTree) start() error { return p.cmd.Start() }

func (p *processTree) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *processTree) close() {}
