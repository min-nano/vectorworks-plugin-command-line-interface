#include "FakeDocuments.h"
#include "TestSupport.h"

#include "core/DocumentTools.h"
#include "core/Files.h"
#include "core/Protocol.h"
#include "core/SaveTarget.h"
#include "core/Serve.h"

#include <cstddef>
#include <cstdint>
#include <memory>
#include <optional>
#include <span>
#include <stdexcept>
#include <string>
#include <string_view>
#include <utility>
#include <vector>

// 図面を操作するツール（documents・save・close）の内 SDK に依存しない部分
// （docs/plugin/tools.md「図面を操作するツール」）。SDK の代わりに擬似 Documents を渡し、
// 呼び出した順を検証する。

namespace save_target = VwCli::core::save_target;
using save_target::Entry;
using save_target::Probe;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::OpenDocument;
using VwCli::core::SaveEnvironment;
using VwCli::core::ToolKind;
using VwCli::core::ToolStatus;
using VwCli::test::FakeDocuments;
using VwCli::test::must;
using VwCli::test::reasonOf;

namespace {

constexpr std::string_view kUntitled = "/Applications/Vectorworks 2026/名称未設定 1";

// sample は 3 枚の図面: 保存済みの plan.vwx（前面）・無題・別のフォルダの plan.vwx。
FakeDocuments sample() {
	FakeDocuments docs;
	docs.open = {{"/Users/me/plan.vwx", false, true, 11},
	             {std::string(kUntitled), true, false, 22},
	             {"/Users/me/old/plan.vwx", false, false, 33}};
	return docs;
}

// Fs は差し替えのファイルシステム（存在しない・フォルダがある、を名前で決定する）と比較の方法。
struct Fs {
	std::vector<std::string> files;
	std::vector<std::string> folders = {"/Users/me/tmp", "/"};
	std::optional<Probe> toFails;
	std::vector<std::string> asked;

	SaveEnvironment environment(save_target::FormCheck form = &save_target::checkMacTo) {
		auto has = [](const std::vector<std::string> &names, const std::string &path) {
			for (const std::string &name : names) {
				if (name == path) {
					return true;
				}
			}
			return false;
		};
		return SaveEnvironment{
		    form,
		    {[](std::string_view a, std::string_view b) {
			     // 差し替えの比較の方法: ASCII の大文字と小文字のみを畳む。
			     if (a.size() != b.size()) {
				     return false;
			     }
			     for (std::size_t i = 0; i < a.size(); ++i) {
				     auto lower = [](char c) {
					     return c >= 'A' && c <= 'Z' ? static_cast<char>(c - 'A' + 'a') : c;
				     };
				     if (lower(a[i]) != lower(b[i])) {
					     return false;
				     }
			     }
			     return true;
		     },
			 [this, has](const std::string &path) {
			     asked.push_back("to " + path);
			     if (toFails) {
				     return *toFails;
			     }
			     if (has(files, path)) {
				     return Probe{Entry::File, 0};
			     }
			     return Probe{has(folders, path) ? Entry::Directory : Entry::Missing, 0};
		     },
			 [this, has](const std::string &path) {
			     asked.push_back("parent " + path);
			     if (has(folders, path)) {
				     return Probe{Entry::Directory, 0};
			     }
			     return Probe{has(files, path) ? Entry::File : Entry::Missing, 0};
		     }}};
	}
};

Json saveArgs(std::string_view document, std::string_view to) {
	return Json::object(
	    {{"document", Json::string(std::string(document))}, {"to", Json::string(std::string(to))}});
}

std::vector<std::string> calls(std::initializer_list<const char *> names) {
	return std::vector<std::string>(names.begin(), names.end());
}

}

// save・close は占有の中でのみ呼び出せる App（Read で登録すると、占有なしで保存・閉じられる）。
// 種類は定数で決定される（ツールの一覧は定数で初期化される）。
static_assert(VwCli::core::kSaveSpec.kind == ToolKind::App);
static_assert(VwCli::core::kCloseSpec.kind == ToolKind::App);

TEST(DocumentToolKinds) {
	CHECK(VwCli::core::kDocumentsSpec.kind == ToolKind::Read);
	CHECK(VwCli::core::kSaveSpec.kind == ToolKind::App);
	CHECK(VwCli::core::kCloseSpec.kind == ToolKind::App);
	for (const auto *spec :
	     {&VwCli::core::kDocumentsSpec, &VwCli::core::kSaveSpec, &VwCli::core::kCloseSpec}) {
		CHECK(must(spec->inputSchema).isObject());
	}
}

TEST(DocumentsListsOpenDocumentsInListOrder) {
	FakeDocuments docs = sample();
	ToolStatus status;
	Json result = VwCli::core::documents(Json::object(), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(
	    result.dump(),
	    std::string(
	        R"({"documents":[)"
	        R"({"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false,"active":true},)"
	        R"({"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1","untitled":true,"active":false},)"
	        R"({"name":"plan.vwx","path":"/Users/me/old/plan.vwx","untitled":false,"active":false}],"count":3})"));
}

// 図面が無くても失敗とせず、空の一覧を返す。
TEST(DocumentsIsEmptyWithoutDocuments) {
	FakeDocuments docs;
	ToolStatus status;
	Json result = VwCli::core::documents(Json::object(), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, must(R"({"documents":[],"count":0})"));
	ToolStatus refused;
	VwCli::core::documents(must(R"({"all":true})"), docs, refused);
	CHECK(refused.code == ErrorCode::InvalidArgs);
}

TEST(DocumentNameIsAfterTheLastSeparator) {
	CHECK_EQ(VwCli::core::documentName("/Users/me/plan.vwx"), std::string("plan.vwx"));
	CHECK_EQ(VwCli::core::documentName("plan.vwx"), std::string("plan.vwx"));
	CHECK_EQ(VwCli::core::documentName("/a/"), std::string(""));
#ifdef _WIN32
	CHECK_EQ(VwCli::core::documentName("C:\\Users\\me\\plan.vwx"), std::string("plan.vwx"));
#endif
}

// 前面でない図面は前面にしてから保存し、保存の直後の一覧から path を読み取ってから前面を戻す。
TEST(SaveSavesTheNamedDocumentAndRestoresTheFront) {
	FakeDocuments docs = sample();
	Fs fs;
	ToolStatus status;
	Json result = VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs,
	                                fs.environment(), status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, Json::object({{"document", Json::string(std::string(kUntitled))},
	                               {"path", Json::string("/Users/me/tmp/a.vwx")}}));
	CHECK(docs.calls ==
	      calls({"list", "switch 22", "list", "save /Users/me/tmp/a.vwx", "list", "switch 11"}));
	CHECK(docs.open[0].active);
	std::vector<std::string> asked = {"to /Users/me/tmp/a.vwx", "parent /Users/me/tmp"};
	CHECK(fs.asked == asked);
}

// 前面の図面を保存する場合は切り替えない。保存済みの図面も、同じ図面の 2 回目も新しい to へ
// 保存できる。
TEST(SaveSavesTheFrontDocumentWithoutSwitching) {
	FakeDocuments docs = sample();
	Fs fs;
	ToolStatus status;
	Json result = VwCli::core::save(saveArgs("/Users/me/plan.vwx", "/Users/me/tmp/a.vwx"), docs,
	                                fs.environment(), status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("path"), Json::string("/Users/me/tmp/a.vwx"));
	CHECK(docs.calls == calls({"list", "save /Users/me/tmp/a.vwx", "list"}));

	// 2 回目: 新しい to なら通過し、直前の保存先は開いている図面のパスであるため to_is_open。
	ToolStatus again;
	result = VwCli::core::save(saveArgs("/Users/me/tmp/a.vwx", "/Users/me/tmp/b.vwx"), docs,
	                           fs.environment(), again);
	CHECK(again.code == ErrorCode::None);
	CHECK_EQ(*result.find("document"), Json::string("/Users/me/tmp/a.vwx"));
	CHECK_EQ(*result.find("path"), Json::string("/Users/me/tmp/b.vwx"));
	ToolStatus previous;
	VwCli::core::save(saveArgs("/Users/me/tmp/b.vwx", "/Users/me/tmp/b.vwx"), docs,
	                  fs.environment(), previous);
	CHECK_EQ(reasonOf(previous), std::string("to_is_open"));
}

// 結果の path は to をそのまま返さず、保存の直後の一覧から読み取る。
TEST(SaveReportsThePathTheListReturns) {
	class Renaming : public FakeDocuments {
	public:
		std::uint32_t saveActive(const std::string &path) override {
			std::uint32_t error = FakeDocuments::saveActive(path);
			for (OpenDocument &document : open) {
				if (document.active) {
					document.path = "/Users/ME/tmp/a.vwx";
				}
			}
			return error;
		}
	};
	Renaming docs;
	docs.open = sample().open;
	Fs fs;
	ToolStatus status;
	Json result = VwCli::core::save(saveArgs("/Users/me/plan.vwx", "/Users/me/tmp/a.vwx"), docs,
	                                fs.environment(), status);
	CHECK_EQ(*result.find("path"), Json::string("/Users/ME/tmp/a.vwx"));
}

TEST(SaveChecksTheFormOfTo) {
	for (const char *to : {"tmp/a.vwx", "/Users/me/tmp/a.txt", "/Users/me/tmp/../a.vwx",
	                       "/Users/me/./a.vwx", "/Users/me//a.vwx", "/Users/me/tmp/"}) {
		FakeDocuments docs;
		Fs fs;
		ToolStatus status;
		Json result = VwCli::core::save(saveArgs(kUntitled, to), docs, fs.environment(), status);
		// 図面が無くても、引数の形式を先に返す。
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(result.isNull());
		CHECK(docs.calls.empty());
		CHECK(fs.asked.empty());
	}
	// Windows の規則を渡せば、/ を含む to を拒否する。
	FakeDocuments docs = sample();
	Fs fs;
	ToolStatus status;
	VwCli::core::save(saveArgs(kUntitled, "C:/x/a.vwx"), docs,
	                  fs.environment(&save_target::checkWindowsTo), status);
	CHECK(status.code == ErrorCode::InvalidArgs);
	CHECK(docs.calls.empty());
}

TEST(SaveRejectsBadArguments) {
	for (const char *args :
	     {R"({})", R"({"document":"/Users/me/plan.vwx"})", R"({"to":"/Users/me/tmp/a.vwx"})",
	      R"({"document":1,"to":"/Users/me/tmp/a.vwx"})",
	      R"({"document":"/Users/me/plan.vwx","to":null})",
	      R"({"document":"/Users/me/plan.vwx","to":"/Users/me/tmp/a.vwx","x":1})"}) {
		FakeDocuments docs;
		Fs fs;
		ToolStatus status;
		VwCli::core::save(must(args), docs, fs.environment(), status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(docs.calls.empty());
	}
}

TEST(SaveAndCloseFailWithoutDocuments) {
	FakeDocuments docs;
	Fs fs;
	ToolStatus saveStatus;
	VwCli::core::save(saveArgs("/Users/me/plan.vwx", "/Users/me/tmp/a.vwx"), docs, fs.environment(),
	                  saveStatus);
	CHECK(saveStatus.code == ErrorCode::Internal);
	CHECK_EQ(saveStatus.detail, must(R"({"reason":"no_document"})"));
	CHECK(fs.asked.empty());
	ToolStatus closeStatus;
	VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, closeStatus);
	CHECK(closeStatus.code == ErrorCode::Internal);
	CHECK_EQ(closeStatus.detail, must(R"({"reason":"no_document"})"));
	CHECK(docs.calls == calls({"list", "list"}));
}

// 指定する図面が無ければ invalid_args（完全に一致するもののみ）。
TEST(SaveAndCloseRejectADocumentThatIsNotOpen) {
	for (const char *document : {"/Users/me/Plan.vwx", "/Users/me/plan", "plan.vwx", ""}) {
		FakeDocuments docs = sample();
		Fs fs;
		ToolStatus status;
		VwCli::core::save(saveArgs(document, "/Users/me/tmp/a.vwx"), docs, fs.environment(),
		                  status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(status.detail.isNull());
		ToolStatus closeStatus;
		VwCli::core::close(Json::object({{"document", Json::string(document)}}), docs, closeStatus);
		CHECK(closeStatus.code == ErrorCode::InvalidArgs);
		CHECK(docs.calls == calls({"list", "list"}));
		CHECK(fs.asked.empty());
	}
}

// 2 枚が一致すれば、untitled が異なっても、状態の検証より先に、保存・閉じるを呼び出す前に拒否する。
TEST(SaveAndCloseRefuseAnAmbiguousDocument) {
	FakeDocuments docs;
	docs.open = {{"/Applications/Vectorworks 2026/plan v2026.vwx", true, false, 1},
	             {"/Users/me/x.vwx", false, true, 2},
	             {"/Applications/Vectorworks 2026/plan v2026.vwx", false, false, 3}};
	Fs fs;
	// to が開いている図面と一致していても、document_ambiguous を先に返す。
	ToolStatus status;
	VwCli::core::save(saveArgs("/Applications/Vectorworks 2026/plan v2026.vwx", "/Users/me/x.vwx"),
	                  docs, fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"document_ambiguous"})"));
	ToolStatus closeStatus;
	VwCli::core::close(must(R"({"document":"/Applications/Vectorworks 2026/plan v2026.vwx"})"),
	                   docs, closeStatus);
	CHECK_EQ(closeStatus.detail, must(R"({"reason":"document_ambiguous"})"));
	CHECK(docs.calls == calls({"list", "list"}));
	CHECK(status.message.find("どれも保存しませんでした") != std::string::npos);
	CHECK(closeStatus.message.find("どれも閉じませんでした") != std::string::npos);
	CHECK(fs.asked.empty());
}

// 状態の失敗は前面を切り替える前・保存を呼ぶ前に返す。
TEST(SaveRefusesTargetsBeforeSaving) {
	struct Case {
		const char *to;
		const char *reason;
	};
	for (const Case &c :
	     {Case{"/USERS/ME/PLAN.VWX", "to_is_open"}, Case{"/Users/me/tmp/exists.vwx", "file_exists"},
	      Case{"/Users/me/tmp/folder.vwx", "file_exists"},
	      Case{"/Users/me/none/a.vwx", "parent_missing"},
	      Case{"/Users/me/tmp/exists.vwx/a.vwx", "parent_missing"}, Case{"/a.vwx", ""}}) {
		FakeDocuments docs = sample();
		Fs fs;
		fs.files = {"/Users/me/tmp/exists.vwx"};
		fs.folders.push_back("/Users/me/tmp/folder.vwx");
		ToolStatus status;
		VwCli::core::save(saveArgs(kUntitled, c.to), docs, fs.environment(), status);
		CHECK_EQ(reasonOf(status), std::string(c.reason));
		if (*c.reason != '\0') {
			CHECK(status.code == ErrorCode::Internal);
			CHECK(docs.calls == calls({"list"}));
		} else {
			// ルートの直下の親はルートとして検証する。
			CHECK(status.code == ErrorCode::None);
			CHECK_EQ(fs.asked.back(), std::string("parent /"));
		}
	}
	// 開いている図面と一致する to は、親のフォルダが削除されていても to_is_open。
	FakeDocuments docs = sample();
	Fs fs;
	fs.folders.clear();
	ToolStatus status;
	VwCli::core::save(saveArgs(kUntitled, "/Users/me/old/plan.vwx"), docs, fs.environment(),
	                  status);
	CHECK_EQ(reasonOf(status), std::string("to_is_open"));
	CHECK(fs.asked.empty());
}

TEST(SaveReportsChecksThatFail) {
	FakeDocuments docs = sample();
	Fs fs;
	fs.toFails = Probe{Entry::Failed, 53};
	ToolStatus status;
	VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs, fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"to_check_failed","check":"to","os_error":53})"));
	CHECK(docs.calls == calls({"list"}));
}

// SaveActiveDocumentPath が 0 以外を返しても、前面を戻してから save_failed で返す。
TEST(SaveReportsTheSdkError) {
	FakeDocuments docs = sample();
	docs.saveError = 50331649;
	Fs fs;
	ToolStatus status;
	Json result = VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs,
	                                fs.environment(), status);
	CHECK(result.isNull());
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"save_failed","gs_error":50331649})"));
	CHECK(docs.calls ==
	      calls({"list", "switch 22", "list", "save /Users/me/tmp/a.vwx", "list", "switch 11"}));
}

TEST(SaveFailsWhenItCannotSwitch) {
	FakeDocuments docs = sample();
	docs.switchFails = true;
	Fs fs;
	ToolStatus status;
	VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs, fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(docs.calls == calls({"list", "switch 22", "list", "switch 11"}));
}

// SwitchToOpenFile が真を返しても前面が指定した図面にならなければ、保存も閉じるも呼び出さずに
// 拒否する（利用者が前面に開いている別の図面を保存・閉じない）。
TEST(SaveAndCloseCheckTheFrontAfterSwitching) {
	FakeDocuments docs = sample();
	docs.switchIgnored = true;
	Fs fs;
	ToolStatus saveStatus;
	VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs, fs.environment(),
	                  saveStatus);
	CHECK(saveStatus.code == ErrorCode::Internal);
	CHECK(saveStatus.detail.isNull());
	CHECK(docs.calls == calls({"list", "switch 22", "list", "switch 11"}));
	docs.calls.clear();
	ToolStatus closeStatus;
	VwCli::core::close(must(R"({"document":"/Users/me/old/plan.vwx"})"), docs, closeStatus);
	CHECK(closeStatus.code == ErrorCode::Internal);
	CHECK(docs.calls == calls({"list", "switch 33", "list", "switch 11"}));
	CHECK_EQ(docs.open.size(), std::size_t{3});
	CHECK_EQ(docs.open[0].path, std::string("/Users/me/plan.vwx"));
}

// パスを読み取れなかった図面（path が空）は指定できない。空の document は、そうした図面が 1 枚でも
// 2 枚でも invalid_args で拒否する（document_ambiguous にも、その図面を閉じることにもならない）。
TEST(SaveAndCloseDoNotMatchDocumentsWithoutAPath) {
	for (int unreadable : {1, 2}) {
		FakeDocuments docs = sample();
		for (int i = 0; i < unreadable; ++i) {
			docs.open.push_back({"", true, false, 40 + i});
		}
		ToolStatus closeStatus;
		VwCli::core::close(must(R"({"document":""})"), docs, closeStatus);
		CHECK(closeStatus.code == ErrorCode::InvalidArgs);
		CHECK(docs.calls == calls({"list"}));
		Fs fs;
		ToolStatus saveStatus;
		VwCli::core::save(saveArgs("", "/Users/me/tmp/a.vwx"), docs, fs.environment(), saveStatus);
		CHECK(saveStatus.code == ErrorCode::InvalidArgs);
		CHECK_EQ(docs.open.size(), std::size_t{3} + static_cast<std::size_t>(unreadable));
	}
}

TEST(SaveFailsWhenTheFrontIsNotListed) {
	FakeDocuments docs = sample();
	docs.saveLosesFront = true;
	Fs fs;
	ToolStatus status;
	VwCli::core::save(saveArgs("/Users/me/plan.vwx", "/Users/me/tmp/a.vwx"), docs, fs.environment(),
	                  status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(status.detail.isNull());
}

// 前面でない図面を閉じたら、前の前面へ戻す。
TEST(CloseClosesTheNamedDocumentAndRestoresTheFront) {
	FakeDocuments docs = sample();
	ToolStatus status;
	Json result =
	    VwCli::core::close(must(R"({"document":"/Users/me/old/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, must(R"({"closed":true})"));
	CHECK(docs.calls == calls({"list", "switch 33", "list", "close", "list", "switch 11"}));
	REQUIRE(docs.open.size() == 2);
	CHECK(docs.open[0].active);
}

// 前面を閉じたら戻せない（Vectorworks が選択した前面のまま）。
TEST(CloseClosesTheFrontDocument) {
	FakeDocuments docs = sample();
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(docs.calls == calls({"list", "close", "list"}));
	CHECK_EQ(docs.open.size(), std::size_t{2});
}

// 前の前面が既に開いていなければ、戻さない。
TEST(CloseDoesNotRestoreAFrontThatIsGone) {
	FakeDocuments docs = sample();
	docs.closeAlsoDrops = 11;
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/old/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(docs.calls == calls({"list", "switch 33", "list", "close", "list"}));
}

TEST(CloseFailsWhenItCannotSwitch) {
	FakeDocuments docs = sample();
	docs.switchFails = true;
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/old/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(status.detail.isNull());
	CHECK(docs.calls == calls({"list", "switch 33", "list", "switch 11"}));
	CHECK_EQ(docs.open.size(), std::size_t{3});
}

TEST(CloseReportsADocumentThatStaysOpen) {
	FakeDocuments docs = sample();
	docs.closeKeeps = true;
	ToolStatus status;
	VwCli::core::close(Json::object({{"document", Json::string(std::string(kUntitled))}}), docs,
	                   status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"not_closed"})"));
	CHECK(docs.calls == calls({"list", "switch 22", "list", "close", "list", "switch 11"}));
}

// 最後の図面を閉じたら、閉じた順のために内部に残存した図面を、一覧が空であることを確認しながら
// 1 枚ずつ消す（#321）。
TEST(CloseClearsClosedDocumentsLeftInsideAfterTheLast) {
	FakeDocuments docs;
	docs.open = {{"/Users/me/plan.vwx", false, true, 11}};
	docs.remnants = 2;
	ToolStatus status;
	Json result = VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, must(R"({"closed":true})"));
	CHECK_EQ(docs.remnants, 0);
	CHECK(docs.calls == calls({"list", "close", "list", "list", "active", "close", "list", "active",
	                           "close", "list", "active"}));
}

// 図面が開いている間は、残存した図面を後処理しない（CloseDocument が開いている図面を閉じて
// しまう）。
TEST(CloseDoesNotClearWhileDocumentsAreOpen) {
	FakeDocuments docs = sample();
	docs.remnants = 1;
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(docs.calls == calls({"list", "close", "list"}));
	CHECK_EQ(docs.remnants, 1);
}

// 後処理が完了しなくても、指定した図面は閉じたため失敗とせず、結果に closed_documents_remain を
// 含める。CloseDocument を呼び出す回数には上限がある。
TEST(CloseStopsClearingAtTheLimit) {
	FakeDocuments docs;
	docs.open = {{"/Users/me/plan.vwx", false, true, 11}};
	docs.remnants = 1;
	docs.remnantsStick = true;
	ToolStatus status;
	Json result = VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, must(R"({"closed":true,"closed_documents_remain":true})"));
	CHECK_EQ(docs.count("close"), 1 + VwCli::core::kMaxClosedRemnants);
}

TEST(ClearClosedDocumentsDoesNothingWithoutRemnants) {
	FakeDocuments docs;
	CHECK(VwCli::core::clearClosedDocuments(docs));
	CHECK(docs.calls == calls({"list", "active"}));
	FakeDocuments open = sample();
	open.remnants = 1;
	CHECK(VwCli::core::clearClosedDocuments(open));
	CHECK(open.calls == calls({"list"}));
}

TEST(CloseRejectsBadArguments) {
	for (const char *args :
	     {R"({})", R"({"document":true})", R"({"document":"/a.vwx","save":false})"}) {
		FakeDocuments docs = sample();
		ToolStatus status;
		VwCli::core::close(must(args), docs, status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(docs.calls.empty());
	}
}

namespace {

namespace files = VwCli::core::files;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::test::Caller;
using VwCli::test::kNow;
using VwCli::test::TempDir;

int gRuns = 0;

Json counts(const Json &, const ToolContext &, ToolStatus &) {
	++gRuns;
	return Json::object();
}

// serveOnce は要求 1 つを配置して受付処理を 1 回実行し、応答を返す。
Json serveOnce(Server &server, Caller &caller, std::span<const Tool> tools, const std::string &id,
               const std::string &request) {
	caller.put(id, request);
	server.serve(tools, kNow);
	Json response = caller.response(id);
	CHECK(!response.isNull());
	return response;
}

}

// ツールの一覧と同じ種類で登録した save・close は、セッション ID なしなら no_session、
// 他のセッション ID なら occupied で、実行されずに拒否される。
TEST(ServeRefusesSaveAndCloseOutsideTheSession) {
	const Tool tools[] = {VwCli::core::toolOf(VwCli::core::kSaveSpec, &counts),
	                      VwCli::core::toolOf(VwCli::core::kCloseSpec, &counts)};
	TempDir dir;
	Caller caller(dir);
	Server server(dir.utf8Path());
	gRuns = 0;
	for (const char *tool : {"save", "close"}) {
		Json refused = serveOnce(server, caller, tools, std::string("a") + tool,
		                         std::string(R"({"tool":")") + tool + "\"}");
		CHECK_EQ(*refused.find("code"), Json::string("no_session"));
	}
	Json started = serveOnce(server, caller, tools, "b", R"({"tool":"@session_start"})");
	REQUIRE(started.find("result"));
	std::string session = started.find("result")->find("session")->asString();
	for (const char *tool : {"save", "close"}) {
		Json other = serveOnce(server, caller, tools, std::string("c") + tool,
		                       std::string(R"({"tool":")") + tool + R"(","session":"other"})");
		CHECK_EQ(*other.find("code"), Json::string("occupied"));
		Json none = serveOnce(server, caller, tools, std::string("d") + tool,
		                      std::string(R"({"tool":")") + tool + "\"}");
		CHECK_EQ(*none.find("code"), Json::string("occupied"));
	}
	CHECK_EQ(gRuns, 0);
	Json allowed =
	    serveOnce(server, caller, tools, "e", R"({"tool":"save","session":")" + session + "\"}");
	CHECK_EQ(*allowed.find("ok"), Json::boolean(true));
	CHECK_EQ(gRuns, 1);
}

// 保存を呼び出せずに投げた場合も、前面を戻してから投げる（Server が internal で応答する）。
TEST(SaveRestoresTheFrontWhenSavingThrows) {
	class Throwing : public FakeDocuments {
	public:
		std::uint32_t saveActive(const std::string &path) override {
			calls.push_back("save " + path);
			throw std::runtime_error("no file identifier");
		}
	};
	Throwing docs;
	docs.open = sample().open;
	Fs fs;
	ToolStatus status;
	bool threw = false;
	try {
		VwCli::core::save(saveArgs(kUntitled, "/Users/me/tmp/a.vwx"), docs, fs.environment(),
		                  status);
	} catch (const std::runtime_error &) {
		threw = true;
	}
	CHECK(threw);
	CHECK(docs.calls ==
	      calls({"list", "switch 22", "list", "save /Users/me/tmp/a.vwx", "list", "switch 11"}));
}

namespace {

using VwCli::core::SheetPdf;

// FakePdf は IExportPDF の代わり。呼び出しを図面の呼び出しと同じ列（docs.calls）に記録し、
// 書き終えたら差し替えのファイルシステムにファイルを置く。
class FakePdf : public SheetPdf {
public:
	FakePdf(std::vector<std::string> &log, Fs &fs) : log_(log), fs_(fs) {}

	std::vector<std::string> names = {"S-1", "S-2", "S-3"};
	std::uint32_t openError = 0;
	std::uint32_t closeError = 0;
	// この名前の頁で失敗する（1 を返す。無い名前を渡した場合の ExportPDFPages と同じ）。
	std::string failsAt;
	// この名前の頁で投げる。
	std::string throwsAt;
	// 書き終えてもファイルを置かない（出力先のフォルダが無い場合を模倣する）。
	bool writes = true;
	// 書き終えた後の to の問い合わせを失敗させる。
	std::optional<Probe> probeAfterClose;

	std::vector<std::string> sheets() override {
		log_.emplace_back("sheets");
		return names;
	}
	std::uint32_t open(const std::string &path) override {
		log_.push_back("open " + path);
		path_ = path;
		return openError;
	}
	std::uint32_t page(const std::string &sheet) override {
		log_.push_back("page " + sheet);
		if (sheet == throwsAt) {
			throw std::runtime_error("page");
		}
		return sheet == failsAt ? 1 : 0;
	}
	std::uint32_t close() override {
		log_.emplace_back("close pdf");
		if (writes) {
			fs_.files.push_back(path_);
		}
		if (probeAfterClose) {
			fs_.toFails = probeAfterClose;
		}
		return closeError;
	}

private:
	std::vector<std::string> &log_;
	Fs &fs_;
	std::string path_;
};

Json exportArgs(std::string_view document, std::initializer_list<const char *> sheets,
                std::string_view to) {
	Json list = Json::array();
	for (const char *sheet : sheets) {
		list.push(Json::string(sheet));
	}
	return Json::object({{"document", Json::string(std::string(document))},
	                     {"sheets", std::move(list)},
	                     {"to", Json::string(std::string(to))}});
}

}

static_assert(VwCli::core::kExportPdfSpec.kind == ToolKind::App);

TEST(ExportPdfSpec) {
	CHECK_EQ(std::string(VwCli::core::kExportPdfSpec.name), std::string("export_pdf"));
	CHECK(must(VwCli::core::kExportPdfSpec.inputSchema).isObject());
}

// 前面でない図面は前面にしてからシートを確かめ、sheets の順に書き出し、書き終えたファイルを
// 確かめてから前面を戻す。
TEST(ExportPdfWritesTheSheetsInOrderAndRestoresTheFront) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	Json result =
	    VwCli::core::exportPdf(exportArgs(kUntitled, {"S-3", "S-1"}, "/Users/me/tmp/a.pdf"), docs,
		                       pdf, fs.environment(), status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, Json::object({{"document", Json::string(std::string(kUntitled))},
	                               {"path", Json::string("/Users/me/tmp/a.pdf")},
	                               {"pages", Json::integer(2)}}));
	CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "open /Users/me/tmp/a.pdf",
	                           "page S-3", "page S-1", "close pdf", "list", "switch 11"}));
	CHECK(docs.open[0].active);
	std::vector<std::string> asked = {"to /Users/me/tmp/a.pdf", "parent /Users/me/tmp",
	                                  "to /Users/me/tmp/a.pdf"};
	CHECK(fs.asked == asked);
}

// 前面の図面なら切り替えない。同じシートを 2 度渡せば 2 頁になる（重複を断らない）。
TEST(ExportPdfExportsTheFrontDocumentWithoutSwitching) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	Json result = VwCli::core::exportPdf(
	    exportArgs("/Users/me/plan.vwx", {"S-2", "S-2"}, "/Users/me/tmp/a.PDF"), docs, pdf,
	    fs.environment(), status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("pages"), Json::integer(2));
	CHECK(docs.calls == calls({"list", "sheets", "open /Users/me/tmp/a.PDF", "page S-2", "page S-2",
	                           "close pdf", "list"}));
}

TEST(ExportPdfRejectsBadArguments) {
	for (
	    const char *args : {
	        R"({})",
	        R"({"document":"/Users/me/plan.vwx","to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1"]})",
	        R"({"sheets":["S-1"],"to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":[],"to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":"S-1","to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":[1],"to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1",null],"to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":null,"to":"/Users/me/tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1"],"to":"/Users/me/tmp/a.vwx"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1"],"to":"tmp/a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1"],"to":"/Users/me/../a.pdf"})",
	        R"({"document":"/Users/me/plan.vwx","sheets":["S-1"],"to":"/Users/me/tmp/a.pdf","x":1})",
	    }) {
		FakeDocuments docs = sample();
		Fs fs;
		FakePdf pdf(docs.calls, fs);
		ToolStatus status;
		Json result = VwCli::core::exportPdf(must(args), docs, pdf, fs.environment(), status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(result.isNull());
		CHECK(docs.calls.empty());
		CHECK(fs.asked.empty());
	}
	// Windows の規則を渡せば、/ を含む to を拒否する。
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, "C:/x/a.pdf"), docs, pdf,
	                       fs.environment(&save_target::checkWindowsTo), status);
	CHECK(status.code == ErrorCode::InvalidArgs);
	CHECK(docs.calls.empty());
}

TEST(ExportPdfFailsWithoutDocuments) {
	FakeDocuments docs;
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs("/Users/me/plan.vwx", {"S-1"}, "/Users/me/tmp/a.pdf"), docs,
	                       pdf, fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"no_document"})"));
	CHECK(docs.calls == calls({"list"}));
	CHECK(fs.asked.empty());
}

TEST(ExportPdfRejectsADocumentThatIsNotOpenOrAmbiguous) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs("/Users/me/Plan.vwx", {"S-1"}, "/Users/me/tmp/a.pdf"), docs,
	                       pdf, fs.environment(), status);
	CHECK(status.code == ErrorCode::InvalidArgs);
	CHECK(docs.calls == calls({"list"}));

	FakeDocuments twins;
	twins.open = {{"/Applications/Vectorworks 2026/plan v2026.vwx", true, false, 1},
	              {"/Applications/Vectorworks 2026/plan v2026.vwx", false, true, 2}};
	FakePdf twinPdf(twins.calls, fs);
	ToolStatus ambiguous;
	VwCli::core::exportPdf(
	    exportArgs("/Applications/Vectorworks 2026/plan v2026.vwx", {"S-1"}, "/Users/me/tmp/a.pdf"),
	    twins, twinPdf, fs.environment(), ambiguous);
	CHECK_EQ(ambiguous.detail, must(R"({"reason":"document_ambiguous"})"));
	CHECK(ambiguous.message.find("どれも書き出しませんでした") != std::string::npos);
	CHECK(twins.calls == calls({"list"}));
	CHECK(fs.asked.empty());
}

// 無い名前（デザインレイヤの名前・空の名前・大文字と小文字だけが違う名前を含む）は、書き出す
// 先を確かめる前・PDF を開く前に invalid_args で断り、前面を戻す。
TEST(ExportPdfRejectsNamesThatAreNotSheets) {
	for (const char *sheet : {"S-4", "1F", "", "s-1"}) {
		FakeDocuments docs = sample();
		Fs fs;
		FakePdf pdf(docs.calls, fs);
		ToolStatus status;
		VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1", sheet}, "/Users/me/tmp/a.pdf"), docs,
		                       pdf, fs.environment(), status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(status.message.find("シートレイヤ " + std::string(sheet) + " がありません") !=
		      std::string::npos);
		CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "list", "switch 11"}));
		CHECK(fs.asked.empty());
	}
}

// 書き出す先の状態は save と同じ順で確かめ、PDF を開く前に断る（to_is_open は返さない）。
TEST(ExportPdfRefusesTargetsBeforeOpening) {
	struct Case {
		std::vector<std::string> files;
		std::vector<std::string> folders;
		const char *to;
		const char *reason;
	};
	for (const Case &c : {
	         Case{{"/Users/me/tmp/a.pdf"}, {"/Users/me/tmp"}, "/Users/me/tmp/a.pdf", "file_exists"},
	         Case{{},
			      {"/Users/me/tmp/a.pdf", "/Users/me/tmp"},
			      "/Users/me/tmp/a.pdf",
			      "file_exists"},
	         Case{{}, {}, "/Users/me/none/a.pdf", "parent_missing"},
	         Case{{"/Users/me/file"}, {}, "/Users/me/file/a.pdf", "parent_missing"},
	     }) {
		FakeDocuments docs = sample();
		Fs fs;
		fs.files = c.files;
		fs.folders = c.folders;
		FakePdf pdf(docs.calls, fs);
		ToolStatus status;
		VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, c.to), docs, pdf, fs.environment(),
		                       status);
		CHECK(status.code == ErrorCode::Internal);
		CHECK_EQ(reasonOf(status), std::string(c.reason));
		CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "list", "switch 11"}));
	}
	FakeDocuments docs = sample();
	Fs fs;
	fs.toFails = Probe{Entry::Failed, 13};
	FakePdf pdf(docs.calls, fs);
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, "/Users/me/tmp/a.pdf"), docs, pdf,
	                       fs.environment(), status);
	CHECK_EQ(status.detail, must(R"({"reason":"to_check_failed","check":"to","os_error":13})"));
}

TEST(ExportPdfReportsAnOpenFailure) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	pdf.openError = 1;
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, "/Users/me/tmp/a.pdf"), docs, pdf,
	                       fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"export_failed","step":"open","vcom_error":1})"));
	// 開けなければ閉じない。
	CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "open /Users/me/tmp/a.pdf",
	                           "list", "switch 11"}));
}

// 頁の失敗では残りを書かずに閉じ、書きかけの PDF を消さない。
TEST(ExportPdfStopsAtAFailedPageAndCloses) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	pdf.failsAt = "S-2";
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1", "S-2", "S-3"}, "/Users/me/tmp/a.pdf"),
	                       docs, pdf, fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail,
	         must(R"({"reason":"export_failed","step":"page","sheet":"S-2","vcom_error":1})"));
	CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "open /Users/me/tmp/a.pdf",
	                           "page S-1", "page S-2", "close pdf", "list", "switch 11"}));
	CHECK(fs.files == std::vector<std::string>{"/Users/me/tmp/a.pdf"});
}

TEST(ExportPdfReportsACloseFailure) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	pdf.closeError = 1;
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs("/Users/me/plan.vwx", {"S-1"}, "/Users/me/tmp/a.pdf"), docs,
	                       pdf, fs.environment(), status);
	CHECK_EQ(status.detail, must(R"({"reason":"export_failed","step":"close","vcom_error":1})"));
}

// 全てが成功を返してもファイルが無ければ失敗にする（出力先のフォルダが無い場合の IExportPDF）。
TEST(ExportPdfChecksTheFileAfterWriting) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	pdf.writes = false;
	ToolStatus status;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, "/Users/me/tmp/a.pdf"), docs, pdf,
	                       fs.environment(), status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"export_failed","step":"file"})"));
	CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "open /Users/me/tmp/a.pdf",
	                           "page S-1", "close pdf", "list", "switch 11"}));

	// 書き終えた後の問い合わせが「無い」以外で失敗したら、os_error を付ける。
	FakeDocuments failing = sample();
	Fs probe;
	FakePdf failingPdf(failing.calls, probe);
	failingPdf.probeAfterClose = Probe{Entry::Failed, 13};
	ToolStatus failed;
	VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1"}, "/Users/me/tmp/a.pdf"), failing,
	                       failingPdf, probe.environment(), failed);
	CHECK_EQ(failed.detail, must(R"({"reason":"export_failed","step":"file","os_error":13})"));
}

// 頁の途中で投げた場合も、PDF を閉じ、前面を戻してから投げる（Server が internal で応答する）。
TEST(ExportPdfClosesAndRestoresTheFrontWhenAPageThrows) {
	FakeDocuments docs = sample();
	Fs fs;
	FakePdf pdf(docs.calls, fs);
	pdf.throwsAt = "S-2";
	ToolStatus status;
	bool threw = false;
	try {
		VwCli::core::exportPdf(exportArgs(kUntitled, {"S-1", "S-2"}, "/Users/me/tmp/a.pdf"), docs,
		                       pdf, fs.environment(), status);
	} catch (const std::runtime_error &) {
		threw = true;
	}
	CHECK(threw);
	CHECK(docs.calls == calls({"list", "switch 22", "list", "sheets", "open /Users/me/tmp/a.pdf",
	                           "page S-1", "page S-2", "close pdf", "list", "switch 11"}));
}
