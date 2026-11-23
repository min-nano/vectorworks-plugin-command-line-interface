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
#include <optional>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

// 道具 quit と、起動を終えた印・終了の依頼の最中の印（docs/plugin/tools.md「quit」・
// docs/plugin/architecture.md「入口」「Vectorworks が処理中」）。SDK の代わりに偽の Documents と
// 偽の終了の呼び出しを渡す。保存の確認が開いて呼び出しが戻らない間に刻みが入り直すこと（Windows）は、
// 偽の終了の呼び出しの中から刻みを呼んで真似る。

using VwCli::core::Activity;
using VwCli::core::Documents;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::Lifecycle;
using VwCli::core::OpenDocument;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::core::ToolKind;
using VwCli::core::ToolStatus;
using VwCli::test::kNow;
using VwCli::test::must;
using VwCli::test::TempDir;
namespace files = VwCli::core::files;

namespace {

// FakeDocuments は開いている図面の一覧だけを持つ。quit は一覧を読むだけなので、ほかの呼び出しは
// 覚えておき、呼ばれないことを確かめる。
class FakeDocuments : public Documents {
public:
	std::vector<OpenDocument> open;
	int lists = 0;
	std::vector<std::string> others;
	// 閉じた図面が中に残った枚数（一覧には出ず、GetActiveDocument だけが真を返す）と、一覧が空の
	// ときの CloseDocument でそれが消えないか。
	int remnants = 0;
	bool remnantsStick = false;

	std::vector<OpenDocument> list() override {
		++lists;
		return open;
	}
	bool switchTo(std::int32_t ref) override {
		others.push_back("switch " + std::to_string(ref));
		return true;
	}
	std::uint32_t saveActive(const std::string &path) override {
		others.push_back("save " + path);
		return 0;
	}
	void closeActive() override {
		others.emplace_back("close");
		if (open.empty() && remnants > 0 && !remnantsStick) {
			--remnants;
		}
	}
	bool hasActive() override { return remnants > 0 || !open.empty(); }
};

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

// quit は占有の中でだけ呼べる App（Read で登録すると、占有なしで終了を頼める）。種類は定数で
// 決まる（道具の表は定数で初期化される）。
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
	// 開いている図面を確かめる 1 回と、残った図面を片付ける前に確かめる 1 回。
	CHECK_EQ(docs.lists, 2);
	CHECK(docs.others.empty());
}

// stillStarting は起動の途中（通知は登録できた）の状態。
ToolContext stillStarting() {
	ToolContext context;
	context.startup.notificationRegistered = true;
	return context;
}

// 起動を終えるまでは、図面の有無によらず、図面の一覧を見ずに starting で断る（detail は
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
		CHECK_EQ(docs.lists, 0);
		CHECK(docs.others.empty());
	}
}

// 起動を終えた目印の通知を登録できていなければ、reason は starting のまま、detail と文言で分けて
// 示す（印はいつまでも立たないので、呼ぶ側が起動を待って呼び直し続けないように）。
TEST(QuitTellsThatTheNotificationIsNotRegistered) {
	FakeDocuments docs;
	ToolStatus status;
	VwCli::core::quit(Json::object(), ToolContext{}, docs, status);
	CHECK(status.code == ErrorCode::Internal);
	CHECK(!status.quit);
	CHECK_EQ(status.detail, must(R"({"reason":"starting","notification_registered":false})"));
	CHECK(status.message.find("Vectorworks が起動を終えていないので終了しませんでした") == 0);
	CHECK(status.message.find("通知を登録できていません") != std::string::npos);
	CHECK_EQ(docs.lists, 0);
}

// 図面が開いていれば、終了を頼まずに documents_open で断り、一覧をその順のまま detail に載せる。
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
	CHECK(docs.others.empty());
}

// 一覧が空でも閉じた図面が中に残っていれば、片付けてから終了を頼む（残すと終了で保存の確認が
// 出る。#321）。
TEST(QuitClearsClosedDocumentsBeforeQuitting) {
	FakeDocuments docs;
	docs.remnants = 2;
	ToolStatus status;
	Json result = VwCli::core::quit(Json::object(), started(), docs, status);
	CHECK(status.code == ErrorCode::None);
	CHECK(status.quit);
	CHECK_EQ(result, must(R"({"quitting":true})"));
	CHECK_EQ(docs.remnants, 0);
	CHECK_EQ(docs.others, (std::vector<std::string>{"close", "close"}));
}

// 片付けられなければ、終了を頼まずに closed_documents_remain で断る。
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
	CHECK_EQ(docs.others.size(), std::size_t(VwCli::core::kMaxClosedRemnants));
}

// 起動を終える前と、図面が開いている間は、残った図面に触れない。
TEST(QuitDoesNotClearBeforeStartingOrWithDocumentsOpen) {
	FakeDocuments before;
	before.remnants = 1;
	ToolStatus starting;
	VwCli::core::quit(Json::object(), ToolContext{}, before, starting);
	CHECK(before.others.empty());
	FakeDocuments open = twoDocuments();
	open.remnants = 1;
	ToolStatus documentsOpen;
	VwCli::core::quit(Json::object(), started(), open, documentsOpen);
	CHECK(open.others.empty());
	CHECK_EQ(open.remnants, 1);
}

// 引数の形は、起動を終えたかより先に確かめる（docs/plugin/tools.md「最初に提供する道具」の
// 確かめの順の 1）。
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
		CHECK_EQ(docs.lists, 0);
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

// 終了を頼んでいる間は Quitting で、2 度目を頼まない。戻ったら下ろし、次はまた頼める。
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

// 終了の呼び出しが投げても、印を下ろす（下ろさないと以後の道具を断り続ける）。
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

// 応答のファイルを読む上限（ServeTests と同じ）。
constexpr std::size_t kMaxResponseBytes = VwCli::core::protocol::kMaxRequestBytes * 2;

// Caller は呼ぶ側を真似る。待つ印を掴んでから要求を置き、壊すと放す。
class Caller {
public:
	explicit Caller(const TempDir &dir) : dir_(dir) {}

	void put(const std::string &id, const std::string &request) {
		std::string error;
		locks_.emplace_back(std::make_unique<files::Lock>());
		CHECK(locks_.back()->take(dir_.sub(id + ".wait"), error) == files::Lock::Result::Taken);
		CHECK(files::write(dir_.sub(id + ".req.json"), request));
	}

	// response は応答を読む。無ければ null。
	Json response(const std::string &id) const {
		std::optional<std::string> text =
		    files::read(dir_.sub(id + ".res.json"), kMaxResponseBytes);
		return text ? must(*text) : Json();
	}

	// member は応答のフィールドの値。無ければ null。
	Json member(const std::string &id, std::string_view key) const {
		Json value = response(id);
		const Json *found = value.find(key);
		return found ? *found : Json();
	}

	Json result(const std::string &id) const { return member(id, "result"); }

	std::string code(const std::string &id) const {
		Json code = member(id, "code");
		return code.isString() ? code.asString() : "";
	}

	// session は session_start の応答の占有の印。無ければ空。
	std::string session(const std::string &id) const {
		Json value = result(id);
		const Json *session = value.find("session");
		return session && session->isString() ? session->asString() : "";
	}

private:
	const TempDir &dir_;
	std::vector<std::unique_ptr<files::Lock>> locks_;
};

FakeDocuments *gDocs = nullptr;

Json quitTool(const Json &args, const ToolContext &context, ToolStatus &status) {
	return VwCli::core::quit(args, context, *gDocs, status);
}

Json documentsTool(const Json &args, const ToolContext & /*context*/, ToolStatus &status) {
	return VwCli::core::documents(args, *gDocs, status);
}

// 道具の表と同じ行（種類）で載せる。
constexpr Tool kTools[] = {VwCli::core::toolOf(VwCli::core::kDocumentsSpec, &documentsTool),
                           VwCli::core::toolOf(VwCli::core::kQuitSpec, &quitTool)};

}

// 刻み（core::tick）を通した quit: 占有の外では断られ、起動を終えるまでと図面が開いている間は
// 終了を頼まず、図面が 0 枚なら応答を書いてから終了を頼む。頼んでいる間に入り直した刻み（Windows の
// 保存の確認の間）では、表の道具（quit と読む道具も）を busy で断って 2 度目を頼まず、tools・
// session_start・session_end は応え、表に無い道具は unknown_tool。戻ったら印を下ろす。
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

	caller.put("a2", R"({"tool":"session_start"})");
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	std::string session = caller.session("a2");
	REQUIRE(!session.empty());
	std::string quit = R"({"tool":"quit","session":")" + session + R"("})";

	// 起動を終える前は、図面が開いていても starting。通知を登録できたかも渡る。
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

	// 起動を終えたあと、図面が開いていれば documents_open で、占有は残る。
	lifecycle.markStarted();
	docs = twoDocuments();
	caller.put("c1", quit);
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	CHECK_EQ(caller.code("c1"), std::string("internal"));
	Json detail = caller.member("c1", "detail");
	const Json *reason = detail.find("reason");
	CHECK(reason && *reason == Json::string("documents_open"));
	CHECK_EQ(requests, 0);
	CHECK(server.inSession());

	// 図面が 0 枚なら終了を頼む。頼んでいる間に刻みが入り直す。
	docs.open.clear();
	caller.put("d1", quit);
	bool answeredFirst = false;
	VwCli::core::ServeResult outer = VwCli::core::tick(server, kTools, kNow, lifecycle, [&] {
		++requests;
		// 終了を頼む前に応答を書いてある。
		answeredFirst = caller.result("d1") == must(R"({"quitting":true})");
		CHECK(lifecycle.activity() == Activity::Quitting);
		caller.put("e1", quit);
		caller.put("e2", R"({"tool":"documents"})");
		caller.put("e3", R"({"tool":"tools"})");
		caller.put("e4", R"({"tool":"no_such_tool"})");
		caller.put("e5", R"({"tool":"session_end","session":")" + session + R"("})");
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
	// 戻ったら下ろす（取り消されて Vectorworks が動き続けても、また頼める）。
	CHECK(lifecycle.activity() == Activity::Idle);
	caller.put("f1", R"({"tool":"session_start"})");
	VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest);
	session = caller.session("f1");
	REQUIRE(!session.empty());
	caller.put("f2", R"({"tool":"quit","session":")" + session + R"("})");
	CHECK(VwCli::core::tick(server, kTools, kNow, lifecycle, noRequest).quit);
	CHECK_EQ(requests, 2);
	CHECK(lifecycle.activity() == Activity::Idle);
	CHECK(docs.others.empty());
	gDocs = nullptr;
}
