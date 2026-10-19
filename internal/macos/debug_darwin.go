//go:build darwin

package macos

import (
	"errors"

	"github.com/ebitengine/purego"
)

// ptDenyAttach is ptrace's PT_DENY_ATTACH.
const ptDenyAttach = 31

// DenyDebuggers keeps debuggers from attaching to this process from now
// on, and from reading its memory through ptrace: a process that a
// debugger already traces exits.
func DenyDebuggers() error {
	sys, err := lib("/usr/lib/libSystem.B.dylib")
	if err != nil {
		return err
	}
	var ptrace func(request, pid int32, addr uintptr, data int32) int32
	purego.RegisterLibFunc(&ptrace, sys, "ptrace")
	if ptrace(ptDenyAttach, 0, 0, 0) != 0 {
		return errors.New("macos: ptrace refused PT_DENY_ATTACH")
	}
	return nil
}
