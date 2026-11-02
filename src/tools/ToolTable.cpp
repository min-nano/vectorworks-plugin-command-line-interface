#include "tools/ToolTable.h"

#include "tools/Tools.h"

#include "core/Protocol.h"

namespace VwCli::tools {

namespace {

// 段 2 に載せるのは ping だけ（docs/plugin/plan.md）。
// 名前は通信規則の綴り（core/Protocol.h）を使い、テストが確かめる名前と
// 応える名前を 1 か所にする。
constexpr core::Tool kTools[] = {
    {core::protocol::kToolPing,
	 "プラグインの版・通信規則の版・受け付けている Vectorworks と、図面が開いているかを返す",
	 R"({"type":"object","properties":{},"additionalProperties":false})", core::ToolKind::Read,
	 &ping},
};

}

std::span<const core::Tool> table() { return kTools; }

}
