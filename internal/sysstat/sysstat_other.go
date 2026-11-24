//go:build !darwin && !linux && !windows

package sysstat

import "context"

// Sampler is a no-op stub on unsupported platforms.
type Sampler struct{}

// NewSampler returns a stub sampler.
func NewSampler() *Sampler { return &Sampler{} }

// Sample returns an empty snapshot on unsupported platforms.
func (s *Sampler) Sample(_ context.Context, _ Options) Snapshot {
	return EmptySnapshot()
}

func detectCapabilities() Capabilities {
	return Capabilities{Detail: "System telemetry unavailable on this platform"}
}
