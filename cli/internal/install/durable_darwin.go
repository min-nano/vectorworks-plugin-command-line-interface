package install

import (
	"os"
	"syscall"
)

// fsyncEntry は、展開したファイルを fsync する（F_FULLFSYNC はしない。syncTree の最後のディレクトリの Sync が
// まとめて書き出す。durable.go）。
func fsyncEntry(file *os.File) error {
	return syscall.Fsync(int(file.Fd()))
}
