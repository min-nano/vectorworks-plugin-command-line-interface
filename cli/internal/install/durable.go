package install

import (
	"io/fs"
	"os"
	"path/filepath"
)

// 電源が落ちても、名前の変更で公開したファイルが空や半分にならないようにする。APFS と ext4 などは、データの
// 書き出しより名前の変更のメタデータが先に永続しうるので、書いたファイルは閉じる前に Sync し、名前を変えて公開する
// 前にデータを書き終える。POSIX では、作った・名前を変えた項目そのものも、親のディレクトリを Sync しないと残らない
// ことがあるので、それも Sync する。
//
// 守るのは、記録（vw2026-install.json）・uninstall の印・展開したプラグインのファイル。zip そのものは展開した後に
// 消すので Sync しない（途中で電源が落ちても、次の回が組み立ての場所ごと消して取り直す）。

// syncFile・syncEntry・syncDir は、テストが差し替えて、呼ばれる順を確かめる（電源断は再現できないため）。
//
// mac の File.Sync（F_FULLFSYNC）は装置のキャッシュごと書き出すので重い。展開したファイル（数が多い）は syncEntry
// （mac は fsync だけ）にし、その後の syncTree のディレクトリの Sync（F_FULLFSYNC）でまとめて書き出す。付け替えは
// 最後の F_FULLFSYNC の後なので、どのファイルのデータも名前の変更より先に残る。
var (
	syncFile  = (*os.File).Sync
	syncEntry = fsyncEntry
	syncDir   = syncDirectory
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

// mkdirAllSynced は mkdirAll と同じに作り、作った段の項目も親のディレクトリを Sync して残す（作った直後の
// フォルダへ移したものが、電源が落ちた後にフォルダごと失われないように。中身は、移す側が Sync する）。
func mkdirAllSynced(path string, perm fs.FileMode) error {
	path = filepath.Clean(path)
	// 作る前に、無い段のうち一番上を求める（調べられない段は、無いものとして親から Sync する。根は在る）。
	first := ""
	for current := path; filepath.Dir(current) != current; current = filepath.Dir(current) {
		if _, err := os.Lstat(current); err == nil {
			break
		}
		first = current
	}
	if err := mkdirAll(path, perm); err != nil {
		return err
	}
	if first == "" {
		return nil
	}
	for current := path; ; current = filepath.Dir(current) {
		if err := syncDir(filepath.Dir(current)); err != nil {
			return err
		}
		if current == first {
			return nil
		}
	}
}

// syncTree は root とその下の全てのディレクトリを syncDir する（展開したプラグインを付け替えで公開する前に、
// 項目が全て残るように）。ファイルのデータは、書いたときに syncEntry している（mac では、ここの F_FULLFSYNC が
// 装置のキャッシュから書き出す）。
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
