//go:build !windows

package fakeplugin

import (
	"errors"
	"os"
	"syscall"
)

// lock はプラグインと同じく flock(LOCK_EX) を掴む。
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

// waitStateOf は <id>.wait の状態（docs/protocol.md「待つ印」）。掴めれば呼ぶ側が放した・死んだ。プラグインと
// 同じく、開いて待たず（O_NONBLOCK）、リンクをたどらない（O_NOFOLLOW）。通常のファイルでない・無い（名前が長すぎて
// 置けないものも）なら無い、それ以外の理由で開けない・試せないなら確かめられない。
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
