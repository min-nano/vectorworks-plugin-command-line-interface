#include "core/Quit.h"

#include "core/Args.h"

#include <string>
#include <vector>

namespace VwCli::core {

namespace {

// refuseOpen は、図面が開いている場合の失敗（internal。documents_open。detail に一覧をその順で）。
void refuseOpen(const std::vector<OpenDocument> &open, ToolStatus &status) {
	Json list = Json::array();
	for (const OpenDocument &document : open) {
		list.push(documentJson(document));
	}
	args::fail(status, ErrorCode::Internal,
	           "図面が開いているので終了しませんでした。先に図面を閉じてください",
	           Json::object({{"reason", Json::string(std::string(kDocumentsOpen))},
	                         {"documents", std::move(list)}}));
}

}

Json quit(const Json &args, const ToolContext &context, Documents &docs, ToolStatus &status) {
	// 1. 引数の形式。
	args::Reader reader(args, kQuitSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	// 2. 起動を終えたか。図面の一覧を参照するより先に返す（起動の途中に終了を依頼すると、mac では
	// 起動画面のまま停止するか、依頼が通知なく破棄される。docs/plugin/tools.md「quit」）。
	// 起動の完了を示す通知を登録できていなければ、フラグはいつまでも設定されないため、呼び出し元が
	// 起動を待機して呼び出しを繰り返さないように、detail と文言で区別して示す（reason は同じ
	// starting）。
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
	// 3. 開いている図面。保存済みの図面の未保存の変更は読み取れないため、1 枚でも開いていれば
	// 拒否する（終了で保存の確認を表示させないため）。
	std::vector<OpenDocument> open = docs.list();
	if (!open.empty()) {
		refuseOpen(open, status);
		return Json();
	}
	// 4. 閉じた図面が内部に残存していれば後処理を行う。一覧が空でも、残存した図面が「変更あり」で
	// あれば終了で保存の確認が表示される（mac。Findings「Documents」の「閉じた図面が残り、終了で
	// 保存の確認が出る」）。残存した図面は close（又は CloseDocument を呼び出す他のもの）が閉じた
	// もので、変更は破棄する。close の後処理が及ばない経路（他のプラグインの CloseDocument が最後の
	// 図面を閉じた・close の後処理が上限で停止したなど。実機では再現できていない）への備えとして、
	// ここでも後処理を行う。残存した図面が無ければ GetActiveDocument を 1 回呼び出すのみ。
	if (!clearClosedDocuments(docs)) {
		args::failWithReason(status, kClosedDocumentsRemain,
		                     "閉じた図面が Vectorworks の中に残っていて片付けられなかったので"
		                     "終了しませんでした");
		return Json();
	}
	// 後処理は一覧が空でなくなると停止する（完了したと返す）ため、後処理の後に再度一覧を参照する。
	// 後処理の間に図面が開かれていれば、上と同様に拒否する（確認から依頼までの間隙を後処理の分だけ
	// 広げない）。
	open = docs.list();
	if (!open.empty()) {
		refuseOpen(open, status);
		return Json();
	}
	// 終了を依頼するのは、応答を書き込んでから受付処理の外で行う（Server が応答を書き込めた場合のみ
	// ServeResult の quit を設定する）。
	status.quit = true;
	return Json::object({{"quitting", Json::boolean(true)}});
}

}
