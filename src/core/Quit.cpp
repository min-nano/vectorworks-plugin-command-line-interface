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
	// 起動を終えた目印の通知を登録できていなければ、印はいつまでも立たないので、呼ぶ側が起動を待って
	// 呼び直し続けないように detail と文言で分けて示す（reason は同じ starting）。
	if (!context.startup.started) {
		bool registered = context.startup.notificationRegistered;
		std::string message = "Vectorworks が起動を終えていないので終了しませんでした";
		if (!registered) {
			message += "（起動を終えたことを知る通知を登録できていません）";
		}
		args::fail(status, ErrorCode::Internal, std::move(message),
		           Json::object({{"reason", Json::string(std::string(kStarting))},
		                         {"notification_registered", Json::boolean(registered)}}));
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
	// 4. 閉じた図面が中に残っていれば片付ける。一覧が空でも、残った図面が「変更あり」なら終了で
	// 保存の確認が出る（mac。Findings「Documents」の「閉じた図面が残り、終了で保存の確認が出る」）。
	// 残った図面は close（または CloseDocument を呼ぶほかのもの）が閉じたもので、変更は捨てる。
	// close の片付けが届かない道（ほかのプラグインの CloseDocument が最後の図面を閉じた・close の
	// 片付けが上限で止まったなど。実機では起こせていない）への備えで、ここでも片付ける。残った図面が
	// 無ければ GetActiveDocument を 1 回呼ぶだけ。
	if (!clearClosedDocuments(docs)) {
		args::failWithReason(status, kClosedDocumentsRemain,
		                     "閉じた図面が Vectorworks の中に残っていて片付けられなかったので"
		                     "終了しませんでした");
		return Json();
	}
	// 終了を頼むのは、応答を書いてから受け付けの外で（Server が応答を書けたときだけ ServeResult
	// の quit を立てる）。
	status.quit = true;
	return Json::object({{"quitting", Json::boolean(true)}});
}

}
