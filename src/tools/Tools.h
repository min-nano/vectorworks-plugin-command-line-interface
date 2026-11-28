#pragma once

// ツールの実装（SDK に依存する）。一覧は ToolTable.cpp。SDK に依存しない部分は src/core の
// 同じ名前の関数。

#include "core/Json.h"
#include "core/Serve.h"

namespace VwCli::tools {

core::Json ping(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);

// 読み取りツール（ReadTools.cpp）。
core::Json layers(const core::Json &args, const core::ToolContext &context,
                  core::ToolStatus &status);
core::Json classes(const core::Json &args, const core::ToolContext &context,
                   core::ToolStatus &status);
core::Json layerObjects(const core::Json &args, const core::ToolContext &context,
                        core::ToolStatus &status);
core::Json objectCounts(const core::Json &args, const core::ToolContext &context,
                        core::ToolStatus &status);

// 図面を操作するツール（Documents.cpp）。
core::Json documents(const core::Json &args, const core::ToolContext &context,
                     core::ToolStatus &status);
core::Json save(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);
core::Json close(const core::Json &args, const core::ToolContext &context,
                 core::ToolStatus &status);
core::Json exportPdf(const core::Json &args, const core::ToolContext &context,
                     core::ToolStatus &status);

// 終了の依頼（Documents.cpp。開いている図面の一覧を、図面を操作するツールと同じインターフェースで
// 読み取る）。
core::Json quit(const core::Json &args, const core::ToolContext &context, core::ToolStatus &status);

}
