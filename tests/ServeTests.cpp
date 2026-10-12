#include "TestSupport.h"

#include "core/Files.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <filesystem>
#include <memory>
#include <string>
#include <vector>

// 受け付け 1 回のうち、作法の見本（FixtureTests.cpp）で表しにくいもの。

using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::Phase;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::core::ToolError;
using VwCli::core::ToolKind;
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
bool gSession = false;

Json record(const char *name) {
	gCalls.emplace_back(name);
	return Json::object();
}

Json first(const Json &, const ToolContext &, ToolError &) { return record("first"); }

// putLater は回の途中で要求を置く（並べたあとに置かれた要求は次の回に回る）。
Json putLater(const Json &, const ToolContext &, ToolError &) {
	gCaller->put("zzz", R"({"tool":"first"})");
	return record("put_later");
}

Json appFails(const Json &, const ToolContext &, ToolError &error) {
	record("app_fails");
	error = ToolError{ErrorCode::Internal, "失敗した"};
	return Json();
}

Json throwsInt(const Json &, const ToolContext &, ToolError &) {
	record("throws_int");
	throw 42;
}

Json readContext(const Json &, const ToolContext &context, ToolError &) {
	gSession = context.session;
	return record("context");
}

const Tool kTools[] = {
    {"first", "", "{}", ToolKind::Read, first},
    {"put_later", "", "{}", ToolKind::Read, putLater},
    {"app_fails", "", "{}", ToolKind::App, appFails},
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

TEST(ServeDoesNotQuitWhenTheAppToolFails) {
	TempDir dir;
	gCalls.clear();
	Server server(dir.utf8Path());
	Caller caller(dir);
	std::string session = startSession(server, caller, "0");
	caller.put("1", R"({"tool":"app_fails","session":")" + session + R"("})");
	caller.put("2", R"({"tool":"first"})");
	caller.put("3", R"({"tool":"first","session":")" + session + R"("})");
	VwCli::core::ServeResult result = server.serve(kTools, kNow);
	CHECK(!result.quit);
	CHECK_EQ(caller.code("1"), std::string("internal"));
	CHECK_EQ(caller.code("2"), std::string("busy"));
	CHECK(caller.ok("3"));
	CHECK_EQ(gCalls, (std::vector<std::string>{"app_fails", "first"}));
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
