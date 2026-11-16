#include "core/Quit.h"

#include "core/Args.h"

#include <string>
#include <vector>

namespace VwCli::core {

Json quit(const Json &args, const ToolContext &context, Documents &docs, ToolStatus &status) {
	// 1. 引数の形。
	args::Reader reader(args, kQuitSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	// 2. 起動を終えたか。図面の一覧を見るより先に返す（起動の途中に終了を頼むと、mac では起動画面の
	// まま止まるか、頼みが黙って捨てられる。docs/plugin/tools.md「quit」）。
	if (!context.started) {
		args::failWithReason(status, kStarting,
		                     "Vectorworks が起動を終えていないので終了しませんでした");
		return Json();
	}
	// 3. 開いている図面。保存済みの図面の未保存の変更は読めないので、1 枚でも開いていれば断る
	// （終了で保存の確認を出さないため）。
	std::vector<OpenDocument> open = docs.list();
	if (!open.empty()) {
		Json list = Json::array();
		for (const OpenDocument &document : open) {
			list.push(Json::object({{"name", Json::string(documentName(document.path))},
			                        {"path", Json::string(document.path)},
			                        {"untitled", Json::boolean(document.untitled)}}));
		}
		args::fail(status, ErrorCode::Internal,
		           "図面が開いているので終了しませんでした。先に図面を閉じてください",
		           Json::object({{"reason", Json::string(std::string(kDocumentsOpen))},
		                         {"documents", std::move(list)}}));
		return Json();
	}
	// 終了を頼むのは、応答を書いてから受け付けの外で（Server が応答を書けたときだけ ServeResult
	// の quit を立てる）。
	status.quit = true;
	return Json::object({{"quitting", Json::boolean(true)}});
}

}
