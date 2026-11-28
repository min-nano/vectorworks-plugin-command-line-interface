#pragma once

// スプールに使用する OS の呼び出し（docs/protocol.md「ロック」「待機標識」）。SDK に依存しない。
// 実装は OS ごとのファイル（FilesPosix.cpp・FilesWindows.cpp）。OS に依存しない部分（isSeparator・
// join・Lock::~Lock）は Files.cpp に配置し、OS ごとの相違は kSeparator と isSeparator の中で
// 区別する。
//
// パスは UTF-8。開くファイルは子プロセスに継承させない（POSIX は O_CLOEXEC、Windows は
// 継承できないハンドル）。継承されると、Vectorworks が終了してもロックが解放されないため。
//
// 開く際に待機しない（通信規則「プラグイン側の義務」）。開くのは通常のファイルのみで、リンクは
// 辿らない（POSIX は O_NONBLOCK と O_NOFOLLOW、Windows は FILE_FLAG_OPEN_REPARSE_POINT）。
// FIFO・デバイス・名前付きパイプを配置されても、メインスレッドを停止させない。Windows は reparse
// point（OneDrive などのクラウドのファイルを含む）と、内容がローカルに存在しないファイルも
// 拒否する。
//
// Windows は \\?\ を付けて開く（CON・NUL などの名前をデバイスとして開かない）。そのため Windows の
// パスは絶対パス（C:\… か UNC）に限定し、それ以外は開けない。

#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core::files {

#ifdef _WIN32
// kSeparator はパスを連結する際に追加する区切り（isSeparator は / も区切りとして解釈する）。
inline constexpr char kSeparator = '\\';
#else
// kSeparator はパスを連結する際に追加する区切り。
inline constexpr char kSeparator = '/';
#endif

// isSeparator は c がパスの区切りか（POSIX は /、Windows は \ と /）。
bool isSeparator(char c);

// join は dir と name を OS の区切り（kSeparator）で連結する（dir が区切りで終わっていれば
// 追加しない）。
std::string join(const std::string &dir, std::string_view name);

// modulePathOf は、address を含むモジュール（プラグインでは cli.vlb か、cli.vwlibrary の中の
// 実行ファイル。テストではテストの実行ファイル）のパスを返す（POSIX は dladdr のパスを absolute で
// 絶対パスにしたもの、Windows は GetModuleHandleExW と GetModuleFileNameW の返すパス。リンクは
// 解決しない。環境変数は読み取らない。通信規則「スプールの場所」）。address がいずれのモジュールの
// 中にも存在しない・取得できなければ空（POSIX は absolute と同様に例外を送出することがある）。
std::optional<std::string> modulePathOf(const void *address);

#ifndef _WIN32
// absolute は path を絶対パスにする。/ で始まればそのまま、それ以外は現在の作業ディレクトリの
// 下（Linux の実行ファイルでは dladdr が argv[0] を返し、相対パスの場合がある。呼ぶ前に作業
// ディレクトリを変更すると誤る）。path が空ならば空。作業ディレクトリを取得できなければ
// std::filesystem::filesystem_error を送出する（std::filesystem::absolute）。
std::optional<std::string> absolute(const std::string &path);
#endif

// makeDirectories は path を作成する（親も存在しなければ作成する。POSIX は 0700）。既に存在すれば
// 何もしない。所有者と権限は確認しない。
bool makeDirectories(const std::string &path, std::string &error);

// list はディレクトリの中の名前を返す（. と .. を除く）。読み取れなければ空。
std::optional<std::vector<std::string>> list(const std::string &dir);

// read はファイルの先頭から limit バイトまでを読み取る。開けない・通常のファイルでない・読み取れ
// なければ空。
std::optional<std::string> read(const std::string &path, std::size_t limit);

// write はファイルを削除してから新たに作成し、data を書き込む（POSIX は 0600）。既存のものが
// リンク（シンボリックリンクでも hard link でも）であっても、リンクの先は変更しない。
bool write(const std::string &path, std::string_view data);

// rename は from を to へ移動する（to が存在すれば置換する）。
bool rename(const std::string &from, const std::string &to);

// remove はファイルを削除する。削除できた場合のみ真。
bool remove(const std::string &path);

// WaitState は待機標識（<id>.wait）の状態（通信規則「待機標識」）。
enum class WaitState {
	// 呼び出し元が保持している（待機している）
	Held,
	// 取得できた（呼び出し元が解放した・終了した）
	Released,
	// 存在しない（開けない・通常のファイルでない場合も。実行しない側に倒す）
	Missing,
};

// probe は待機標識を取得できるか試行し、取得できたら直ちに解放する。
WaitState probe(const std::string &path);

// Lock はファイルを排他的に保持し続ける（bridge.lock。通信規則「ロック」）。破棄すると解放する。
class Lock {
public:
	enum class Result {
		// 取得した
		Taken,
		// 他が保持している
		Busy,
		// 開けない
		Failed,
	};

	Lock() = default;
	~Lock();
	Lock(const Lock &) = delete;
	Lock &operator=(const Lock &) = delete;

	// take は path を作成して（存在すれば開いて）排他的に取得する。Failed ならば error に理由を
	// 書き込む。
	//
	// path が通常のファイルでない・内容がローカルに存在しない（Windows）として拒否した場合は、
	// 名前を削除して再作成する。但し 1 つの Lock につき 1 度のみ（受付処理の度に削除と作成を
	// 繰り返さない。同期のサービスなどが、その度に動作しないように）。
	Result take(const std::string &path, std::string &error);
	bool held() const { return handle_ != kNone; }
	void release();

private:
	// POSIX はファイル記述子、Windows は HANDLE（INVALID_HANDLE_VALUE は -1）。
	static constexpr std::intptr_t kNone = -1;
	std::intptr_t handle_ = kNone;
	// 拒否した path を、既に再作成したか。
	bool recreated_ = false;
};

}
