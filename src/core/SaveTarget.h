#pragma once

// save の保存先（to）の検証（docs/plugin/save-target.md）。SDK に依存しない。
//
//   - to の形式の規則は mac と Windows の 2 つの関数とし、いずれも全ての OS でビルドする（文字列の
//     処理のみで OS の呼び出しを使用しない）。プラグインが使用する規則は kNativeForm（_WIN32 で
//     選択するのはここのみ）。
//   - 状態の検証の手順（to_is_open → file_exists → parent_missing）は、比較する関数とファイル
//     システムに問い合わせる関数を引数で受け取る（テストは差し替えたものを渡す）。
//   - ファイルシステムに問い合わせる関数（probeTo・probeParent）と、OS のエラーの番号を「無い」と
//     それ以外に区別する表（isMissing）は OS ごとのファイル（SaveTargetPosix.cpp・
//     SaveTargetWindows.cpp）。

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

// FormCheck は to の形式の規則。正しければ親のフォルダのパス（ルートの直下ならルートの形式。
// 区切りを含む）を返し、誤りであれば空を返して problem に理由（日本語）を書き込む。
using FormCheck = std::optional<std::string> (*)(std::string_view to, std::string &problem);

// checkMacTo は mac の規則（/ から始まる絶対パス。255 バイトを超える段・1024 バイト以上を
// 拒否する）。
std::optional<std::string> checkMacTo(std::string_view to, std::string &problem);

// checkWindowsTo は Windows の規則（X:\ で始まる絶対パスと UNC。/ を含むもの・255 文字を超える段・
// 260 文字以上（いずれも UTF-16 の単位）を拒否する）。
std::optional<std::string> checkWindowsTo(std::string_view to, std::string &problem);

// kNativeForm はプラグインが使用する規則（出荷するプラグインは OS ごとのビルドであるため、
// ビルドで決定される）。
extern const FormCheck kNativeForm;

// Entry はファイルシステムに問い合わせた結果。
enum class Entry {
	File,
	Directory,
	// 存在しない（OS のエラーの番号が「無い」の表に含まれる）
	Missing,
	// 確認できない（それ以外の失敗。osError に番号）
	Failed,
};

struct Probe {
	Entry entry = Entry::Missing;
	// Failed の場合の OS のエラーの番号（mac は errno、Windows は GetLastError()）。
	std::int64_t osError = 0;
};

// isMissing は OS のエラーの番号が「無い」に該当するか（mac は ENOENT・ENOTDIR、Windows は
// 2・3・67・267）。
bool isMissing(std::int64_t osError);

// probeTo は to そのものを、リンクをたどらずに問い合わせる（mac は lstat、Windows は \\?\ を付けて
// GetFileAttributesW）。壊れたリンクも存在するものとして返す。
Probe probeTo(const std::string &path);

// probeParent は親のフォルダを、リンクをたどって問い合わせる（mac は stat、Windows は \\?\ を付けて
// FILE_FLAG_BACKUP_SEMANTICS の CreateFileW と GetFileInformationByHandle。要求するアクセスは 0、
// 共有は読み取り・書き込み・削除の全て）。
Probe probeParent(const std::string &path);

// Checks は状態の検証に使用する関数。
struct Checks {
	// same は開いている図面のパスと to が一致するか（NFC にして大文字と小文字を区別せずに
	// 比較する）。
	std::function<bool(std::string_view, std::string_view)> same;
	std::function<Probe(const std::string &)> to;
	std::function<Probe(const std::string &)> parent;
};

// check は保存先の状態を to_is_open → file_exists → parent_missing の順に検証する（OS の呼び出しが
// 「無い」以外の理由で失敗すれば、その段階で to_check_failed）。通過すれば真。拒否する場合は
// status を internal にして偽を返す。openPaths は開いている全ての図面のパス。
bool check(const std::string &to, const std::string &parent,
           const std::vector<std::string> &openPaths, const Checks &checks, ToolStatus &status);

}
