#pragma once

// 通信規則（docs/protocol.md）の定数。protocol/fixtures/constants.json と対で、テストが
// 照合する（CLI 側の cli/internal/spool も同じフィクスチャと照合する）。通信規則を変更したら、
// 文書とフィクスチャと両側の実装を修正する。

#include <algorithm>
#include <cstddef>
#include <string_view>

namespace VwCli::core::protocol {

// 通信規則の版。@tools と ping の結果に含めるのみで、実行時に照合しない（通信規則「版」）。
inline constexpr int kVersion = 7;

// cli のプラグインのフォルダ（<Plug-Ins>/cli/）の下のスプールの名前（通信規則「スプールの場所」）。
inline constexpr std::string_view kSpoolDirName = "spool";

// スプールに配置するファイルの綴り（通信規則「ファイル」）。
inline constexpr std::string_view kLockFile = "bridge.lock";
inline constexpr std::string_view kRequestSuffix = ".req.json";
inline constexpr std::string_view kResponseSuffix = ".res.json";
inline constexpr std::string_view kWaitSuffix = ".wait";
inline constexpr std::string_view kTempSuffix = ".tmp";

// 要求 1 件の大きさと、入れ子（オブジェクトと配列）の深さの上限（通信規則「要求」）。深さは要求の
// 外側のオブジェクトを 1 と数える。
inline constexpr std::size_t kMaxRequestBytes = std::size_t{1} << 20;
inline constexpr int kMaxNestingDepth = 64;

// id の長さの上限（綴りは [0-9A-Za-z_-]{1,64}。通信規則「id」）。
inline constexpr std::size_t kMaxIdLength = 64;

// 予約ツール（通信規則「予約ツール」）。通信規則そのものが担う 3 つは、ツールの名前の綴り
// （[a-z0-9_]+(\.[a-z0-9_]+)*）では記述できない @ で始め、ツールの名前と重複しないようにする。
// ping は本プラグインのツール（種類 read）としてツールの一覧に含まれる。
inline constexpr std::string_view kToolTools = "@tools";
inline constexpr std::string_view kToolPing = "ping";
inline constexpr std::string_view kToolSessionStart = "@session_start";
inline constexpr std::string_view kToolSessionEnd = "@session_end";

// ツールの種類（@tools の結果の kind）。
inline constexpr std::string_view kKindRead = "read";
inline constexpr std::string_view kKindWrite = "write";
inline constexpr std::string_view kKindApp = "app";

// 失敗の種別（応答の code。通信規則「応答」）。
inline constexpr std::string_view kCodeInvalidRequest = "invalid_request";
inline constexpr std::string_view kCodeUnknownTool = "unknown_tool";
inline constexpr std::string_view kCodeInvalidArgs = "invalid_args";
inline constexpr std::string_view kCodeNoWait = "no_wait";
inline constexpr std::string_view kCodeBusy = "busy";
inline constexpr std::string_view kCodeOccupied = "occupied";
inline constexpr std::string_view kCodeNoSession = "no_session";
inline constexpr std::string_view kCodeInternal = "internal";

// validId は id の綴りが [0-9A-Za-z_-]{1,64} を満たすか。ファイル名に使用するため、満たさない要求は
// 受け付けない（../ を混入させてスプールの外へ書き込ませないための関門）。
constexpr bool validId(std::string_view id) {
	return !id.empty() && id.size() <= kMaxIdLength && std::ranges::all_of(id, [](char c) {
		return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
		       c == '_' || c == '-';
	});
}

}
