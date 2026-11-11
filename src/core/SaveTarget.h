#pragma once

// save の保存先（to）の確かめ（docs/plugin/tools.md「図面を扱う道具」の save）。SDK に依らない。
//
//   - to の形の規則は mac と Windows の 2 つの関数にし、どちらもどの OS でもビルドする（文字列の
//     処理だけで OS の呼び出しを使わない）。プラグインが使う規則は kNativeForm（_WIN32 で選ぶのは
//     ここだけ）。
//   - 状態の確かめの手順（to_is_open → file_exists → parent_missing）は、比べる関数とファイル
//     システムに聞く関数を引数で受ける（テストは差し替えを渡す）。
//   - ファイルシステムに聞く関数（probeTo・probeParent）と、OS のエラーの番号を「無い」とそれ以外に
//     分ける表（isMissing）は OS ごとのファイル（SaveTargetPosix.cpp・SaveTargetWindows.cpp）。

#include "core/Serve.h"

#include <cstdint>
#include <functional>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core::save_target {

// 失敗の detail の reason（docs/plugin/tools.md）。
inline constexpr std::string_view kToIsOpen = "to_is_open";
inline constexpr std::string_view kFileExists = "file_exists";
inline constexpr std::string_view kParentMissing = "parent_missing";
inline constexpr std::string_view kToCheckFailed = "to_check_failed";

// FormCheck は to の形の規則。正しければ親のフォルダのパス（ルートの直下ならルートの形。区切りを
// 含む）を返し、誤りなら空を返して problem に理由（日本語）を書く。
using FormCheck = std::optional<std::string> (*)(std::string_view to, std::string &problem);

// checkMacTo は mac の規則（/ から始まる絶対パス。255 バイトを超える段・1024 バイト以上を断る）。
std::optional<std::string> checkMacTo(std::string_view to, std::string &problem);

// checkWindowsTo は Windows の規則（X:\ で始まる絶対パスと UNC。/ を含むもの・255 文字を超える段・
// 260 文字以上（どちらも UTF-16 の単位）を断る）。
std::optional<std::string> checkWindowsTo(std::string_view to, std::string &problem);

// kNativeForm はプラグインが使う規則（出荷するプラグインは OS
// ごとのビルドなので、ビルドで決まる）。
extern const FormCheck kNativeForm;

// Entry はファイルシステムに聞いた結果。
enum class Entry {
	File,
	Directory,
	// 無い（OS のエラーの番号が「無い」の表にある）
	Missing,
	// 確かめられない（それ以外の失敗。osError に番号）
	Failed,
};

struct Probe {
	Entry entry = Entry::Missing;
	// Failed のときの OS のエラーの番号（mac は errno、Windows は GetLastError()）。
	std::int64_t osError = 0;
};

// isMissing は OS のエラーの番号が「無い」に当たるか（mac は ENOENT・ENOTDIR、Windows は
// 2・3・67・267）。
bool isMissing(std::int64_t osError);

// probeTo は to そのものを、リンクをたどらずに聞く（mac は lstat、Windows は \\?\ を付けて
// GetFileAttributesW）。壊れたリンクもあるものとして返す。
Probe probeTo(const std::string &path);

// probeParent は親のフォルダを、リンクをたどって聞く（mac は stat、Windows は \\?\ を付けて
// FILE_FLAG_BACKUP_SEMANTICS の CreateFileW と GetFileInformationByHandle。望むアクセスは 0、共有は
// 読み・書き・消すのすべて）。
Probe probeParent(const std::string &path);

// Checks は状態の確かめに使う関数。
struct Checks {
	// same は開いている図面のパスと to が一致するか（NFC にして大文字と小文字を区別せずに比べる）。
	std::function<bool(std::string_view, std::string_view)> same;
	std::function<Probe(const std::string &)> to;
	std::function<Probe(const std::string &)> parent;
};

// check は保存先の状態を to_is_open → file_exists → parent_missing の順に確かめる（OS の呼び出しが
// 「無い」以外の理由で失敗すれば、その段で to_check_failed）。通れば真。断るなら status を internal
// にして偽を返す。openPaths は開いているすべての図面のパス。
bool check(const std::string &to, const std::string &parent,
           const std::vector<std::string> &openPaths, const Checks &checks, ToolStatus &status);

}
