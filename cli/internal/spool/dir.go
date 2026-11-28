package spool

import (
	"os"
	"path/filepath"
)

// SpoolDirName は cli のプラグインのフォルダ（<Plug-Ins>/cli/）の下のスプールの名前
// （docs/protocol.md「スプールの場所」）。
const SpoolDirName = "spool"

// Dir は、実行している実行ファイルの場所からスプールの場所を返す（DirOf）。
func Dir() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	return DirOf(executable), nil
}

// DirOf は、実行ファイル（<Plug-Ins>/cli/bin/vw2026）の場所からスプールの場所
// （<Plug-Ins>/cli/spool）を返す。リンクを解決した実行ファイルのあるフォルダ（bin）の親の下の
// SpoolDirName で、確認も探索もしない。
//
// プラグインも自身のモジュールの場所から導出するので、両側が同一の場所に到達する。環境変数は
// 読み取らない（GUI アプリの Vectorworks と、ssh や別のアプリから起動された CLI とで齟齬が
// 生じないように）。リンクを解決するのは、mac の ~/.local/bin/vw2026 が
// <Plug-Ins>/cli/bin/vw2026 へのリンクであるため。フォルダの外で実行した実行ファイル（zip を
// 展開したのみのもの・手動でビルドしたもの）は、インストールされた cli のスプールではない場所を
// 返す。そこでロックを取得する者は存在しないので、呼び出し元には停止していると見える（場所は
// コマンドが示す）。
//
// リンクを解決できなければ、解決する前のパスから導出する（ResolveLinks）。
func DirOf(executable string) string {
	return filepath.Join(filepath.Dir(filepath.Dir(ResolveLinks(executable))), SpoolDirName)
}

// ResolveLinks は、実行ファイルのパスのリンクを解決したものを返す（mac の ~/.local/bin/vw2026 は
// <Plug-Ins>/cli/bin/vw2026 へのリンク）。解決できなければ（Windows の subst のドライブ・割り当てた
// ネットワークドライブなどでは EvalSymlinks が失敗することがある）解決する前のパスを返す。
// 実行ファイルのパスは使用できるので、場所を決定できずに終了させないため。スプールの場所（DirOf）
// と、install が自身のインストールされた <Plug-Ins> と実行中の自身を特定する場合に、同一の
// 解決方法を使用する（実行ファイルの場所はスプールの場所と同じ方法で導出する）。実在する
// 実行ファイルのパスに使用する。未だ存在しない可能性のある場所（--plugins-dir・<Work>）を
// 解決するのは install の resolvedPath で、存在するところまでの親を解決する。
func ResolveLinks(executable string) string {
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		return resolved
	}
	return executable
}
