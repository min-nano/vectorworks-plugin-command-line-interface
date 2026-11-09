#pragma once

// 道具の中身（SDK に依る）。表は ToolTable.cpp。SDK に依らない部分は src/core の同じ名前の関数。

#include "core/Json.h"
#include "core/Serve.h"

namespace VwCli::tools {

core::Json ping(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);

// 読む道具（ReadTools.cpp）。
core::Json layers(const core::Json &args, const core::ToolContext &context,
                  core::ToolStatus &status);
core::Json classes(const core::Json &args, const core::ToolContext &context,
                   core::ToolStatus &status);
core::Json layerObjects(const core::Json &args, const core::ToolContext &context,
                        core::ToolStatus &status);
core::Json objectCounts(const core::Json &args, const core::ToolContext &context,
                        core::ToolStatus &status);

// 図面を扱う道具（Documents.cpp）。
core::Json documents(const core::Json &args, const core::ToolContext &context,
                     core::ToolStatus &status);
core::Json save(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);
core::Json close(const core::Json &args, const core::ToolContext &context,
                 core::ToolStatus &status);

}
