#include "core/DocumentTools.h"

#include "core/Args.h"
#include "core/Files.h"

#include <algorithm>
#include <iterator>
#include <optional>

namespace VwCli::core {

namespace {

// Target は指定した図面と、その時点の前面の図面。
struct Target {
	// 指定した図面が 1 枚に決定したか（偽ならば status に失敗を書き込み済み）。
	bool found = false;
	OpenDocument document;
	// 指定した図面が前面でなければ、その時点の前面の図面（戻す先）。前面の図面が無ければ空。
	bool hasFront = false;
	std::int32_t front = 0;
};

// findTarget は document に完全に一致する図面を 1 つ探索する。存在しなければ invalid_args、
// 2 枚以上ならば internal（document_ambiguous）で失敗する。undone は失敗の文言の動詞（「保存し」
// 「閉じ」）。パスを読み取れなかった図面（path が空）は指定できないため、一致の対象から除外する
// （空の document でその図面を閉じられないように）。
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

// restoreFront は前面を指定前の図面へ戻す（まだ開いていれば）。戻せなくても失敗としない。
void restoreFront(Documents &docs, const Target &target, const std::vector<OpenDocument> &after) {
	if (!target.hasFront) {
		return;
	}
	if (std::ranges::find(after, target.front, &OpenDocument::ref) != after.end()) {
		(void)docs.switchTo(target.front);
	}
}

// bringForward は指定した図面を前面にする（前面ならば何もしない）。SwitchToOpenFile の戻り値のみを
// 信頼せず、切り替えた後の一覧で前面が指定した図面になったことを確認する（前面が変わらないまま
// 保存・閉じるを呼び出すと、利用者の別の図面に作用する。CLAUDE.md の 5）。前面にできなければ、
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

// exportFailed は書き出しの失敗（internal。reason は export_failed）。step は失敗した段
// （open・page・close・file）。
void exportFailed(ToolStatus &status, std::string message, std::string_view step, Json detail) {
	detail.set("reason", Json::string(std::string(kExportFailed)));
	detail.set("step", Json::string(std::string(step)));
	args::fail(status, ErrorCode::Internal, std::move(message), std::move(detail));
}

// exportFront は前面にした図面のシートを確かめ、書き出す先の状態を確かめてから書き出す
// （export_pdf の検証の順の 3 の残りと 4、書き出し）。前面は呼び出し元が戻す。
Json exportFront(const std::string &document, const std::vector<std::string> &sheets,
                 const std::string &to, const std::string &parent, SheetPdf &pdf,
                 const SaveEnvironment &environment, ToolStatus &status) {
	// 無い名前を渡すと、ExportPDFPages は失敗を返し、頁の欠けた PDF が残るため、書き出す前に全てを
	// 確かめる（Findings「PDF Export」の「失敗の見え方」）。デザインレイヤの名前も断る。
	std::vector<std::string> present = pdf.sheets();
	for (const std::string &sheet : sheets) {
		if (std::ranges::find(present, sheet) == present.end()) {
			args::fail(status, ErrorCode::InvalidArgs,
			           std::string(kExportPdfSpec.name) + " の引数が誤っています: 図面 " +
			               document + " にシートレイヤ " + sheet +
			               " がありません（layers の kind が sheet の name を渡してください）");
			return Json();
		}
	}
	// 開いている図面は .vwx なので、.pdf の to と比べない（to_is_open は返さない）。
	if (!save_target::check(to, parent, {}, environment.checks, status)) {
		return Json();
	}
	std::uint32_t error = pdf.open(to);
	if (error != 0) {
		exportFailed(status,
		             "PDF を開けませんでした（OpenPDFDocument が " + std::to_string(error) +
		                 " を返した）",
		             "open", Json::object({{"vcom_error", Json::integer(error)}}));
		return Json();
	}
	std::optional<std::string> failedSheet;
	try {
		for (const std::string &sheet : sheets) {
			error = pdf.page(sheet);
			if (error != 0) {
				failedSheet = sheet;
				break;
			}
		}
	} catch (...) {
		// 開いた PDF を閉じ、設定を戻してから、Server に internal で応答させる。
		(void)pdf.close();
		throw;
	}
	std::uint32_t closed = pdf.close();
	// 途中で失敗しても、書きかけの PDF は消さない（CLAUDE.md の 9）。
	if (failedSheet) {
		exportFailed(status,
		             "シート " + *failedSheet + " を書き出せませんでした（ExportPDFPages が " +
		                 std::to_string(error) + " を返した）。" + to +
		                 " に書きかけの PDF が残っていることがあります",
		             "page",
		             Json::object({{"sheet", Json::string(*failedSheet)},
		                           {"vcom_error", Json::integer(error)}}));
		return Json();
	}
	if (closed != 0) {
		exportFailed(status,
		             "PDF を書き終えられませんでした（ClosePDFDocument が " +
		                 std::to_string(closed) + " を返した）",
		             "close", Json::object({{"vcom_error", Json::integer(closed)}}));
		return Json();
	}
	// 戻り値が成功でもファイルができないことがあるため、書き終えた後に確かめる（同じ節）。
	save_target::Probe written = environment.checks.to(to);
	if (written.entry != save_target::Entry::File) {
		Json detail = Json::object();
		if (written.entry == save_target::Entry::Failed) {
			detail.set("os_error", Json::integer(written.osError));
		}
		exportFailed(status, "書き出しは成功を返しましたが、" + to + " にファイルがありません",
		             "file", std::move(detail));
		return Json();
	}
	return Json::object({{"document", Json::string(document)},
	                     {"path", Json::string(to)},
	                     {"pages", Json::integer(static_cast<std::int64_t>(sheets.size()))}});
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
	// 1. 引数の形式（to の形式を含む）。
	args::Reader reader(args, kSaveSpec.name, status);
	reader.onlyKnown({"document", "to"});
	std::string document = reader.requiredString("document");
	std::string to = reader.requiredString("to");
	if (!reader.ok()) {
		return Json();
	}
	std::string problem;
	std::optional<std::string> checked = environment.form(to, save_target::kVwxExtension, problem);
	if (!checked) {
		args::fail(status, ErrorCode::InvalidArgs, "save の引数が誤っています: " + problem);
		return Json();
	}
	std::string parent = std::move(*checked);
	// 2. 図面が存在しない。
	std::vector<OpenDocument> before = docs.list();
	if (before.empty()) {
		args::noDocument(status);
		return Json();
	}
	// 3. 指定する図面。
	Target target = findTarget(before, document, kSaveSpec.name, "保存し", status);
	if (!target.found) {
		return Json();
	}
	// 4. 保存先の状態（前面を切り替える前に拒否する）。
	std::vector<std::string> openPaths;
	std::ranges::transform(before, std::back_inserter(openPaths), &OpenDocument::path);
	if (!save_target::check(to, parent, openPaths, environment.checks, status) ||
	    !bringForward(docs, target, status)) {
		return Json();
	}
	std::uint32_t error = 0;
	try {
		error = docs.saveActive(to);
	} catch (...) {
		// 保存を呼び出せなかった（SDK のインターフェースを生成できないなど）。前面を戻してから、
		// Server に internal で応答させる。
		restoreFront(docs, target, docs.list());
		throw;
	}
	// 結果の path は、保存の直後に前面になっている項目から、前面を戻す前に読み取る。
	std::vector<OpenDocument> after = docs.list();
	auto active = std::ranges::find_if(after, &OpenDocument::active);
	const bool saved = active != after.end();
	std::string path = saved ? active->path : std::string();
	// 成否に関わらず前面を戻す。
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
	// 戻り値では成否が判明しないため、閉じた後の一覧で判定する。
	std::vector<OpenDocument> after = docs.list();
	restoreFront(docs, target, after);
	if (std::ranges::find(after, document, &OpenDocument::path) != after.end()) {
		args::failWithReason(status, kNotClosed, "図面 " + document + " が閉じませんでした");
		return Json();
	}
	// 最後の図面を閉じた場合は、閉じた順序に起因して内部に残存した図面を後処理する（残置すると、
	// 利用者がメニューで終了した場合にも、表示されない図面の保存の確認が表示される）。後処理が
	// 完了しなくても、指定した図面は閉じたため失敗とせず、結果に含めて呼び出し元に通知する（quit は
	// 終了を要求する前に再度後処理し、完了しなければ拒否する）。
	Json result = Json::object({{"closed", Json::boolean(true)}});
	if (after.empty() && !clearClosedDocuments(docs)) {
		result.set(std::string(kClosedDocumentsRemainField), Json::boolean(true));
	}
	return result;
}

Json exportPdf(const Json &args, Documents &docs, SheetPdf &pdf, const SaveEnvironment &environment,
               ToolStatus &status) {
	// 1. 引数の形式（to の形式を含む）。
	args::Reader reader(args, kExportPdfSpec.name, status);
	reader.onlyKnown({"document", "sheets", "to"});
	std::string document = reader.requiredString("document");
	std::vector<std::string> sheets = reader.requiredStrings("sheets");
	std::string to = reader.requiredString("to");
	if (!reader.ok()) {
		return Json();
	}
	std::string problem;
	std::optional<std::string> checked = environment.form(to, save_target::kPdfExtension, problem);
	if (!checked) {
		args::fail(status, ErrorCode::InvalidArgs,
		           std::string(kExportPdfSpec.name) + " の引数が誤っています: " + problem);
		return Json();
	}
	// 2. 図面が存在しない。
	std::vector<OpenDocument> before = docs.list();
	if (before.empty()) {
		args::noDocument(status);
		return Json();
	}
	// 3. 指定する図面。シートレイヤは前面の図面からしか読めず、前面でない図面のシートは書き出せない
	// ため、前面にしてから確かめる（Findings「PDF Export」）。
	Target target = findTarget(before, document, kExportPdfSpec.name, "書き出し", status);
	if (!target.found || !bringForward(docs, target, status)) {
		return Json();
	}
	Json result;
	try {
		result = exportFront(document, sheets, to, *checked, pdf, environment, status);
	} catch (...) {
		restoreFront(docs, target, docs.list());
		throw;
	}
	// 成否に関わらず前面を戻す。
	restoreFront(docs, target, docs.list());
	return result;
}

}
