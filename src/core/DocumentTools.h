#pragma once

// 図面を操作するツール（documents・save・close・export_pdf）の内 SDK に依存しない部分
// （docs/plugin/tools.md「図面を操作するツール」・「export_pdf」）。引数の検査・検証の順序（引数の
// 形式 → no_document → 指定する図面 → 状態）・保存先の検証・前面を戻す手順・結果の形式をここに
// 配置し、SDK の呼び出しは Documents と SheetPdf（src/tools/Documents.cpp が SDK で実装する）から
// 受け取るため、無 SDK でテストできる。

#include "core/Json.h"
#include "core/SaveTarget.h"
#include "core/Serve.h"

#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core {

// ツールの一覧の行（種類を含む。src/tools/ToolTable.cpp はこれを使用する）。save・close は図面の
// ファイルやウィンドウに作用する App（占有の中でのみ呼び出せる。Read で登録すると占有なしで
// 保存・閉じることができる）。
inline constexpr ToolSpec kDocumentsSpec{
    "documents",
    "開いている図面の一覧（名前・パス・ディスクのファイルに結び付いていないか・前面か）を返す",
    R"({"type":"object","properties":{},"additionalProperties":false})", ToolKind::Read};

inline constexpr ToolSpec kSaveSpec{
    "save",
    "名指した図面を to の新しいパスへ保存する（上書き保存はしない。"
    "既にあるファイル・開いている図面のパスは断る）。"
    "保存すると図面のパスは to へ移り、前面の図面は保存の前に戻す",
    R"({"type":"object","properties":{)"
    R"("document":{"type":"string","description":"図面のパス（documents の path）"},)"
    R"("to":{"type":"string","description":"保存先の新しいパス（絶対パス。拡張子は .vwx）"}},)"
    R"("required":["document","to"],"additionalProperties":false})",
    ToolKind::App};

inline constexpr ToolSpec kCloseSpec{
    "close",
    "名指した図面を、保存の確認を出さずに閉じる（保存せず、未保存の変更は捨てる）。最後の図面を"
    "閉じたら、Vectorworks の中に残った閉じた図面も片付ける（変更は捨てる）。片付けきれなければ、"
    "成功のまま結果に closed_documents_remain:true を載せる",
    R"({"type":"object","properties":{)"
    R"("document":{"type":"string","description":"図面のパス（documents の path）"}},)"
    R"("required":["document"],"additionalProperties":false})",
    ToolKind::App};

// export_pdf は図面の内容を変更せず、ファイルに書き込む App（save と同じ）。
inline constexpr ToolSpec kExportPdfSpec{
    "export_pdf",
    "名指した図面のシートレイヤを、sheets の順に 1 枚 1 頁で to の新しい PDF へ書き出す"
    "（ダイアログを出さない。既にあるファイルは断る）。前面の図面は書き出した後に戻す",
    R"({"type":"object","properties":{)"
    R"("document":{"type":"string","description":"図面のパス（documents の path）"},)"
    R"("sheets":{"type":"array","items":{"type":"string"},"minItems":1,)"
    R"("description":"シートレイヤの名前（layers の kind が sheet の name）。この順に頁が並ぶ"},)"
    R"("to":{"type":"string","description":"書き出す先の新しいパス（絶対パス。拡張子は .pdf）"}},)"
    R"("required":["document","sheets","to"],"additionalProperties":false})",
    ToolKind::App};

// 失敗の detail の reason（docs/plugin/tools.md）。
inline constexpr std::string_view kDocumentAmbiguous = "document_ambiguous";
inline constexpr std::string_view kSaveFailed = "save_failed";
inline constexpr std::string_view kNotClosed = "not_closed";
inline constexpr std::string_view kExportFailed = "export_failed";

// close の結果に、閉じた図面が内部に残存して後処理できなかった場合のみ真で含めるフィールド
// （docs/plugin/tools.md「close」）。
inline constexpr std::string_view kClosedDocumentsRemainField = "closed_documents_remain";

// kMaxClosedRemnants は clearClosedDocuments が CloseDocument を呼び出す回数の上限。残存した図面は
// 1 回で 1 枚削除される（Findings「Documents」の「閉じた図面が残り、終了で保存の確認が出る」）
// ため、常駐の間に蓄積しうる枚数より十分に大きく、受付処理を長時間停止させない数とする。
inline constexpr int kMaxClosedRemnants = 64;

// OpenDocument は GetOpenFilesList の 1 つの項目。
struct OpenDocument {
	// fpFileID の GetFileFullPath（UTF-8。正規化しない）。
	std::string path;
	// fIsInMemoryOnly。
	bool untitled = false;
	// fIsActive。
	bool active = false;
	// fFileRef（SwitchToOpenFile のキー）。
	std::int32_t ref = 0;
};

// Documents は開いている図面に作用するインターフェース。1 回のツールの実行の中でのみ使用する。
class Documents {
public:
	virtual ~Documents() = default;
	// list は GetOpenFilesList の項目をその順序で返す。
	virtual std::vector<OpenDocument> list() = 0;
	// switchTo は SwitchToOpenFile。
	virtual bool switchTo(std::int32_t ref) = 0;
	// saveActive は前面の図面を path へ保存する（SaveActiveDocumentPath）。GSError を返す（0
	// は成功）。
	virtual std::uint32_t saveActive(const std::string &path) = 0;
	// closeActive は前面の図面を、保存の確認を表示せずに閉じる（CloseDocument。戻り値は信頼
	// できないため返さない）。一覧が空の場合に呼ぶと、内部に残存した閉じた図面（残存した図面）を
	// 1 枚削除する（Findings「Documents」の「閉じた図面が残り、終了で保存の確認が出る」）。
	virtual void closeActive() = 0;
	// hasActive は GetActiveDocument が真か。一覧が空であるのに真ならば、残存した図面が存在する
	// （同一の節）。
	virtual bool hasActive() = 0;
};

// SheetPdf は前面の図面のシートレイヤを PDF に書き出すインターフェース（IExportPDF。Findings
// 「PDF Export」）。1 回のツールの実行の中でのみ使用する。
class SheetPdf {
public:
	virtual ~SheetPdf() = default;
	// sheets は前面の図面のシートレイヤ（ovLayerType が 2 のレイヤ）の名前を列の順に返す。
	virtual std::vector<std::string> sheets() = 0;
	// open は利用者の書き出しの設定を退避し、書き出す設定（fOpenInViewer を偽・
	// eWholeAreaAsOnePage）にしてから path の PDF を開く（OpenPDFDocument）。VCOMError を返す
	// （0 は成功）。失敗した場合は設定を戻してから返す。
	virtual std::uint32_t open(const std::string &path) = 0;
	// page は開いた PDF にシート sheet の頁を足す（ExportPDFPages）。VCOMError を返す。
	virtual std::uint32_t page(const std::string &sheet) = 0;
	// close は PDF を閉じて書き終え（ClosePDFDocument）、退避した設定を戻す。open が成功した
	// 場合にのみ、1 回呼ぶ。VCOMError（ClosePDFDocument の戻り値）を返す。
	virtual std::uint32_t close() = 0;
};

// SaveEnvironment は save と export_pdf の書き出す先の検証に使用する規則と関数（プラグインは to の
// 形式の規則に save_target::kNativeForm を、比較の方法に path_compare::equivalent を渡す）。
struct SaveEnvironment {
	save_target::FormCheck form = nullptr;
	save_target::Checks checks;
};

// clearClosedDocuments は、一覧が空であるのに残存した図面が存在すれば（GetActiveDocument が真）、
// CloseDocument で 1 枚ずつ削除する（先に開いた図面を、後に開いた図面が開いている間に閉じると、
// 閉じた図面が内部に残存し、終了時に保存の確認を表示する。SDK リファレンス #321）。図面が開いて
// いれば何もしない（CloseDocument はその図面を閉じるため、毎回一覧を確認する）。後処理が完了した
// （図面が開いている・残存した図面が無い）ならば真、kMaxClosedRemnants 回呼び出しても残存して
// いれば偽。
bool clearClosedDocuments(Documents &docs);

// documentName は path の最後の区切り文字より後方（documents の name）。
std::string documentName(std::string_view path);

// documentJson は図面の項目 {"name","path","untitled"}（documents の結果の項目と、quit の
// documents_open の detail の項目に共通の部分）。
Json documentJson(const OpenDocument &open);

Json documents(const Json &args, Documents &docs, ToolStatus &status);
Json save(const Json &args, Documents &docs, const SaveEnvironment &environment,
          ToolStatus &status);
Json close(const Json &args, Documents &docs, ToolStatus &status);
Json exportPdf(const Json &args, Documents &docs, SheetPdf &pdf, const SaveEnvironment &environment,
               ToolStatus &status);

}
