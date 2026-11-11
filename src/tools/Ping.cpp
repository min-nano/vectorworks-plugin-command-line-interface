#include "PluginPrefix.h"

#include "BuildConfig.h"
#include "core/Ping.h"
#include "tools/Text.h"
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
	// レイヤのハンドルに GetObjectName を使うと、レイヤの名前（「レイヤ-1」）が mac・Windows とも
	// 読める（Findings「Timers and Notifications」の 4 の 8 の表）。
	gSDK->GetObjectName(layer, name);
	return utf8(name);
}

}

core::Json ping(const core::Json &args, const core::ToolContext &context,
                core::ToolStatus &status) {
	core::PingFacts facts{VW_BUILD_VERSION, VW_BUILD_BRANCH, processId(), currentLayer()};
	return core::ping(args, context, facts, status);
}

}
