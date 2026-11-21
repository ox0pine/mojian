//go:build windows

package acp

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree keeps the agent and every descendant in a kill-on-close job.
type processTree struct {
	cmd *exec.Cmd

	mu  sync.Mutex
	job windows.Handle
}

func newProcessTree(cmd *exec.Cmd) (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("create agent job object: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, fmt.Errorf("configure agent job object: %w", err)
	}
	return &processTree{cmd: cmd, job: job}, nil
}

func (p *processTree) start() error {
	if err := p.cmd.Start(); err != nil {
		p.close()
		return err
	}
	// WithHandle pins the process identity while it is assigned; opening a
	// fresh handle by PID could assign an unrelated process after a fast exit.
	var err error
	err = p.cmd.Process.WithHandle(func(handle uintptr) {
		err = windows.AssignProcessToJobObject(p.job, windows.Handle(handle))
	})
	if err != nil {
		p.close()
		_ = p.cmd.Process.Kill()
		_ = p.cmd.Wait()
		return fmt.Errorf("assign agent to job object: %w", err)
	}
	return nil
}

func (p *processTree) kill() {
	p.mu.Lock()
	job := p.job
	p.job = 0
	p.mu.Unlock()
	if job != 0 {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(job)
		return
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *processTree) close() {
	p.mu.Lock()
	job := p.job
	p.job = 0
	p.mu.Unlock()
	if job != 0 {
		_ = windows.CloseHandle(job)
	}
}
