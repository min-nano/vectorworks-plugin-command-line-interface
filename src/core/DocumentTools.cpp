#include "core/DocumentTools.h"

#include "core/Args.h"
#include "core/Files.h"

#include <algorithm>
#include <iterator>
#include <optional>

namespace VwCli::core {

namespace {

// Target は名指した図面と、そのときの前面の図面。
struct Target {
	// 名指した図面が 1 枚に決まったか（偽なら status に失敗を書いてある）。
	bool found = false;
	OpenDocument document;
	// 名指した図面が前面でなければ、そのときの前面の図面（戻す先）。前面の図面が無ければ空。
	bool hasFront = false;
	std::int32_t front = 0;
};

// findTarget は document に完全に一致する図面を 1 つ探す。無ければ invalid_args、2 枚以上なら
// internal（document_ambiguous）で失敗する。undone は失敗の文言の動詞（「保存し」「閉じ」）。
// パスを読めなかった図面（path が空）は名指せないので、一致の対象から外す（空の document で
// その図面を閉じられないように）。
Target findTarget(const std::vector<OpenDocument> &list, const std::string &document,
                  std::string_view tool, std::string_view undone, ToolStatus &status) {
	Target target;
	std::optional<std::size_t> at = args::findOne(
	    list, [&](const OpenDocument &open) { return !open.path.empty() && open.path == document; },
	    tool,
	    [&] {
		    return "開いている図面に " + document +
			       " がありません（documents の path を渡してください）";
	    },
	    kDocumentAmbiguous,
	    [&](int matches) {
		    return "パスが " + document + " の図面が " + std::to_string(matches) +
			       " 枚あるので、どれも" + std::string(undone) + "ませんでした";
	    },
	    status);
	if (!at) {
		return target;
	}
	target.found = true;
	target.document = list[*at];
	auto front = std::find_if(list.begin(), list.end(),
	                          [](const OpenDocument &open) { return open.active; });
	if (!target.document.active && front != list.end()) {
		target.hasFront = true;
		target.front = front->ref;
	}
	return target;
}

// restoreFront は前面を名指す前の図面へ戻す（まだ開いていれば）。戻せなくても失敗にしない。
void restoreFront(Documents &docs, const Target &target, const std::vector<OpenDocument> &after) {
	if (!target.hasFront) {
		return;
	}
	if (std::ranges::find(after, target.front, &OpenDocument::ref) != after.end()) {
		(void)docs.switchTo(target.front);
	}
}

// FrontGuard は、名指した図面を前面にしてから抜けるまでの間を受け持ち、どの道で抜けても前面を戻す。
// restore を呼ばずに抜けたら（例外で抜けたときも）、壊すときに今の一覧で戻す（利用者の前面の図面が
// 変わったまま残らないように）。壊すときは投げない（一覧を読めなければ戻さない）。
class FrontGuard {
public:
	FrontGuard(Documents &docs, const Target &target) : docs_(docs), target_(target) {}
	FrontGuard(const FrontGuard &) = delete;
	FrontGuard &operator=(const FrontGuard &) = delete;
	~FrontGuard() {
		if (restored_ || !target_.hasFront) {
			return;
		}
		try {
			restoreFront(docs_, target_, docs_.list());
		} catch (...) {
			// 戻せなくても失敗にしない（restoreFront と同じ）。抜ける理由の例外を差し替えない。
			return;
		}
	}

	// restore は after（読み直した一覧）で前面を戻す。1 度だけ戻す（壊すときに戻し直さない）。
	void restore(const std::vector<OpenDocument> &after) {
		restored_ = true;
		restoreFront(docs_, target_, after);
	}

private:
	Documents &docs_;
	const Target &target_;
	bool restored_ = false;
};

// bringForward は名指した図面を前面にする（前面なら何もしない）。SwitchToOpenFile の戻り値だけを
// 信じず、切り替えた後の一覧で前面が名指した図面になったことを確かめる（前面が変わらないまま
// 保存・閉じるを呼ぶと、利用者の別の図面に効いてしまう。CLAUDE.md の 5）。前面にできなければ、
// 前面を戻してから internal で失敗する。
bool bringForward(Documents &docs, const Target &target, FrontGuard &guard, ToolStatus &status) {
	if (target.document.active) {
		return true;
	}
	bool switched = docs.switchTo(target.document.ref);
	std::vector<OpenDocument> after = docs.list();
	bool front = switched && std::any_of(after.begin(), after.end(), [&](const OpenDocument &open) {
		             return open.active && open.ref == target.document.ref &&
					        open.path == target.document.path;
	             });
	if (front) {
		return true;
	}
	guard.restore(after);
	args::fail(status, ErrorCode::Internal,
	           "図面 " + target.document.path + " を前面にできませんでした（SwitchToOpenFile）");
	return false;
}

}

bool clearClosedDocuments(Documents &docs) {
	for (int cleared = 0;; ++cleared) {
		if (!docs.list().empty() || !docs.hasActive()) {
			return true;
		}
		if (cleared == kMaxClosedRemnants) {
			return false;
		}
		docs.closeActive();
	}
}

std::string documentName(std::string_view path) {
	for (std::size_t at = path.size(); at > 0; --at) {
		if (files::isSeparator(path[at - 1])) {
			return std::string(path.substr(at));
		}
	}
	return std::string(path);
}

Json documentJson(const OpenDocument &open) {
	return Json::object({{"name", Json::string(documentName(open.path))},
	                     {"path", Json::string(open.path)},
	                     {"untitled", Json::boolean(open.untitled)}});
}

Json documents(const Json &args, Documents &docs, ToolStatus &status) {
	args::Reader reader(args, kDocumentsSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	Json list = Json::array();
	for (const OpenDocument &open : docs.list()) {
		Json item = documentJson(open);
		item.set("active", Json::boolean(open.active));
		list.push(std::move(item));
	}
	return args::listResult("documents", std::move(list));
}

Json save(const Json &args, Documents &docs, const SaveEnvironment &environment,
          ToolStatus &status) {
	// 1. 引数の形（to の形を含む）。
	args::Reader reader(args, kSaveSpec.name, status);
	reader.onlyKnown({"document", "to"});
	std::string document = reader.requiredString("document");
	std::string to = reader.requiredString("to");
	if (!reader.ok()) {
		return Json();
	}
	std::string problem;
	std::optional<std::string> checked = environment.form(to, problem);
	if (!checked) {
		args::fail(status, ErrorCode::InvalidArgs, "save の引数が誤っています: " + problem);
		return Json();
	}
	std::string parent = std::move(*checked);
	// 2. 図面が無い。
	std::vector<OpenDocument> before = docs.list();
	if (before.empty()) {
		args::noDocument(status);
		return Json();
	}
	// 3. 名指す図面。
	Target target = findTarget(before, document, kSaveSpec.name, "保存し", status);
	if (!target.found) {
		return Json();
	}
	// 4. 保存先の状態（前面を切り替える前に断る）。
	std::vector<std::string> openPaths;
	std::ranges::transform(before, std::back_inserter(openPaths), &OpenDocument::path);
	if (!save_target::check(to, parent, openPaths, environment.checks, status)) {
		return Json();
	}
	// ここから抜けるまで、どの道でも前面を戻す（保存を呼べずに投げた・一覧を読めずに投げたときも。
	// 投げた例外は Server が internal で応える）。
	FrontGuard guard(docs, target);
	if (!bringForward(docs, target, guard, status)) {
		return Json();
	}
	std::uint32_t error = docs.saveActive(to);
	// 結果の path は、保存の直後に前面になっている項目から、前面を戻す前に読む。
	std::vector<OpenDocument> after = docs.list();
	auto active = std::ranges::find_if(after, &OpenDocument::active);
	const bool saved = active != after.end();
	std::string path = saved ? active->path : std::string();
	// 成否によらず前面を戻す。
	guard.restore(after);
	if (error != 0) {
		args::fail(status, ErrorCode::Internal,
		           "保存できませんでした（SaveActiveDocumentPath が " + std::to_string(error) +
		               " を返した）",
		           Json::object({{"reason", Json::string(std::string(kSaveFailed))},
		                         {"gs_error", Json::integer(error)}}));
		return Json();
	}
	if (!saved) {
		args::fail(status, ErrorCode::Internal,
		           "保存しましたが、保存したあとの前面の図面を一覧から読めませんでした");
		return Json();
	}
	return Json::object({{"document", Json::string(document)}, {"path", Json::string(path)}});
}

Json close(const Json &args, Documents &docs, ToolStatus &status) {
	args::Reader reader(args, kCloseSpec.name, status);
	reader.onlyKnown({"document"});
	std::string document = reader.requiredString("document");
	if (!reader.ok()) {
		return Json();
	}
	std::vector<OpenDocument> before = docs.list();
	if (before.empty()) {
		args::noDocument(status);
		return Json();
	}
	Target target = findTarget(before, document, kCloseSpec.name, "閉じ", status);
	if (!target.found) {
		return Json();
	}
	// ここから抜けるまで、どの道でも前面を戻す（save と同じ）。
	FrontGuard guard(docs, target);
	if (!bringForward(docs, target, guard, status)) {
		return Json();
	}
	docs.closeActive();
	// 戻り値では成否が分からないので、閉じた後の一覧で決める。
	std::vector<OpenDocument> after = docs.list();
	guard.restore(after);
	if (std::ranges::find(after, document, &OpenDocument::path) != after.end()) {
		args::failWithReason(status, kNotClosed, "図面 " + document + " が閉じませんでした");
		return Json();
	}
	// 最後の図面を閉じたら、閉じた順のために中に残った図面を片付ける（残すと、利用者がメニューで
	// 終了したときにも、見えない図面の保存の確認が出る）。片付かなくても、名指した図面は閉じたので
	// 失敗にせず、結果に載せて呼ぶ側に知らせる（quit は終了を頼む前にもう一度片付け、片付かなければ
	// 断る）。
	Json result = Json::object({{"closed", Json::boolean(true)}});
	if (after.empty() && !clearClosedDocuments(docs)) {
		result.set(std::string(kClosedDocumentsRemainField), Json::boolean(true));
	}
	return result;
}

}
