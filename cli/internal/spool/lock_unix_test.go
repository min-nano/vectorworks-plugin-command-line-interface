//go:build !windows

package spool_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/fakeplugin"
	. "github.com/min-nano/vectorworks-plugin-command-line-interface/cli/internal/spool"
)

// ロックが FIFO・リンクなら、開いて待たずに、調べられないという誤りを返す。Open は止まっていると判定し、理由を
// Reason に載せる（FIFO を O_NONBLOCK なしで開くと、書き手を待って status も call も止まる）。
func TestLockThatIsNotARegularFile(t *testing.T) {
	for _, kind := range []string{"fifo", "link"} {
		t.Run(kind, func(t *testing.T) {
			dir := fakeplugin.NewDir(t)
			lock := filepath.Join(dir, LockFile)
			switch kind {
			case "fifo":
				if err := syscall.Mkfifo(lock, 0o600); err != nil {
					t.Skip("cannot make a FIFO:", err)
				}
			case "link":
				target := filepath.Join(t.TempDir(), "real.lock")
				if err := os.WriteFile(target, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, lock); err != nil {
					t.Fatal(err)
				}
			}
			done := make(chan *Bridge, 1)
			go func() { done <- Open(dir) }()
			select {
			case bridge := <-done:
				if bridge.Running || !strings.HasPrefix(bridge.Reason, "cannot check the lock:") {
					t.Fatalf("%+v", bridge)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Open waited on the lock")
			}
			if held, err := LockHeld(dir); held || err == nil {
				t.Fatal(held, err)
			}
		})
	}
}
