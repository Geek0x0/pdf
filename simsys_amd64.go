//go:build amd64
// +build amd64

package pdf

import "golang.org/x/sys/cpu"

// hasAVX2 returns true if the CPU supports AVX2 instructions.
func hasAVX2() bool {
	return cpu.X86.HasAVX2
}
