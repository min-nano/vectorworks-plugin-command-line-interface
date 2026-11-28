#pragma once

// ツール quit の内 SDK に依存しない部分（docs/plugin/tools.md「quit」）。引数の検査・確認の順序
// （引数の形式 → 起動を終えたか → 開いている図面 → 内部に残存した閉じた図面の後処理（完了しなければ
// closed_documents_remain。後処理の後に図面が開いていれば documents_open））・失敗の detail の
// 形式をここに配置し、開いている図面の一覧は Documents（src/tools/Documents.cpp が SDK で作成する）
// から受け取るため、無 SDK でテストできる。
// 終了を依頼するのはツールではなく、受付処理の外のタイマー（core/Lifecycle.h の tick）。

#include "core/DocumentTools.h"
#include "core/Json.h"
#include "core/Serve.h"

#include <string_view>

namespace VwCli::core {

// ツールの一覧の行。Vectorworks に作用する App（占有の中でのみ呼び出せる。Read で登録すると占有
// なしで終了を依頼できる）。
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
// 内部に残存していれば後処理を行い（clearClosedDocuments）、終了の依頼（status.quit）を設定して
// {"quitting":true} を返す。失敗はいずれも internal で、終了を依頼しない。起動を終えていなければ
// starting（detail に通知を登録できたか）、図面が開いていれば documents_open（detail に一覧）、
// 残存した図面の後処理ができなければ closed_documents_remain。
Json quit(const Json &args, const ToolContext &context, Documents &docs, ToolStatus &status);

}
