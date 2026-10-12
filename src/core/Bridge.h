#pragma once

// スプール 1 つ（docs/plugin/bridge.md「スプールの用意」）。SDK に依らない。場所は引数で受ける
// （テストは一時ディレクトリを渡す）。作法の真実は docs/protocol.md。

#include "core/Files.h"

#include <optional>
#include <string>
#include <vector>

namespace VwCli::core {

// defaultSpoolDir は <CLI>/spool を返す（作法「スプールの場所」）。環境変数は読まず、探索
// しない。求められなければ空。
std::optional<std::string> defaultSpoolDir();

class Bridge {
public:
	explicit Bridge(std::string dir) : dir_(std::move(dir)) {}

	const std::string &dir() const { return dir_; }
	bool locked() const { return lock_.held(); }

	// prepare は無ければスプールを 0700 で作る（親も無ければ作る）。持ち主と権限は確かめない。
	bool prepare(std::string &error);

	// lock は bridge.lock を掴む。掴めたら壊すまで放さない。
	files::Lock::Result lock(std::string &error);

	// sweep は前の回の残骸を消す。ロックを掴んだ直後に 1 度だけ呼ぶ。消すのは要求・応答・待つ印・
	// 書きかけのうち、待つ印を掴める・無い id のもの（id はファイル名の最初の . より前）。
	void sweep();

	// poll は要求の id を、ファイル名の昇順で返す。綴りを満たさない id の要求は返さない（読まず・
	// 消さず・応えない）。
	std::vector<std::string> poll() const;

	// take は要求を読んでから消す。上限を超える分は読まない（大きすぎることだけ分かればよい）。
	// 読めない（無くなっていた）・消せない（ほかのプロセスが一時的に掴んでいる）なら空で、
	// その要求は実行しない（消さずに実行すると、次の回にまた実行してしまう）。
	std::optional<std::string> take(const std::string &id);

	// waitState は呼ぶ側の待つ印を掴めるか試す（作法「待つ印」）。
	files::WaitState waitState(const std::string &id) const;
	void removeWait(const std::string &id);

	// reply は応答を書きかけ（.tmp）として書いてから、rename で公開する。
	bool reply(const std::string &id, const std::string &body);

private:
	std::string path(const std::string &name) const { return files::join(dir_, name); }

	std::string dir_;
	files::Lock lock_;
};

} // namespace VwCli::core
