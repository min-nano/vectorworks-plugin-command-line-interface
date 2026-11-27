#pragma once

// 図面を扱う道具（documents・save・close）の内 SDK に依らない部分（docs/plugin/tools.md「図面を
// 扱う道具」）。引数の検査・確かめの順（引数の形 → no_document → 名指す図面 → 状態）・save の
// 保存先の確かめ・前面を戻す手順・結果の形をここに置き、SDK の呼び出しは
// Documents（src/tools/Documents.cpp が SDK で作る）から受けるので、無 SDK でテストできる。

#include "core/Json.h"
#include "core/SaveTarget.h"
#include "core/Serve.h"

#include <cstdint>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core {

// 道具の表の行（種類を含む。src/tools/ToolTable.cpp はこれを使う）。save・close は図面のファイルや
// 窓に働く App（占有の中でだけ呼べる。Read で登録すると占有なしで保存・閉じられる）。
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

// 失敗の detail の reason（docs/plugin/tools.md）。
inline constexpr std::string_view kDocumentAmbiguous = "document_ambiguous";
inline constexpr std::string_view kSaveFailed = "save_failed";
inline constexpr std::string_view kNotClosed = "not_closed";

// close の結果に、閉じた図面が中に残って片付けられなかったときだけ真で載せるフィールド
// （docs/plugin/tools.md「close」）。
inline constexpr std::string_view kClosedDocumentsRemainField = "closed_documents_remain";

// kMaxClosedRemnants は clearClosedDocuments が CloseDocument を呼ぶ回数の上限。残った図面は 1 回で
// 1 枚消える（Findings「Documents」の「閉じた図面が残り、終了で保存の確認が出る」）ので、常駐の
// 間に溜まりうる枚数より十分に大きく、受け付けを長く止めない数にする。
inline constexpr int kMaxClosedRemnants = 64;

// OpenDocument は GetOpenFilesList の項目 1 つ。
struct OpenDocument {
	// fpFileID の GetFileFullPath（UTF-8。正規化しない）。
	std::string path;
	// fIsInMemoryOnly。
	bool untitled = false;
	// fIsActive。
	bool active = false;
	// fFileRef（SwitchToOpenFile の鍵）。
	std::int32_t ref = 0;
};

// Documents は開いている図面に働く口。道具 1 回の中でだけ使う。
class Documents {
public:
	virtual ~Documents() = default;
	// list は GetOpenFilesList の項目をその順に返す。
	virtual std::vector<OpenDocument> list() = 0;
	// switchTo は SwitchToOpenFile。
	virtual bool switchTo(std::int32_t ref) = 0;
	// saveActive は前面の図面を path へ保存する（SaveActiveDocumentPath）。GSError を返す（0
	// は成功）。
	virtual std::uint32_t saveActive(const std::string &path) = 0;
	// closeActive は前面の図面を、保存の確認を出さずに閉じる（CloseDocument。戻り値は当てに
	// ならないので返さない）。一覧が空のときに呼ぶと、閉じた図面が中に残ったもの（残った図面）を
	// 1 枚消す（Findings「Documents」の「閉じた図面が残り、終了で保存の確認が出る」）。
	virtual void closeActive() = 0;
	// hasActive は GetActiveDocument が真か。一覧が空なのに真なら、残った図面がある（同じ節）。
	virtual bool hasActive() = 0;
};

// SaveEnvironment は save の保存先の確かめに使う規則と関数（プラグインは to の形の規則に
// save_target::kNativeForm を、比べ方に path_compare::equivalent を渡す）。
struct SaveEnvironment {
	save_target::FormCheck form = nullptr;
	save_target::Checks checks;
};

// clearClosedDocuments は、一覧が空なのに残った図面があれば（GetActiveDocument が真）、
// CloseDocument で 1 枚ずつ消す（先に開いた図面を後に開いた図面が開いている間に閉じると、
// 閉じた図面が中に残り、終了で保存の確認を出す。SDK リファレンス #321）。図面が開いていれば
// 何もしない（CloseDocument はその図面を閉じてしまうので、回ごとに一覧を確かめる）。片付いた
// （図面が開いている・残った図面が無い）なら真、kMaxClosedRemnants 回呼んでも残っていれば偽。
bool clearClosedDocuments(Documents &docs);

// documentName は path の最後の区切り文字より後ろ（documents の name）。
std::string documentName(std::string_view path);

// documentJson は図面の項目 {"name","path","untitled"}（documents の結果の項目と、quit の
// documents_open の detail の項目に共通の部分）。
Json documentJson(const OpenDocument &open);

Json documents(const Json &args, Documents &docs, ToolStatus &status);
Json save(const Json &args, Documents &docs, const SaveEnvironment &environment,
          ToolStatus &status);
Json close(const Json &args, Documents &docs, ToolStatus &status);

}
