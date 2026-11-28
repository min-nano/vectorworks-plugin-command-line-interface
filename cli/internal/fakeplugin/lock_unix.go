//go:build !windows

package fakeplugin

import (
	"errors"
	"os"
	"syscall"
)

// lock はプラグインと同様に flock(LOCK_EX) を取得する。
func lock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}

// waitStateOf は <id>.wait の状態（docs/protocol.md「待機標識」）。取得できれば呼び出し元が
// 解放した・異常終了した。プラグインと同じく、開いて待機せず（O_NONBLOCK）、リンクをたどらない
// （O_NOFOLLOW）。通常のファイルでない・存在しない（名前が長すぎて配置できないものも）ならば存在しない、
// それ以外の理由で開けない・取得を試行できないならば確認できない。
func waitStateOf(path string) waitState {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		var link os.FileInfo
		switch {
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ENAMETOOLONG):
			return waitMissing
		case errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EMLINK):
			// 最後の名前がリンクなら、通常のファイルでない（親のリンクの循環なら確かめられない）。
			if link, err = os.Lstat(path); err == nil && link.Mode()&os.ModeSymlink != 0 {
				return waitMissing
			}
		}
		return waitUnknown
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil {
		return waitUnknown
	} else if !info.Mode().IsRegular() {
		return waitMissing
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	switch {
	case err == nil:
		return waitReleased
	case errors.Is(err, syscall.EWOULDBLOCK):
		return waitHeld
	}
	return waitUnknown
}
