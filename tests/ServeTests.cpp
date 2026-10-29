#include "TestSupport.h"

#include "core/Files.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <filesystem>
#include <memory>
#include <span>
#include <string>
#include <vector>

// 受け付け 1 回のうち、通信規則の見本（FixtureTests.cpp）で表しにくいもの。

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

constexpr std::int64_t kNow = 1760000000000000000;

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
		std::optional<std::string> text = files::read(dir_.sub(id + ".res.json"), 1 << 20);
		if (!text) {
			return Json();
		}
		std::optional<Json> value = Json::parse(*text, 64);
		CHECK(value);
		return value ? *value : Json();
	}

	// ok は応答が成功か。応答が無ければ偽。
	bool ok(const std::string &id) const {
		Json value = response(id);
		const Json *ok = value.find("ok");
		return ok && ok->isBool() && ok->asBool();
	}

	std::string code(const std::string &id) const {
		Json value = response(id);
		const Json *code = value.find("code");
		return code && code->isString() ? code->asString() : "";
	}

private:
	const TempDir &dir_;
	std::vector<std::unique_ptr<files::Lock>> locks_;
};

std::vector<std::string> gCalls;
Caller *gCaller = nullptr;
Server *gServer = nullptr;
// nests が入れ子に渡す道具の表（表の定義より前に使うので、テストが置く）。
std::span<const Tool> gTools;
bool gSession = false;

Json record(const char *name) {
	gCalls.emplace_back(name);
	return Json::object();
}

Json first(const Json &, const ToolContext &, ToolStatus &) { return record("first"); }

// putLater は回の途中で要求を置く（並べたあとに置かれた要求は次の回に回る）。
Json putLater(const Json &, const ToolContext &, ToolStatus &) {
	gCaller->put("zzz", R"({"tool":"first"})");
	return record("put_later");
}

// nests は道具の中で Vectorworks
// がイベントを回し、刻みがまた届いたことを真似る（受け付けの入れ子）。
Json nests(const Json &, const ToolContext &, ToolStatus &) {
	gCaller->put("c", R"({"tool":"first"})");
	gServer->serve(gTools, kNow);
	return record("nests");
}

// quitFails は終了を頼んでから失敗する（失敗した道具の終了の依頼は出さない）。
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

// badDetail は detail にオブジェクトでない値を返す（道具の不具合）。
Json badDetail(const Json &, const ToolContext &, ToolStatus &status) {
	status.code = ErrorCode::InvalidArgs;
	status.message = "引数が誤っています";
	status.detail = Json::array();
	return record("bad_detail");
}

Json throwsInt(const Json &, const ToolContext &, ToolStatus &) {
	record("throws_int");
	throw 42;
}

Json readContext(const Json &, const ToolContext &context, ToolStatus &) {
	gSession = context.session;
	return record("context");
}

const Tool kTools[] = {
    {"first", "", "{}", ToolKind::Read, first},
    {"put_later", "", "{}", ToolKind::Read, putLater},
    {"nests", "", "{}", ToolKind::Read, nests},
    {"quit_fails", "", "{}", ToolKind::App, quitFails},
    {"quits", "", "{}", ToolKind::App, quits},
    {"bad_detail", "", "{}", ToolKind::Read, badDetail},
    {"throws_int", "", "{}", ToolKind::Read, throwsInt},
    {"context", "", "{}", ToolKind::Read, readContext},
};

// startSession は占有の印を受け取る。
std::string startSession(Server &server, Caller &caller, const std::string &id) {
	caller.put(id, R"({"tool":"session_start"})");
	server.serve(kTools, kNow);
	Json response = caller.response(id);
	const Json *result = response.find("result");
	const Json *session = result ? result->find("session") : nullptr;
	CHECK(session && session->isString());
	return session && session->isString() ? session->asString() : "";
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

// detail がオブジェクトでなければ、道具の不具合として internal で応え、detail を付けない。
TEST(ServeAnswersInternalForADetailThatIsNotAnObject) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"bad_detail"})");
	server.serve(kTools, kNow);
	CHECK_EQ(caller.code("1"), std::string("internal"));
	CHECK(!caller.response("1").find("detail"));
}

// 受け付けの入れ子では、外の回が並べた要求（b・s）も、回の途中に置かれた要求（c）も、解析せずに
// busy で断る。占有の印は発行しない。外の回は、断られた要求を取り出せずに飛ばす。待つ印は入れ子でも
// 確かめる（放されていれば応えずに消し、無ければ no_wait）。
TEST(ServeRefusesEveryRequestWhileNested) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	gCaller = &caller;
	gServer = &server;
	gTools = kTools;
	// 先にロックを取る（取るときの掃除で、待つ印の放された・無い要求が消えないように）。
	server.serve(kTools, kNow);
	caller.put("a", R"({"tool":"nests"})");
	caller.put("b", R"({"tool":"first"})");
	caller.put("s", R"({"tool":"session_start"})");
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
	// 入れ子から戻ったあとは、ふつうに受け付ける。
	caller.put("d", R"({"tool":"first"})");
	server.serve(kTools, kNow);
	CHECK(caller.ok("d"));
	gCaller = nullptr;
	gServer = nullptr;
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

TEST(ServeIssuesSessionsSpelledLikeIds) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::string session = startSession(server, caller, "1");
	CHECK(VwCli::core::protocol::validId(session));
	CHECK_EQ(session.size(), std::size_t{36});
	CHECK_EQ(session.substr(0, 20), std::string("1760000000000000000-"));
	// 2 つ目の印は別の乱数になる。
	caller.put("2", R"({"tool":"session_end","session":")" + session + R"("})");
	server.serve(kTools, kNow);
	CHECK(startSession(server, caller, "3") != session);
}

// 応答を書けなければ、占有の印を変えない（呼ぶ側が受け取れない印で占有が残る・応答が届かないのに
// 占有だけが消える、を避ける）。書きかけの名前にディレクトリを置くと、応答を書けない。
TEST(ServeKeepsTheSessionWhenTheReplyCannotBeWritten) {
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::filesystem::create_directory(dir.path() / "1.res.json.tmp");
	caller.put("1", R"({"tool":"session_start"})");
	server.serve(kTools, kNow);
	CHECK(!server.inSession());
	std::string session = startSession(server, caller, "2");
	CHECK(server.inSession());
	std::filesystem::create_directory(dir.path() / "3.res.json.tmp");
	caller.put("3", R"({"tool":"session_end","session":")" + session + R"("})");
	server.serve(kTools, kNow);
	CHECK(server.inSession());
}

// 終了の依頼も、応答を書けたときだけ出す（応答が届かないまま Vectorworks を終わらせない）。
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
	// 書ければ終了を頼み、その回の残りは取り出さない。
	caller.put("3", R"({"tool":"quits","session":")" + session + R"("})");
	caller.put("4", R"({"tool":"first","session":")" + session + R"("})");
	CHECK(server.serve(kTools, kNow).quit);
	CHECK(caller.ok("3"));
	CHECK(std::filesystem::exists(dir.path() / "4.req.json"));
}

TEST(ServeAnswersInternalForABrokenInputSchema) {
	const Tool broken[] = {{"bad", "", "{", ToolKind::Read, first}};
	TempDir dir;
	Server server(dir.utf8Path());
	Caller caller(dir);
	caller.put("1", R"({"tool":"tools"})");
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
	// 次の回にやり直す。
	CHECK(files::remove(dir.sub("spool")));
	CHECK(server.serve(kTools, kNow).phase == Phase::Serving);
}

// ロックのファイルを開けなければ（名前にディレクトリがある）、誤りを返し、次の回にやり直す。
// ディレクトリは開けないので、断ったことにならず、作り直さない（通信規則「ロック」）。
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

// 読めない要求（名前にディレクトリがある）は黙って飛ばし（応答しない）、残りに応える
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
