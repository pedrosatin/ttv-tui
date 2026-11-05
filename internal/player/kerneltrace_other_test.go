//go:build unix && !darwin

package player

import "testing"

// traceKernel has nothing to log here: only Darwin asks the kernel through sysctl.
func traceKernel(*testing.T) {}
