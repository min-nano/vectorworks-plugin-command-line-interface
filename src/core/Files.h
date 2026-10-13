#pragma once

// スプールに使う OS の呼び出し（docs/protocol.md「ロック」「待つ印」）。SDK に依らない。
// 実装は OS ごとのファイル（FilesPosix.cpp・FilesWindows.cpp）。
//
// パスは UTF-8。開くファイルは子プロセスに受け継がせない（POSIX は O_CLOEXEC、Windows は
// 継承できないハンドル）。受け継がれると、Vectorworks が終わってもロックが放されないため。
//
// 開いて待たない（作法「プラグイン側の義務」）。開くのは通常のファイルだけで、リンクは
// たどらない（POSIX は O_NONBLOCK と O_NOFOLLOW、Windows は FILE_FLAG_OPEN_REPARSE_POINT）。
// FIFO・デバイス・名前付きパイプを置かれても、メインスレッドを止めない。Windows は reparse
// point（OneDrive などのクラウドのファイルを含む）と、中身がローカルにないファイルも断る。
//
// Windows は \\?\ を付けて開く（CON・NUL などの名前をデバイスとして開かない）。そのため Windows の
// パスは絶対パス（C:\… か UNC）に限り、それ以外は開けない。

#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core::files {

// join は dir と name を OS の区切りでつなぐ。
std::string join(const std::string &dir, std::string_view name);

// appRoot は <CLI> の親を返す（mac は利用者の passwd の項目のホームの Library/Application
// Support、Windows は SHGetKnownFolderPath(FOLDERID_LocalAppData)。環境変数は読まない。作法
// 「スプールの場所」）。求められなければ空。
std::optional<std::string> appRoot();

// makeDirectories は path を作る（親も無ければ作る。POSIX は 0700）。既にあれば何もしない。
// 持ち主と権限は確かめない。
bool makeDirectories(const std::string &path, std::string &error);

// list はディレクトリの中の名前を返す（. と .. を除く）。読めなければ空。
std::optional<std::vector<std::string>> list(const std::string &dir);

// read はファイルの先頭から limit バイトまでを読む。開けない・通常のファイルでない・読めなければ
// 空。
std::optional<std::string> read(const std::string &path, std::size_t limit);

// write はファイルを消してから新しく作り、data を書く（POSIX は 0600）。あったものがリンク
// （シンボリックリンクでも hard link でも）でも、リンクの先は変えない。
bool write(const std::string &path, std::string_view data);

// rename は from を to へ移す（to があれば置き換える）。
bool rename(const std::string &from, const std::string &to);

// remove はファイルを消す。消せたときだけ真。
bool remove(const std::string &path);

// WaitState は待つ印（<id>.wait）の状態（作法「待つ印」）。
enum class WaitState {
	// 呼ぶ側が掴んでいる（待っている）
	Held,
	// 掴めた（呼ぶ側が放した・死んだ）
	Released,
	// 無い（開けない・通常のファイルでないときも。実行しない側に倒す）
	Missing,
};

// probe は待つ印を掴めるか試し、掴めたらすぐ放す。
WaitState probe(const std::string &path);

// Lock はファイルを排他で掴み続ける（bridge.lock。作法「ロック」）。壊すと放す。
class Lock {
public:
	enum class Result {
		// 掴んだ
		Taken,
		// ほかが掴んでいる
		Busy,
		// 開けない
		Failed,
	};

	Lock() = default;
	~Lock();
	Lock(const Lock &) = delete;
	Lock &operator=(const Lock &) = delete;

	// take は path を作って（あれば開いて）排他で掴む。Failed なら error に理由を書く。
	Result take(const std::string &path, std::string &error);
	bool held() const { return handle_ != kNone; }
	void release();

private:
	// POSIX はファイル記述子、Windows は HANDLE（INVALID_HANDLE_VALUE は -1）。
	static constexpr std::intptr_t kNone = -1;
	std::intptr_t handle_ = kNone;
};

}
