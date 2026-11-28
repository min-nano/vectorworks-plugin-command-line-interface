#pragma once

// 1 つのスプール（docs/plugin/bridge.md「スプールの準備」）。SDK に依存しない。場所は引数で
// 受け取る（テストは一時ディレクトリを渡す）。通信規則の真実は docs/protocol.md。

#include "core/Files.h"

#include <optional>
#include <string>
#include <vector>

namespace VwCli::core {

// spoolDirOf は、モジュールのパスからスプールの場所を返す（通信規則「スプールの場所」）。パスの
// 綴りのみから決定し、ファイルを参照しない（探索しない）。
//
// - mac のバンドルの中の実行ファイル（<X>.vwlibrary/Contents/MacOS/<実行ファイル>。
//   <Plug-Ins>/cli/cli.vwlibrary/Contents/MacOS/cli）ならば、バンドルのあるフォルダの下の spool。
// - それ以外（Windows の <Plug-Ins>\cli\cli.vlb・テストの実行ファイル）は、モジュールのある
//   フォルダの下の spool。パスの途中に .vwlibrary で終わるフォルダが存在しても遡らない。
//
// 区切りは / と、Windows では \ も。フォルダを導出できない（区切りが無い）場合は空。
std::optional<std::string> spoolDirOf(const std::string &modulePath);

// defaultSpoolDir は、このコードを含むモジュールの場所（files::modulePathOf）から spoolDirOf で
// スプールの場所を導出する。環境変数は読み取らない。導出できなければ空（POSIX で作業ディレクトリを
// 取得できなければ例外。受付処理の周期処理が捕捉し、次の周期処理で再試行する）。
std::optional<std::string> defaultSpoolDir();

class Bridge {
public:
	explicit Bridge(std::string dir) : dir_(std::move(dir)) {}

	bool locked() const { return lock_.held(); }

	// prepare はスプールが存在しなければ作成する（親も存在しなければ作成する。POSIX は 0700）。
	// 所有者と権限は確認しない。
	bool prepare(std::string &error);

	// lock は bridge.lock を取得する。取得できた場合は破棄するまで解放しない。
	files::Lock::Result lock(std::string &error);

	// sweep は前回の残存物を削除する。ロックを取得した直後に 1 度のみ呼ぶ。削除するのは要求・応答・
	// 待機標識・書き込み途中のファイルの内、待機標識を取得できる・存在しない id のもの（id は
	// ファイル名の最初の . より前）。
	void sweep();

	// poll は要求の id を、ファイル名の昇順で返す。綴りを満たさない id の要求は返さない
	// （読み取らず・削除せず・応答しない）。
	std::vector<std::string> poll() const;

	// take は要求を読み取ってから削除する。上限を超える部分は読み取らない（過大であることのみ
	// 判明すればよい）。読み取れない（消失していた）・削除できない（他のプロセスが一時的に保持して
	// いる）場合は空で、その要求は実行しない（削除せずに実行すると、次回に再度実行することに
	// なる）。
	std::optional<std::string> take(const std::string &id);

	// waitState は呼び出し元の待機標識を取得できるか試行する（通信規則「待機標識」）。
	files::WaitState waitState(const std::string &id) const;
	void removeWait(const std::string &id);

	// reply は応答を書き込み途中のファイル（.tmp）として書き込んでから、rename で公開する。
	bool reply(const std::string &id, const std::string &body);

private:
	std::string path(const std::string &name) const { return files::join(dir_, name); }

	std::string dir_;
	files::Lock lock_;
};

}
