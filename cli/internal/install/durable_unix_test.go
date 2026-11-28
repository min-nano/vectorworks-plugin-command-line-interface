//go:build !windows

package install

import (
	"io/fs"
	"syscall"
	"testing"
)

// ディレクトリの Sync を受け付けない（EINVAL・ENOTSUP）ファイルシステムでは、ディレクトリの Sync を成功と
// みなす（install と uninstall が通らなくならないように）。他の誤りは誤りのまま。
func TestUnsupportedDirectorySync(t *testing.T) {
	for _, c := range []struct {
		err     error
		ignored bool
	}{
		{syscall.EINVAL, true},
		{syscall.ENOTSUP, true},
		{&fs.PathError{Op: "sync", Path: "/x", Err: syscall.EINVAL}, true},
		{syscall.EIO, false},
	} {
		if got := ignoreUnsupported(c.err); (got == nil) != c.ignored {
			t.Errorf("%v: %v", c.err, got)
		}
	}
	if ignoreUnsupported(nil) != nil {
		t.Error("nil should stay nil")
	}
}
