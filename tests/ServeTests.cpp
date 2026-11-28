#include "TestSupport.h"

#include "core/Files.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <cstddef>
#include <filesystem>
#include <memory>
#include <span>
#include <string>
#include <vector>

// 1 回の受付処理の内、通信規則のフィクスチャ（FixtureTests.cpp）で表現しにくいもの。

using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::Phase;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::core::ToolKind;
using VwCli::core::ToolStatus;
namespace files = VwCli::core::files;
using VwCli::test::TempDir;

namespace {

using VwCli::test::kNow;

using VwCli::test::Caller;

std::vector<std::string> gCalls;
Caller *gCaller = nullptr;
Server *gServer = nullptr;
// nests が入れ子に渡すツールの一覧（一覧の定義より前に使用するため、テストが設定する）。
std::span<const Tool> gTools;
bool gSession = false;
bool gStarted = false;

Json record(const char *name) {
	gCalls.emplace_back(name);
	return Json::object();
}

Json first(const Json &, const ToolContext &, ToolStatus &) { return record("first"); }

// putLater は回の途中で要求を配置する（並べた後に配置された要求は次の回に回される）。
Json putLater(const Json &, const ToolContext &, ToolStatus &) {
	gCaller->put("zzz", R"({"tool":"first"})");
	return record("put_later");
}

// nests はツールの中で Vectorworks がイベントを処理し、周期処理が再度届いたことを模倣する
// （受付処理の入れ子）。
Json nests(const Json &, const ToolContext &, ToolStatus &) {
	gCaller->put("c", R"({"tool":"first"})");
	gServer->serve(gTools, kNow);
	return record("nests");
}

// quitFails は終了を依頼してから失敗する（失敗したツールの終了の依頼は発行しない）。
Json quitFails(const Json &, const ToolContext &, ToolStatus &status) {
	record("quit_fails");
	status.code = ErrorCode::Internal;
	status.message = "失敗した";
	status.quit = true;
	return Json();
}

Json quits(const Json &, const ToolContext &, ToolStatus &status) {
	status.quit = true;
	return record("quits");
}

// badDetail は detail にオブジェクトでない値を返す（ツールの不具合）。
Json badDetail(const Json &, const ToolContext &, ToolStatus &status) {
	status.code = ErrorCode::InvalidArgs;
	status.message = "引数が誤っています";
	status.detail = Json::array();
	return record("bad_detail");
}

Json throwsInt(const Json &, const ToolContext &, ToolStatus &) {
	record("throws_int");
	// std::exception から派生しない例外も捕捉することを確認するため、意図的に int を投げる。
	// NOLINTNEXTLINE(bugprone-std-exception-baseclass)
	throw 42;
}

Json readContext(const Json &, const ToolContext &context, ToolStatus &) {
	gSession = context.session;
	gStarted = context.startup.started;
	return record("context");
}

const Tool kTools[] = {
    {{"first", "", "{}", ToolKind::Read}, first},
    {{"put_later", "", "{}", ToolKind::Read}, putLater},
    {{"nests", "", "{}", ToolKind::Read}, nests},
    {{"quit_fails", "", "{}", ToolKind::App}, quitFails},
    {{"quits", "", "{}", ToolKind::App}, quits},
    {{"bad_detail", "", "{}", ToolKind::Read}, badDetail},
    {{"throws_int", "", "{}", ToolKind::Read}, throwsInt},
    {{"context", "", "{}", ToolKind::Read}, readContext},
};

// startSession は占有のセッション ID を受け取る。
std::string startSession(Server &server, Caller &caller, const std::string &id) {
	caller.put(id, R"({"tool":"@session_start"})");
	server.serve(kTools, kNow);
	std::string session = caller.session(id);
	CHECK(!session.empty());
	return session;
}

}

TEST(ServeLeavesRequestsPutDuringTheRoundForTheNext) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	gCaller = &caller;
	caller.put("a", R"({"tool":"put_later"})");
	caller.put("b", R"({"tool":"first"})");
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
	CHECK_EQ(gCalls, (std::vector<std::string>{"put_later", "first"}));
	CHECK(std::filesystem::exists(dir.path() / "zzz.req.json"));
	server.serve(kTools, kNow);
	CHECK_EQ(gCalls, (std::vector<std::string>{"put_later", "first", "first"}));
	gCaller = nullptr;
}

TEST(ServeDoesNotQuitWhenTheToolFails) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::string session = startSession(server, caller, "0");
	caller.put("1", R"({"tool":"quit_fails","session":")" + session + R"("})");
	caller.put("2", R"({"tool":"first"})");
	caller.put("3", R"({"tool":"first","session":")" + session + R"("})");
	VwCli::core::ServeResult result = server.serve(kTools, kNow);
	CHECK(!result.quit);
	CHECK_EQ(caller.code("1"), std::string("internal"));
	CHECK(caller.ok("2"));
	CHECK(caller.ok("3"));
	CHECK_EQ(gCalls, (std::vector<std::string>{"quit_fails", "first", "first"}));
}

// detail がオブジェクトでなければ、ツールの不具合として internal で応答し、detail を付けない。
TEST(ServeAnswersInternalForADetailThatIsNotAnObject) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"bad_detail"})");
	server.serve(kTools, kNow);
	CHECK_EQ(caller.code("1"), std::string("internal"));
	CHECK(!caller.response("1").find("detail"));
}

// 受付処理の入れ子では、外の回が並べた要求（b・s）も、回の途中に配置された要求（c）も、解析せずに
// busy で拒否する。占有のセッション ID は発行しない。外の回は、拒否された要求を取り出せずに飛ばす。
// 待機標識は入れ子でも確認する（解放されていれば応答せずに削除し、存在しなければ no_wait）。
TEST(ServeRefusesEveryRequestWhileNested) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	gCaller = &caller;
	gServer = &server;
	gTools = kTools;
	// 先にロックを取得する（取得する際の整理で、待機標識が解放された・存在しない要求が消えない
	// ように）。
	server.serve(kTools, kNow);
	caller.put("a", R"({"tool":"nests"})");
	caller.put("b", R"({"tool":"first"})");
	caller.put("s", R"({"tool":"@session_start"})");
	CHECK(files::write(dir.sub("r.wait"), ""));
	CHECK(files::write(dir.sub("r.req.json"), R"({"tool":"first"})"));
	CHECK(files::write(dir.sub("w.req.json"), R"({"tool":"first"})"));
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
	CHECK_EQ(gCalls, (std::vector<std::string>{"nests"}));
	CHECK(!std::filesystem::exists(dir.path() / "r.res.json"));
	CHECK(!std::filesystem::exists(dir.path() / "r.wait"));
	CHECK_EQ(caller.code("w"), std::string("no_wait"));
	CHECK(caller.ok("a"));
	CHECK_EQ(caller.code("b"), std::string("busy"));
	CHECK_EQ(caller.code("c"), std::string("busy"));
	CHECK_EQ(caller.code("s"), std::string("busy"));
	CHECK(!server.inSession());
	// 入れ子から戻った後は、通常通り受け付ける。
	caller.put("d", R"({"tool":"first"})");
	server.serve(kTools, kNow);
	CHECK(caller.ok("d"));
	gCaller = nullptr;
	gServer = nullptr;
}

// 確かめられない待機標識の要求は、実行せずに no_wait
// で応える（待っているかが分からないので、実行しない側に
// 倒す）。待機標識は消さない。読めなくできない環境（POSIX の root）では確かめない。
TEST(ServeRefusesARequestWhoseWaitCannotBeChecked) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	// 先にロックを取る（取るときの掃除で消えないように。確かめられない待機標識の id
	// は掃除も残す）。
	server.serve(kTools, kNow);
	CHECK(files::write(dir.sub("u.wait"), ""));
	CHECK(files::write(dir.sub("u.req.json"), R"({"tool":"first"})"));
	if (!VwCli::test::denyRead(dir.path() / "u.wait")) {
		return;
	}
	server.serve(kTools, kNow);
	VwCli::test::allowRead(dir.path() / "u.wait");
	CHECK(gCalls.empty());
	CHECK_EQ(caller.code("u"), std::string("no_wait"));
	CHECK(!std::filesystem::exists(dir.path() / "u.req.json"));
	CHECK(std::filesystem::exists(dir.path() / "u.wait"));
}

TEST(ServeAnswersAnyExceptionWithInternal) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"throws_int"})");
	caller.put("2", R"({"tool":"first"})");
	server.serve(kTools, kNow);
	CHECK_EQ(caller.code("1"), std::string("internal"));
	CHECK(caller.ok("2"));
}

TEST(ServeTellsToolsWhetherASessionIsIssued) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"context"})");
	server.serve(kTools, kNow);
	CHECK(!gSession);
	CHECK(!server.inSession());
	std::string session = startSession(server, caller, "2");
	CHECK(server.inSession());
	caller.put("3", R"({"tool":"context","session":")" + session + R"("})");
	server.serve(kTools, kNow);
	CHECK(gSession);
}

// 起動を終了したか（受付処理の外の状態）は、serve に渡した通りにツールへ渡す（quit が参照する）。
TEST(ServeTellsToolsWhetherVectorworksHasStarted) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	gStarted = true;
	caller.put("1", R"({"tool":"context"})");
	server.serve(kTools, kNow);
	CHECK(!gStarted);
	caller.put("2", R"({"tool":"context"})");
	server.serve(kTools, kNow, VwCli::core::Activity::Idle, VwCli::core::Startup{true, true});
	CHECK(gStarted);
}

TEST(ServeIssuesSessionsSpelledLikeIds) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::string session = startSession(server, caller, "1");
	CHECK(VwCli::core::protocol::validId(session));
	CHECK_EQ(session.size(), std::size_t{36});
	// セッション ID の先頭は、受付処理に渡した現在時刻と区切り。
	std::string head = std::to_string(kNow) + "-";
	CHECK_EQ(session.substr(0, head.size()), head);
	// 2 つ目のセッション ID は別の乱数になる。
	caller.put("2", R"({"tool":"@session_end","session":")" + session + R"("})");
	server.serve(kTools, kNow);
	CHECK(startSession(server, caller, "3") != session);
}

// 応答を書き込めなければ、占有のセッション ID を変更しない（呼び出し元が受け取れない
// セッション ID で占有が残る・応答が届かないにもかかわらず占有のみが消える、を回避する）。
// 書き込み途中の名前にディレクトリを配置すると、応答を書き込めない。
TEST(ServeKeepsTheSessionWhenTheReplyCannotBeWritten) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::filesystem::create_directory(dir.path() / "1.res.json.tmp");
	caller.put("1", R"({"tool":"@session_start"})");
	server.serve(kTools, kNow);
	CHECK(!server.inSession());
	std::string session = startSession(server, caller, "2");
	CHECK(server.inSession());
	std::filesystem::create_directory(dir.path() / "3.res.json.tmp");
	caller.put("3", R"({"tool":"@session_end","session":")" + session + R"("})");
	server.serve(kTools, kNow);
	CHECK(server.inSession());
}

// 終了の依頼も、応答を書き込めた場合のみ発行する（応答が届かないまま Vectorworks を終了させない）。
TEST(ServeDoesNotQuitWhenTheReplyCannotBeWritten) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::string session = startSession(server, caller, "0");
	std::filesystem::create_directory(dir.path() / "1.res.json.tmp");
	caller.put("1", R"({"tool":"quits","session":")" + session + R"("})");
	caller.put("2", R"({"tool":"first","session":")" + session + R"("})");
	CHECK(!server.serve(kTools, kNow).quit);
	CHECK_EQ(gCalls, (std::vector<std::string>{"quits", "first"}));
	// 書き込めれば終了を依頼し、その回の残りは取り出さない。
	caller.put("3", R"({"tool":"quits","session":")" + session + R"("})");
	caller.put("4", R"({"tool":"first","session":")" + session + R"("})");
	CHECK(server.serve(kTools, kNow).quit);
	CHECK(caller.ok("3"));
	CHECK(std::filesystem::exists(dir.path() / "4.req.json"));
}

TEST(ServeAnswersInternalForABrokenInputSchema) {
	const Tool broken[] = {{{"bad", "", "{", ToolKind::Read}, first}};
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"@tools"})");
	server.serve(broken, kNow);
	CHECK_EQ(caller.code("1"), std::string("internal"));
}

TEST(ServeCreatesTheSpool) {
	TempDir dir;
	Server server(dir.sub("cli/spool"));
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
	CHECK(std::filesystem::exists(dir.path() / "cli" / "spool" / "bridge.lock"));
}

TEST(ServeReportsAnErrorAndRetries) {
	TempDir dir;
	CHECK(files::write(dir.sub("spool"), ""));
	Server server(dir.sub("spool"));
	VwCli::core::ServeResult result = server.serve(kTools, kNow);
	CHECK(result.phase == Phase::Error);
	CHECK(!result.message.empty());
	// 次の回に再試行する。
	CHECK(files::remove(dir.sub("spool")));
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
}

// ロックのファイルを開けなければ（名前にディレクトリがある）、誤りを返し、次の回に再試行する。
// ディレクトリは開けないため、拒否したことにならず、再作成しない（通信規則「ロック」）。
TEST(ServeReportsAnErrorWhenTheLockCannotBeOpened) {
	TempDir dir;
	std::filesystem::create_directory(dir.path() / "bridge.lock");
	Server server(dir.utf8Path());
	VwCli::core::ServeResult result = server.serve(kTools, kNow);
	CHECK(result.phase == Phase::Error);
	CHECK(result.message.find("bridge.lock") != std::string::npos);
	CHECK(result.message.find("通常のファイルではありません") == std::string::npos);
	CHECK(std::filesystem::is_directory(dir.path() / "bridge.lock"));
	CHECK(std::filesystem::remove(dir.path() / "bridge.lock"));
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
}

// 読み取れない要求（名前にディレクトリがある）は通知せずに飛ばし（応答しない）、残りに応答する
// （通信規則「プラグイン側の義務」）。
TEST(ServeSkipsRequestsItCannotRead) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
	std::filesystem::create_directory(dir.path() / "1.req.json");
	caller.put("2", R"({"tool":"first"})");
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
	CHECK_EQ(gCalls, (std::vector<std::string>{"first"}));
	CHECK(caller.ok("2"));
	CHECK(!std::filesystem::exists(dir.path() / "1.res.json"));
}

TEST(ServeWritesValidUtf8EvenForUnknownToolNames) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", "{\"tool\":\"n\xffo\"}");
	server.serve(kTools, kNow);
	std::string text = VwCli::test::readFile(dir.path() / "1.res.json");
	CHECK(text.find("n\xef\xbf\xbdo") != std::string::npos);
	CHECK_EQ(caller.code("1"), std::string("unknown_tool"));
}
