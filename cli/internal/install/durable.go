package install

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// 電源が落ちても、名前の変更で公開したファイルが空や半分にならないようにする。APFS と ext4 などは、データの
// 書き出しより名前の変更のメタデータが先に永続しうるので、書いたファイルは閉じる前に Sync し、名前を変えて公開する
// 前にデータを書き終える。POSIX では、作った・名前を変えた項目そのものも、親のディレクトリを Sync しないと残らない
// ことがあるので、それも Sync する。
//
// 守るのは、記録（vw2026-install.json）・uninstall の印・展開したプラグインのファイル。zip そのものは展開した後に
// 消すので Sync しない（途中で電源が落ちても、次の回が組み立ての場所ごと消して取り直す）。

// syncFile と syncDir は、テストが差し替えて、呼ばれる順を確かめる（電源断は再現できないため）。
var (
	syncFile = (*os.File).Sync
	syncDir  = syncDirectory
)

// writeSynced は os.WriteFile と同じに書き、閉じる前に Sync する（writeFile の既定）。
func writeSynced(name string, data []byte, perm fs.FileMode) error {
	file, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	if err == nil {
		err = syncFile(file)
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// syncDirectory は dir の項目（作った・消した・名前を変えたもの）を永続させる。Windows では何もしない（読むだけで
// 開いたディレクトリのハンドルは FlushFileBuffers で断られる。NTFS はメタデータをジャーナルに書くので、ファイルの
// データを Sync してから名前を変えれば足りる）。mac の File.Sync は F_FULLFSYNC。
func syncDirectory(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = file.Sync()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}

// syncTree は root とその下の全てのディレクトリを syncDir する（展開したプラグインを付け替えで公開する前に、
// 項目が全て残るように）。ファイルのデータは、書いたときに Sync している。
func syncTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		return syncDir(path)
	})
}
