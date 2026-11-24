#pragma once

// 道具 quit のうち SDK に依らない部分（docs/plugin/tools.md「quit」）。引数の検査・確かめの順
// （引数の形 → 起動を終えたか → 開いている図面 → 中に残った閉じた図面の片付け（片付かなければ
// closed_documents_remain。片付けのあとに図面が開いていれば documents_open））・失敗の detail の
// 形をここに置き、開いている図面の
// 一覧は Documents（src/tools/Documents.cpp が SDK で作る）から受けるので、無 SDK でテストできる。
// 終了を頼むのは道具ではなく、受け付けの外のタイマー（core/Lifecycle.h の tick）。

#include "core/DocumentTools.h"
#include "core/Json.h"
#include "core/Serve.h"

#include <string_view>

namespace VwCli::core {

// 道具の表の行。Vectorworks に働く App（占有の中でだけ呼べる。Read で登録すると占有なしで終了を
// 頼める）。
inline constexpr ToolSpec kQuitSpec{
    "quit",
    "Vectorworks を終了させる（図面が 1 枚も開いていないときだけ。開いていれば一覧を detail に"
    "載せて断る。起動を終えるまでと、閉じた図面が中に残って片付けられないときも断る）。"
    "保存の確認は出さず、終了を見届けるのは呼ぶ側",
    R"({"type":"object","properties":{},"additionalProperties":false})", ToolKind::App};

// 失敗の detail の reason（docs/plugin/tools.md「quit」）。
inline constexpr std::string_view kStarting = "starting";
inline constexpr std::string_view kDocumentsOpen = "documents_open";
inline constexpr std::string_view kClosedDocumentsRemain = "closed_documents_remain";

// quit は、起動を終えていて（context.startup.started）図面が 1 枚も開いていなければ、閉じた図面が
// 中に残っていれば片付けて（clearClosedDocuments）、終了の依頼（status.quit）を立てて
// {"quitting":true} を返す。失敗はどれも internal で、終了を頼まない。起動を終えていなければ
// starting（detail に通知を登録できたか）、図面が開いていれば documents_open（detail に一覧）、
// 残った図面を片付けられなければ closed_documents_remain。
Json quit(const Json &args, const ToolContext &context, Documents &docs, ToolStatus &status);

}
