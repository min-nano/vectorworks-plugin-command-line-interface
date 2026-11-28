//go:build !windows

package install

import (
	"errors"
	"os"
	"syscall"
)

// syncDirectory は dir の項目（作った・消した・名前を変えたもの）を永続させる（mac の File.Sync は
// F_FULLFSYNC）。ディレクトリの Sync を受け付けないファイルシステム（一部のネットワーク・FUSE のボリュームなど）
// では、何もせずに成功とする（そこでは名前の変更の順を保証できないが、install と uninstall が通らなくなるより
// 良い。ファイルのデータの Sync は今の通り失敗とする）。
func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = file.Sync()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return ignoreUnsupported(err)
}

// ignoreUnsupported は、err がディレクトリの Sync を受け付けないことを示すなら（EINVAL・ENOTSUP）、成功とする。
func ignoreUnsupported(err error) error {
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}
