#pragma once

// 道具の中身（SDK に依る）。表は ToolTable.cpp。

#include "core/Json.h"
#include "core/Serve.h"

namespace VwCli::tools {

core::Json ping(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);

}
