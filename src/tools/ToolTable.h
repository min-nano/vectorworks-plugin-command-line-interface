#pragma once

// ツールの一覧（docs/plugin/bridge.md「ツールの一覧」）。ツールを追加する場合は一覧の 1 行と実装
// 1 つを追加する。

#include "core/Serve.h"

#include <span>

namespace VwCli::tools {

std::span<const core::Tool> table();

}
