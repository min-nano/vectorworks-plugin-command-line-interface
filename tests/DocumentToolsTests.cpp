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

// 図面を扱う道具（documents・save・close）の内 SDK に依らない部分（docs/plugin/tools.md「図面を
// 扱う道具」）。SDK の代わりに偽の Documents を渡し、呼んだ順を確かめる。

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

// Fs は差し替えのファイルシステム（無い・フォルダがある、を名前で決める）と比べ方。
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
			     // 差し替えの比べ方: ASCII の大文字と小文字だけを畳む。
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

// save・close は占有の中でだけ呼べる App（Read で登録すると、占有なしで保存・閉じられる）。
// 種類は定数で決まる（道具の表は定数で初期化される）。
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

// 図面が無くても失敗にせず、空の一覧を返す。
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

// 前面でない図面は前面にしてから保存し、保存の直後の一覧から path を読んでから前面を戻す。
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

// 前面の図面を保存するときは切り替えない。保存済みの図面も、同じ図面の 2 回目も新しい to
// へ保存できる。
TEST(SaveSavesTheFrontDocumentWithoutSwitching) {
	FakeDocuments docs = sample();
	Fs fs;
	ToolStatus status;
	Json result = VwCli::core::save(saveArgs("/Users/me/plan.vwx", "/Users/me/tmp/a.vwx"), docs,
	                                fs.environment(), status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("path"), Json::string("/Users/me/tmp/a.vwx"));
	CHECK(docs.calls == calls({"list", "save /Users/me/tmp/a.vwx", "list"}));

	// 2 回目: 新しい to なら通り、直前の保存先は開いている図面のパスなので to_is_open。
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

// 結果の path は to をそのまま返さず、保存の直後の一覧から読む。
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
		// 図面が無くても、引数の形を先に返す。
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(result.isNull());
		CHECK(docs.calls.empty());
		CHECK(fs.asked.empty());
	}
	// Windows の規則を渡せば、/ を含む to を断る。
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

// 名指す図面が無ければ invalid_args（完全に一致するものだけ）。
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

// 2 枚が一致すれば、untitled が食い違っても、状態の確かめより先に、保存・閉じるを呼ぶ前に断る。
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
			// ルートの直下の親はルートとして確かめる。
			CHECK(status.code == ErrorCode::None);
			CHECK_EQ(fs.asked.back(), std::string("parent /"));
		}
	}
	// 開いている図面と一致する to は、親のフォルダが消されていても to_is_open。
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

// SwitchToOpenFile が真を返しても前面が名指した図面にならなければ、保存も閉じるも呼ばずに断る
// （利用者が前面に開いている別の図面を保存・閉じない）。
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

// パスを読めなかった図面（path が空）は名指せない。空の document は、そうした図面が 1 枚でも
// 2 枚でも invalid_args で断る（document_ambiguous にも、その図面を閉じることにもならない）。
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

// 前面を閉じたら戻せない（Vectorworks が選んだ前面のまま）。
TEST(CloseClosesTheFrontDocument) {
	FakeDocuments docs = sample();
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(docs.calls == calls({"list", "close", "list"}));
	CHECK_EQ(docs.open.size(), std::size_t{2});
}

// 前の前面がもう開いていなければ、戻さない。
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

// 最後の図面を閉じたら、閉じた順のために中に残った図面を、一覧が空であることを確かめながら 1 枚
// ずつ消す（#321）。
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

// 図面が開いている間は、残った図面を片付けない（CloseDocument が開いている図面を閉じてしまう）。
TEST(CloseDoesNotClearWhileDocumentsAreOpen) {
	FakeDocuments docs = sample();
	docs.remnants = 1;
	ToolStatus status;
	VwCli::core::close(must(R"({"document":"/Users/me/plan.vwx"})"), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(docs.calls == calls({"list", "close", "list"}));
	CHECK_EQ(docs.remnants, 1);
}

// 片付かなくても、名指した図面は閉じたので失敗にせず、結果に closed_documents_remain を載せる。
// CloseDocument を呼ぶ回数には上限がある。
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

// serveOnce は要求 1 つを置いて受け付けを 1 回走らせ、応答を返す。
Json serveOnce(Server &server, Caller &caller, std::span<const Tool> tools, const std::string &id,
               const std::string &request) {
	caller.put(id, request);
	server.serve(tools, kNow);
	Json response = caller.response(id);
	CHECK(!response.isNull());
	return response;
}

}

// 道具の表と同じ種類で載せた save・close は、印なしなら no_session、他の印なら occupied で、
// 実行されずに断られる。
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
	Json started = serveOnce(server, caller, tools, "b", R"({"tool":"session_start"})");
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

// 保存を呼べずに投げたときも、前面を戻してから投げる（Server が internal で応える）。
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
