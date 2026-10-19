#include "tools/ToolTable.h"

#include "tools/Tools.h"

namespace VwCli::tools {

namespace {

// 段 2 に載せるのは ping だけ（docs/plugin/plan.md）。
constexpr core::Tool kTools[] = {
    {"ping",
	 "プラグインの版・通信規則の版・受け付けている Vectorworks と、図面が開いているかを返す",
	 R"({"type":"object","properties":{},"additionalProperties":false})", core::ToolKind::Read,
	 &ping},
};

}

std::span<const core::Tool> table() { return kTools; }

}
