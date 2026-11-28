#pragma once

// ツール ping の内 SDK に依存しない部分（docs/plugin/tools.md「ping」）。SDK から収集する値（現在の
// レイヤの名前）は呼び出し元（src/tools/Ping.cpp）が渡すため、無 SDK でテストできる。

#include "core/Json.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <cstdint>
#include <optional>
#include <string>

namespace VwCli::core {

// PingFacts は ping の結果に含まれる、受付処理の外の値。
struct PingFacts {
	// VW_BUILD_VERSION（短い sha）と VW_BUILD_BRANCH。
	std::string version;
	std::string branch;
	// 受付処理を行っている Vectorworks のプロセス ID。
	std::int64_t pid = 0;
	// 現在のレイヤの名前。図面が開いていなければ空（GetCurrentLayer() が nullptr）。
	std::optional<std::string> currentLayer;
};

// kPingSpec はツール ping の一覧の 1 行の内 SDK に依存しない部分（ToolSpec）。
// 名前は通信規則の綴り（core/Protocol.h）を使用し、テストが検証する名前と
// 応答する名前を 1 か所に統一する。
inline constexpr ToolSpec kPingSpec{
    protocol::kToolPing,
    "プラグインの版・通信規則の版・受け付けている Vectorworks と、図面が開いているかを返す",
    R"({"type":"object","properties":{},"additionalProperties":false})", ToolKind::Read};

// ping は ping の結果を返す。引数を受け付けないため、args にキーがあれば invalid_args で失敗する
// （通信規則「互換性」）。
Json ping(const Json &args, const ToolContext &context, const PingFacts &facts, ToolStatus &status);

}
