#pragma once

// 道具の表（docs/plugin/bridge.md「道具の表」）。道具を足すときは表の 1 行と中身 1 つを足す。

#include "core/Serve.h"

#include <span>

namespace VwCli::tools {

std::span<const core::Tool> table();

}
