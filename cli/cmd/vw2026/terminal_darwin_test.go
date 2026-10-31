package main

import (
	"bytes"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openTerminal は疑似端末の子の側（/dev/ttys…）を開く（親の側の /dev/ptmx は、mac では端末の設定を読めない）。
func openTerminal(t *testing.T) *os.File {
	t.Helper()
	parent, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	t.Cleanup(func() { parent.Close() })
	var name [128]byte
	for _, request := range []uintptr{syscall.TIOCPTYGRANT, syscall.TIOCPTYUNLK, syscall.TIOCPTYGNAME} {
		var arg uintptr
		if request == syscall.TIOCPTYGNAME {
			arg = uintptr(unsafe.Pointer(&name[0]))
		}
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, parent.Fd(), request, arg); errno != 0 {
			t.Skipf("cannot open the pseudo terminal: %v", errno)
		}
	}
	child, err := os.OpenFile(string(name[:bytes.IndexByte(name[:], 0)]), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	t.Cleanup(func() { child.Close() })
	return child
}
