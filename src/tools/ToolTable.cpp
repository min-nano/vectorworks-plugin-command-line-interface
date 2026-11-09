#include "tools/ToolTable.h"

#include "tools/Tools.h"

#include "core/DocumentTools.h"
#include "core/Protocol.h"
#include "core/ReadTools.h"

namespace VwCli::tools {

namespace {

// 名前・説明・inputSchema・種類は src/core の各道具の定数（ToolSpec）を使い、種類を無 SDK の
// テストで確かめる（save・close は App。Read で載せると占有なしで保存・閉じられる）。ping の名前は
// 通信規則の綴り（core/Protocol.h）を使い、テストが確かめる名前と応える名前を 1 か所にする。
const core::Tool kTools[] = {
    {core::protocol::kToolPing,
	 "プラグインの版・通信規則の版・受け付けている Vectorworks と、図面が開いているかを返す",
	 R"({"type":"object","properties":{},"additionalProperties":false})", core::ToolKind::Read,
	 &ping},
    core::toolOf(core::kLayersSpec, &layers),
    core::toolOf(core::kClassesSpec, &classes),
    core::toolOf(core::kLayerObjectsSpec, &layerObjects),
    core::toolOf(core::kObjectCountsSpec, &objectCounts),
    core::toolOf(core::kDocumentsSpec, &documents),
    core::toolOf(core::kSaveSpec, &save),
    core::toolOf(core::kCloseSpec, &close),
};

}

std::span<const core::Tool> table() { return kTools; }

}
