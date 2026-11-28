#include "tools/ToolTable.h"

#include "tools/Tools.h"

#include "core/DocumentTools.h"
#include "core/Ping.h"
#include "core/Quit.h"
#include "core/ReadTools.h"

namespace VwCli::tools {

namespace {

// 名前・説明・inputSchema・種類は src/core の各ツールの定数（ToolSpec。constexpr であるため一覧も
// 定数で初期化され、静的な初期化の順序に依存しない）を使用し、種類を無 SDK のテストで検証する
// （save・close・export_pdf・quit は App。Read として登録すると占有なしで保存・閉じる・書き出す・
// 終了を依頼できる）。export_pdf は Windows で振る舞いを確かめるまで mac にだけ登録する
// （CLAUDE.md の 7。SDK リファレンス #334）。
constexpr core::Tool kTools[] = {
    core::toolOf(core::kPingSpec, &ping),
    core::toolOf(core::kLayersSpec, &layers),
    core::toolOf(core::kClassesSpec, &classes),
    core::toolOf(core::kLayerObjectsSpec, &layerObjects),
    core::toolOf(core::kObjectCountsSpec, &objectCounts),
    core::toolOf(core::kDocumentsSpec, &documents),
    core::toolOf(core::kSaveSpec, &save),
    core::toolOf(core::kCloseSpec, &close),
#ifndef _WIN32
    core::toolOf(core::kExportPdfSpec, &exportPdf),
#endif
    core::toolOf(core::kQuitSpec, &quit),
};

}

std::span<const core::Tool> table() { return kTools; }

}
