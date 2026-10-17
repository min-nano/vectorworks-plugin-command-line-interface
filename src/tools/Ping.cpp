#include "PluginPrefix.h"

#include "BuildConfig.h"
#include "core/Ping.h"
#include "tools/Tools.h"

#include <cstdint>
#include <optional>
#include <string>

#if GS_MAC
#include <unistd.h>
#endif

namespace VwCli::tools {

namespace {

std::int64_t processId() {
#if GS_MAC
	return static_cast<std::int64_t>(::getpid());
#else
	return static_cast<std::int64_t>(::GetCurrentProcessId());
#endif
}

// currentLayer は現在のレイヤの名前。図面が 1 枚も開いていなければ GetCurrentLayer() は落ちずに
// nullptr を返す（起動の直後も、全部閉じた後も。Findings「Timers and Notifications」の 4 の 8）。
// ハンドルは刻みをまたいで持たない（閉じた文書と別の文書のレイヤが同じアドレスで返る。同じ節）。
std::optional<std::string> currentLayer() {
	MCObjectHandle layer = gSDK->GetCurrentLayer();
	if (layer == nullptr) {
		return std::nullopt;
	}
	TXString name;
	gSDK->GetObjectName(layer, name);
	// operator const char* は UTF-8 を返す（Findings「TXString」）。式の中で写しきる。
	return std::string(static_cast<const char *>(name));
}

}

core::Json ping(const core::Json &args, const core::ToolContext &context, core::ToolError &error) {
	core::PingFacts facts{VW_BUILD_VERSION, VW_BUILD_BRANCH, processId(), currentLayer()};
	return core::ping(args, context, facts, error);
}

}
