#include "FakeDocuments.h"
#include "TestSupport.h"

#include "core/DocumentTools.h"
#include "core/Files.h"
#include "core/Lifecycle.h"
#include "core/Protocol.h"
#include "core/Quit.h"
#include "core/Serve.h"

#include <cstddef>
#include <cstdint>
#include <memory>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

// ツール quit と、起動を終了したフラグ・終了の依頼の最中のフラグ（docs/plugin/tools.md「quit」・
// docs/plugin/architecture.md「エントリポイント」「Vectorworks が処理中」）。SDK の代わりに
// 擬似 Documents と擬似的な終了の呼び出しを渡す。保存の確認が開いて呼び出しが戻らない間に周期処理が
// 再入すること（Windows）は、擬似的な終了の呼び出しの中から周期処理を呼び出して模倣する。

using VwCli::core::Activity;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::Lifecycle;
using VwCli::core::OpenDocument;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::core::ToolKind;
using VwCli::core::ToolStatus;
using VwCli::test::FakeDocuments;
using VwCli::test::kNow;
using VwCli::test::must;
using VwCli::test::TempDir;
namespace files = VwCli::core::files;

namespace {

FakeDocuments twoDocuments() {
	FakeDocuments docs;
	docs.open = {{"/Users/me/plan.vwx", false, true, 11},
	             {"/Applications/Vectorworks 2026/名称未設定 1", true, false, 22}};
	return docs;
}

ToolContext started() {
	ToolContext context;
	context.startup = {true, true};
	return context;
}

}

// quit は占有の中でのみ呼び出せる App（Read で登録すると、占有なしで終了を依頼できる）。種類は
// 定数で決定される（ツールの一覧は定数で初期化される）。
static_assert(VwCli::core::kQuitSpec.kind == ToolKind::App);

TEST(QuitSpec) {
	CHECK_EQ(std::string(VwCli::core::kQuitSpec.name), std::string("quit"));
	CHECK(VwCli::core::kQuitSpec.kind == ToolKind::App);
	CHECK(must(VwCli::core::kQuitSpec.inputSchema).isObject());
}

TEST(QuitQuitsWithoutDocuments) {
	FakeDocuments docs;
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(status.quit);
	CHECK_EQ(result, must(R"({"quitting":true})"));
	// 開いている図面を確認する 1 回・残存した図面を後処理する前に確認する 1 回・後処理の後の 1 回。
	CHECK_EQ(docs.count("list"), 3);
	CHECK(docs.actions().empty());
}

// stillStarting は起動の途中（通知は登録できた）の状態。
ToolContext stillStarting() {
	ToolContext context;
	context.startup.notificationRegistered = true;
	return context;
}

// 起動を終了するまでは、図面の有無によらず、図面の一覧を参照せずに starting で拒否する（detail は
// documents を持たない）。
TEST(QuitRefusesBeforeStarting) {
	for (bool withDocuments : {false, true}) {
		FakeDocuments docs = withDocuments ? twoDocuments() : FakeDocuments();
		ToolStatus status;
		Json result = VwCli::core::quit(Json::object(), stillStarting(), docs, status);
		CHECK(status.code == ErrorCode::Internal);
		CHECK(!status.quit);
		CHECK(result.isNull());
		CHECK_EQ(status.detail, must(R"({"reason":"starting","notification_registered":true})"));
		CHECK_EQ(status.message,
		         std::string("Vectorworks が起動を終えていないので終了しませんでした"));
		CHECK_EQ(docs.count("list"), 0);
		CHECK(docs.actions().empty());
	}
}

// 起動の終了を示す通知を登録できていなければ、reason は starting のまま、detail と文言で区別して
// 示す（フラグは永久に立たないため、呼び出し元が起動を待機して呼び出しを繰り返し続けないように）。
TEST(QuitTellsThatTheNotificationIsNotRegistered) {
	FakeDocuments docs;
	ToolStatus status;
	VwCli::core::quit(Json::object(), ToolContext{}, docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(!status.quit);
	CHECK_EQ(status.detail, must(R"({"reason":"starting","notification_registered":false})"));
	CHECK(status.message.find("Vectorworks が起動を終えていないので終了しませんでした") == 0);
	CHECK(status.message.find("通知を登録できていません") != std::string::npos);
	CHECK_EQ(docs.count("list"), 0);
}

// 図面が開いていれば、終了を依頼せずに documents_open で拒否し、一覧をその順のまま detail に
// 含める。
TEST(QuitRefusesWithDocumentsOpen) {
	FakeDocuments docs = twoDocuments();
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(!status.quit);
	CHECK(result.isNull());
	CHECK(status.message.find("図面が開いているので終了しませんでした。先に図面を閉じてください") !=
	      std::string::npos);
	CHECK_EQ(status.detail,
	         must(R"({"reason":"documents_open","documents":[)"
	              R"({"name":"plan.vwx","path":"/Users/me/plan.vwx","untitled":false},)"
	              R"({"name":"名称未設定 1","path":"/Applications/Vectorworks 2026/名称未設定 1",)"
	              R"("untitled":true}]})"));
	CHECK(docs.actions().empty());
}

// 一覧が空でも閉じた図面が内部に残存していれば、後処理してから終了を依頼する（残すと終了で保存の
// 確認が表示される。#321）。
TEST(QuitClearsClosedDocumentsBeforeQuitting) {
	FakeDocuments docs;
	docs.remnants = 2;
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(status.quit);
	CHECK_EQ(result, must(R"({"quitting":true})"));
	CHECK_EQ(docs.remnants, 0);
	CHECK_EQ(docs.actions(), (std::vector<std::string>{"close", "close"}));
}

// 後処理できなければ、終了を依頼せずに closed_documents_remain で拒否する。
TEST(QuitRefusesWhenClosedDocumentsRemain) {
	FakeDocuments docs;
	docs.remnants = 1;
	docs.remnantsStick = true;
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(!status.quit);
	CHECK(result.isNull());
	CHECK_EQ(status.detail, must(R"({"reason":"closed_documents_remain"})"));
	CHECK_EQ(docs.actions().size(), std::size_t(VwCli::core::kMaxClosedRemnants));
}

// 後処理の間に図面が開けば、終了を依頼せずに documents_open で拒否する（後処理は一覧が空で
// なくなると停止して「後処理が完了した」と返すため、その後に再度一覧を参照する）。
TEST(QuitRefusesADocumentOpenedWhileClearing) {
	FakeDocuments docs;
	docs.remnants = 2;
	docs.opensWhileClearing = OpenDocument{"/Users/me/new.vwx", false, true, 44};
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(!status.quit);
	CHECK(result.isNull());
	CHECK_EQ(status.detail,
	         must(R"({"reason":"documents_open","documents":[)"
	              R"({"name":"new.vwx","path":"/Users/me/new.vwx","untitled":false}]})"));
	CHECK_EQ(docs.actions(), (std::vector<std::string>{"close"}));
}

// 起動を終了する前と、図面が開いている間は、残存した図面に触れない。
TEST(QuitDoesNotClearBeforeStartingOrWithDocumentsOpen) {
	FakeDocuments before;
	before.remnants = 1;
	ToolStatus starting;
	VwCli::core::quit(Json::object(), ToolContext{}, before, starting);
	CHECK(before.actions().empty());
	FakeDocuments open = twoDocuments();
	open.remnants = 1;
	ToolStatus documentsOpen;
	VwCli::core::quit(Json::object(), started(), open, documentsOpen);
	CHECK(open.actions().empty());
	CHECK_EQ(open.remnants, 1);
}

// 引数の形式は、起動を終了したかより先に検証する（docs/plugin/tools.md「共通の規則」の
// 検証の順の 1）。
TEST(QuitRejectsArguments) {
	for (bool hasStarted : {false, true}) {
		FakeDocuments docs;
		ToolStatus status;
		Json result = VwCli::core::quit(must(R"({"force":true})"),
		                                hasStarted ? started() : ToolContext{}, docs, status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(!status.quit);
		CHECK(result.isNull());
		CHECK(status.detail.isNull());
		CHECK(status.message.find("force") != std::string::npos);
		CHECK_EQ(docs.count("list"), 0);
	}
}

TEST(LifecycleStartsWhenMarked) {
	Lifecycle lifecycle;
	CHECK(!lifecycle.startup().started);
	CHECK(!lifecycle.startup().notificationRegistered);
	CHECK(!lifecycle.notificationRegistered());
	CHECK(lifecycle.activity() == Activity::Idle);
	lifecycle.markNotificationRegistered();
	CHECK(lifecycle.notificationRegistered());
	CHECK(lifecycle.startup().notificationRegistered);
	CHECK(!lifecycle.startup().started);
	lifecycle.markStarted();
	CHECK(lifecycle.startup().started);
	// 通知は以後も届く。立ったまま。
	lifecycle.markStarted();
	CHECK(lifecycle.startup().started);
	CHECK(lifecycle.activity() == Activity::Idle);
}

// 終了を依頼している間は Quitting で、2 度目を依頼しない。戻ったら下ろし、次は再度依頼できる。
TEST(LifecycleRaisesTheMarkWhileQuitting) {
	Lifecycle lifecycle;
	int requests = 0;
	bool nested = true;
	CHECK(lifecycle.quit([&] {
		++requests;
		CHECK(lifecycle.activity() == Activity::Quitting);
		nested = lifecycle.quit([&] { ++requests; });
	}));
	CHECK(!nested);
	CHECK_EQ(requests, 1);
	CHECK(lifecycle.activity() == Activity::Idle);
	CHECK(lifecycle.quit([&] { ++requests; }));
	CHECK_EQ(requests, 2);
	CHECK(lifecycle.activity() == Activity::Idle);
}

// 終了の呼び出しが投げても、フラグを下ろす（下ろさないと以後のツールを拒否し続ける）。
TEST(LifecycleLowersTheMarkWhenTheRequestThrows) {
	Lifecycle lifecycle;
	bool thrown = false;
	try {
		lifecycle.quit([] { throw std::runtime_error("終了を頼めなかった"); });
	} catch (const std::runtime_error &) {
		thrown = true;
	}
	CHECK(thrown);
	CHECK(lifecycle.activity() == Activity::Idle);
}

namespace {

using VwCli::test::Caller;

FakeDocuments *gDocs = nullptr;

Json quitTool(const Json &args, const ToolContext &context, ToolStatus &status) {
	return VwCli::core::quit(args, context, *gDocs, status);
}

Json documentsTool(const Json &args, const ToolContext & /*context*/, ToolStatus &status) {
	return VwCli::core::documents(args, *gDocs, status);
}

// ツールの一覧と同じ行（種類）で登録する。
constexpr Tool kTools[] = {VwCli::core::toolOf(VwCli::core::kDocumentsSpec, &documentsTool),
                           VwCli::core::toolOf(VwCli::core::kQuitSpec, &quitTool)};

}

// 周期処理（core::tick）を経由した quit: 占有の外では拒否され、起動を終了するまでと図面が
// 開いている間は終了を依頼せず、図面が 0 枚なら応答を書き込んでから終了を依頼する。依頼している
// 間に再入した周期処理（Windows の保存の確認の間）では、一覧のツール（quit と読み取りツールも）を
// busy で拒否して 2 度目を依頼せず、@tools・@session_start・@session_end は応答し、一覧に無い
// ツールは unknown_tool。戻ったらフラグを下ろす。
TEST(TickQuitsOnlyAfterStartingWithoutDocuments) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	Lifecycle lifecycle;
	FakeDocuments docs = twoDocuments();
	gDocs = &docs;
	int requests = 0;
	auto noRequest = [&] { ++requests; };

	// 占有の外。
	caller.put("a1", R"({"tool":"quit"})");
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	CHECK_EQ(caller.code("a1"), std::string("no_session"));

	caller.put("a2", R"({"tool":"@session_start"})");
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	std::string session = caller.session("a2");
	REQUIRE(!session.empty());
	std::string quit = R"({"tool":"quit","session":")" + session + R"("})";

	// 起動を終了する前は、図面が開いていても starting。通知を登録できたかも渡される。
	caller.put("b1", quit);
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	CHECK_EQ(caller.code("b1"), std::string("internal"));
	CHECK_EQ(caller.member("b1", "detail"),
	         must(R"({"reason":"starting","notification_registered":false})"));
	lifecycle.markNotificationRegistered();
	docs.open.clear();
	caller.put("b2", quit);
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	CHECK_EQ(caller.member("b2", "detail"),
	         must(R"({"reason":"starting","notification_registered":true})"));
	CHECK_EQ(requests, 0);

	// 起動を終了した後、図面が開いていれば documents_open で、占有は残る。
	lifecycle.markStarted();
	docs = twoDocuments();
	caller.put("c1", quit);
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	CHECK_EQ(caller.code("c1"), std::string("internal"));
	CHECK_EQ(VwCli::test::reasonOf(caller.member("c1", "detail")), std::string("documents_open"));
	CHECK_EQ(requests, 0);
	CHECK(server.inSession());

	// 図面が 0 枚なら終了を依頼する。依頼している間に周期処理が再入する。
	docs.open.clear();
	caller.put("d1", quit);
	bool answeredFirst = false;
	VwCli::core::ServeResult outer = VwCli::core::tick(server, kTools, kNow, lifecycle, [&] {
		++requests;
		// 終了を依頼する前に応答を書き込んである。
		answeredFirst = caller.result("d1") == must(R"({"quitting":true})");
		CHECK(lifecycle.activity() == Activity::Quitting);
		caller.put("e1", quit);
		caller.put("e2", R"({"tool":"documents"})");
		caller.put("e3", R"({"tool":"@tools"})");
		caller.put("e4", R"({"tool":"no_such_tool"})");
		caller.put("e5", R"({"tool":"@session_end","session":")" + session + R"("})");
		VwCli::core::ServeResult inner =
		    VwCli::core::tick(server, kTools, kNow, lifecycle, [&] { ++requests; });
		CHECK(!inner.quit);
	});
	CHECK(outer.quit);
	CHECK(answeredFirst);
	CHECK_EQ(requests, 1);
	CHECK_EQ(caller.code("e1"), std::string("busy"));
	CHECK_EQ(caller.code("e2"), std::string("busy"));
	CHECK(caller.result("e3").isObject());
	CHECK_EQ(caller.code("e4"), std::string("unknown_tool"));
	CHECK(caller.result("e5").isObject());
	CHECK(!server.inSession());
	// 戻ったら下ろす（取り消されて Vectorworks が動作し続けても、再度依頼できる）。
	CHECK(lifecycle.activity() == Activity::Idle);
	caller.put("f1", R"({"tool":"@session_start"})");
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	session = caller.session("f1");
	REQUIRE(!session.empty());
	caller.put("f2", R"({"tool":"quit","session":")" + session + R"("})");
	CHECK(VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest).quit);
	CHECK_EQ(requests, 2);
	CHECK(lifecycle.activity() == Activity::Idle);
	CHECK(docs.actions().empty());
	gDocs = nullptr;
}
