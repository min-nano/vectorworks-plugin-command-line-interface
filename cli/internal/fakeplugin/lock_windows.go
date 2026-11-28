//go:build windows

package fakeplugin

import (
	"errors"
	"syscall"
)

// lock はプラグインと同じく共有なし（share mode 0）でロックファイルを開いたままにする。
func lock(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return func() { syscall.CloseHandle(handle) }, nil
}

// errorSharingViolation は ERROR_SHARING_VIOLATION。
const errorSharingViolation = syscall.Errno(32)

// Windows の誤りの番号（ERROR_INVALID_NAME・ERROR_FILENAME_EXCED_RANGE）。
const (
	errorInvalidName        = syscall.Errno(123)
	errorFilenameExcedRange = syscall.Errno(206)
)

// waitStateOf は <id>.wait の状態（docs/protocol.md「待機標識」）。共有なしで開ければ呼ぶ側が
// 放した・死んだ。共有違反なら呼ぶ側が待っている。無い（名前が長すぎて置けない・名前に使えないものと、
// フォルダも）なら無い、それ以外の理由で開けなければ確かめられない（プラグインと同じ分け方）。
func waitStateOf(path string) waitState {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return waitMissing
	}
	handle, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	switch {
	case err == nil:
		syscall.CloseHandle(handle)
		return waitReleased
	case errors.Is(err, errorSharingViolation):
		return waitHeld
	case errors.Is(err, syscall.ERROR_FILE_NOT_FOUND), errors.Is(err, syscall.ERROR_PATH_NOT_FOUND),
		errors.Is(err, errorInvalidName), errors.Is(err, errorFilenameExcedRange):
		return waitMissing
	case errors.Is(err, syscall.ERROR_ACCESS_DENIED):
		// フォルダは無いものとする（FILE_FLAG_BACKUP_SEMANTICS なしには開けない）。
		if attributes, err := syscall.GetFileAttributes(name); err == nil &&
			attributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 {
			return waitMissing
		}
	}
	return waitUnknown
}
