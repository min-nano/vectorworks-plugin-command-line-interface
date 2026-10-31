package main

import (
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// openTerminal は疑似端末の子の側（/dev/pts/N）を開く。
func openTerminal(t *testing.T) *os.File {
	t.Helper()
	parent, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	t.Cleanup(func() { parent.Close() })
	var unlock, number int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, parent.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); errno != 0 {
		t.Skipf("cannot unlock the pseudo terminal: %v", errno)
	}
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, parent.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&number))); errno != 0 {
		t.Skipf("cannot name the pseudo terminal: %v", errno)
	}
	child, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	t.Cleanup(func() { child.Close() })
	return child
}
