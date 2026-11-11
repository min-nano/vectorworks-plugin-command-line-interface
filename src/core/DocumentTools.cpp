#include "core/DocumentTools.h"

#include "core/Args.h"
#include "core/Files.h"

#include <algorithm>
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
	int matches = 0;
	const OpenDocument *front = nullptr;
	for (const OpenDocument &open : list) {
		if (!open.path.empty() && open.path == document) {
			target.document = open;
			++matches;
		}
		if (open.active && !front) {
			front = &open;
		}
	}
	if (matches == 0) {
		args::fail(status, ErrorCode::InvalidArgs,
		           std::string(tool) + " の引数が誤っています: 開いている図面に " + document +
		               " がありません（documents の path を渡してください）");
		return target;
	}
	if (matches > 1) {
		args::failWithReason(status, kDocumentAmbiguous,
		                     "パスが " + document + " の図面が " + std::to_string(matches) +
		                         " 枚あるので、どれも" + std::string(undone) + "ませんでした");
		return target;
	}
	target.found = true;
	if (!target.document.active && front) {
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
	bool stillOpen = std::any_of(after.begin(), after.end(), [&](const OpenDocument &open) {
		return open.ref == target.front;
	});
	if (stillOpen) {
		(void)docs.switchTo(target.front);
	}
}

// bringForward は名指した図面を前面にする（前面なら何もしない）。SwitchToOpenFile の戻り値だけを
// 信じず、切り替えたあとの一覧で前面が名指した図面になったことを確かめる（前面が変わらないまま
// 保存・閉じるを呼ぶと、利用者の別の図面に効いてしまう。CLAUDE.md の 5）。前面にできなければ、
// 前面を戻してから internal で失敗する。
bool bringForward(Documents &docs, const Target &target, ToolStatus &status) {
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
	restoreFront(docs, target, after);
	args::fail(status, ErrorCode::Internal,
	           "図面 " + target.document.path + " を前面にできませんでした（SwitchToOpenFile）");
	return false;
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

Json documents(const Json &args, Documents &docs, ToolStatus &status) {
	args::Reader reader(args, kDocumentsSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	Json list = Json::array();
	for (const OpenDocument &open : docs.list()) {
		list.push(Json::object({{"name", Json::string(documentName(open.path))},
		                        {"path", Json::string(open.path)},
		                        {"untitled", Json::boolean(open.untitled)},
		                        {"active", Json::boolean(open.active)}}));
	}
	auto count = static_cast<std::int64_t>(list.items().size());
	return Json::object({{"documents", std::move(list)}, {"count", Json::integer(count)}});
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
	for (const OpenDocument &open : before) {
		openPaths.push_back(open.path);
	}
	if (!save_target::check(to, parent, openPaths, environment.checks, status) ||
	    !bringForward(docs, target, status)) {
		return Json();
	}
	std::uint32_t error = 0;
	try {
		error = docs.saveActive(to);
	} catch (...) {
		// 保存を呼べなかった（SDK の口を作れないなど）。前面を戻してから、Server に internal で
		// 応えさせる。
		restoreFront(docs, target, docs.list());
		throw;
	}
	// 結果の path は、保存の直後に前面になっている項目から、前面を戻す前に読む。
	std::vector<OpenDocument> after = docs.list();
	const OpenDocument *saved = nullptr;
	for (const OpenDocument &open : after) {
		if (open.active) {
			saved = &open;
			break;
		}
	}
	std::string path = saved ? saved->path : std::string();
	// 成否によらず前面を戻す。
	restoreFront(docs, target, after);
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
	if (!target.found || !bringForward(docs, target, status)) {
		return Json();
	}
	docs.closeActive();
	// 戻り値では成否が分からないので、閉じたあとの一覧で決める。
	std::vector<OpenDocument> after = docs.list();
	restoreFront(docs, target, after);
	for (const OpenDocument &open : after) {
		if (open.path == document) {
			args::failWithReason(status, kNotClosed, "図面 " + document + " が閉じませんでした");
			return Json();
		}
	}
	return Json::object({{"closed", Json::boolean(true)}});
}

}
