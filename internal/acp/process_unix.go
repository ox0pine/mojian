//go:build darwin || linux

package acp

import (
	"os/exec"
	"syscall"
)

// processTree owns the agent's process group. Tool children inherit the
// group unless they deliberately start a new session of their own.
type processTree struct {
	cmd *exec.Cmd
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processTree{cmd: cmd}, nil
}

func (p *processTree) start() error { return p.cmd.Start() }

func (p *processTree) kill() {
	if p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}

func (p *processTree) close() {}
