package spool

import (
	"os"
	"path/filepath"
)

// SpoolDirName は cli のプラグインのフォルダ（<Plug-Ins>/cli/）の下のスプールの名前
// （docs/protocol.md「スプールの場所」）。
const SpoolDirName = "spool"

// Dir は、走らせている実行ファイルの場所からスプールの場所を返す（DirOf）。
func Dir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return DirOf(executable), nil
}

// DirOf は、実行ファイル（<Plug-Ins>/cli/bin/vw2026）の場所からスプールの場所
// （<Plug-Ins>/cli/spool）を返す。リンクを解いた実行ファイルのあるフォルダ（bin）の親の下の
// SpoolDirName で、確かめも探索もしない。
//
// プラグインも自分のモジュールの場所から求めるので、両側が同じ場所にたどり着く。環境変数は
// 読まない（GUI アプリの Vectorworks と、ssh や別のアプリから起動された CLI とで食い違わない
// ように）。リンクを解くのは、mac の ~/.local/bin/vw2026 が <Plug-Ins>/cli/bin/vw2026 への
// リンクだから。フォルダの外で動かした実行ファイル（zip を展開しただけのもの・手でビルドした
// もの）は、入っている cli のスプールではない場所を返す。そこにロックを掴む者は居ないので、
// 呼ぶ側には止まっていると見える（場所はコマンドが示す）。
//
// リンクを解けなければ（Windows の subst のドライブ・割り当てたネットワークドライブなどでは
// EvalSymlinks が失敗することがある）、解く前のパスから求める。実行ファイルのパスは使えるので、
// スプールを決められずに終わらせないため。
func DirOf(executable string) string {
	path := executable
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		path = resolved
	}
	return filepath.Join(filepath.Dir(filepath.Dir(path)), SpoolDirName)
}
