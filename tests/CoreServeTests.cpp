//
//	CoreServeTests.cpp
//
//	受け付け 1 回の手順（src/core/Serve.h）の単体テスト。偽の道具の表（tests/FakeTools.h）で、
//	ロック・掃除・取り出しの順・待つ印・読めない要求・占有・終了の依頼・道具の失敗を確かめる。
//

#include "FakeTools.h"
#include "SpoolHelpers.h"
#include "TestFramework.h"
#include "core/Bridge.h"
#include "core/Json.h"
#include "core/Serve.h"

#include <cstddef>
#include <filesystem>
#include <map>
#include <memory>
#include <set>
#include <string>
#include <utility>

using namespace VwCli::core;
using vwtest::Exists;
using vwtest::FakeTools;
using vwtest::HeldFile;
using vwtest::Joined;
using vwtest::ListNames;
using vwtest::ReadFile;
using vwtest::TempDir;
using vwtest::WriteFile;

namespace {
// 呼ぶ側の真似。待つ印を掴んでから要求を置き、応答を読む。
class Caller {
public:
	explicit Caller(std::filesystem::path dir)
	    : fDir(std::move(dir)) {}

	void put(const std::string& id, const std::string& text) {
		hold(id);
		WriteFile(fDir / (id + kBridgeRequestSuffix), text);
	}

	// 待つ印を置かずに要求だけ置く（作法に反する呼ぶ側）。
	void putWithoutWait(const std::string& id, const std::string& text) {
		WriteFile(fDir / (id + kBridgeRequestSuffix), text);
	}

	void hold(const std::string& id) {
		fWaits[id] = std::make_unique<HeldFile>(fDir / (id + kBridgeWaitSuffix));
	}

	void release(const std::string& id) {
		fWaits.erase(id);
	}

	// 応答を読んで消す。無ければ null。
	Json response(const std::string& id) {
		const std::filesystem::path path = fDir / (id + kBridgeResponseSuffix);
		if (!Exists(path))
			return Json::null();
		Json value;
		std::string error;
		Json::parse(ReadFile(path), value, error);
		std::filesystem::remove(path);
		return value;
	}

	std::string code(const std::string& id) {
		return response(id).at("code").asString("<none>");
	}

private:
	std::filesystem::path fDir;
	std::map<std::string, std::unique_ptr<HeldFile>> fWaits;
};

// 呼ばれた回数を数える道具を足した表。
ToolTable CountingTools(int& calls) {
	ToolTable tools = FakeTools();
	tools.push_back(Tool{"count", "数える", "{}", ToolKind::Read,
	  [&calls](const Json&, const ToolContext&, ToolError&) {
		  ++calls;
		  return Json::object();
	  }});
	return tools;
}

std::string Session(const Json& response) {
	return response.at("result").at("session").asString();
}
} // namespace

// --- ロックと掃除 -------------------------------------------------------------------

TEST(serve_waits_while_another_bridge_holds_the_lock) {
	const TempDir temp;
	Caller caller(temp.path());
	caller.put("a", R"({"tool":"ping"})");
	WriteFile(temp.path() / "old.res.json", "{}");
	HeldFile other(temp.path() / kBridgeLockFile);

	Server server(temp.path());
	const ServeResult result = server.serve(FakeTools());
	CHECK(result.phase == ServePhase::Waiting);
	CHECK(!result.quit);
	// 取り出さず、掃除もしない。
	CHECK_EQ(Joined(ListNames(temp.path())), std::string("a.req.json a.wait old.res.json"));

	// 相手が終われば次の回に引き継ぎ、待つ者の居ない残骸を消して応える。
	other.release();
	CHECK(server.serve(FakeTools()).phase == ServePhase::Serving);
	CHECK(caller.response("a").at("ok").asBool());
	CHECK_EQ(Joined(ListNames(temp.path())), std::string("a.wait"));
}

TEST(serve_reports_a_spool_it_cannot_prepare) {
	const TempDir temp;
	WriteFile(temp.path() / "blocked", "x");
	Server server(temp.path() / "blocked" / "spool");
	const ServeResult result = server.serve(FakeTools());
	CHECK(result.phase == ServePhase::Error);
	CHECK(!result.message.empty());
}

TEST(serve_creates_the_spool) {
	const TempDir temp;
	Server server(temp.path() / "app" / "spool");
	CHECK(server.serve(FakeTools()).phase == ServePhase::Serving);
	CHECK(Exists(temp.path() / "app" / "spool" / kBridgeLockFile));
}

TEST(serve_sweeps_only_when_it_takes_the_lock) {
	const TempDir temp;
	Server server(temp.path());
	CHECK(server.serve(FakeTools()).phase == ServePhase::Serving);
	// ロックを掴んだあとに置かれた応答（呼ぶ側がまだ読んでいない）は消さない。
	WriteFile(temp.path() / "late.res.json", "{}");
	CHECK(server.serve(FakeTools()).phase == ServePhase::Serving);
	CHECK(Exists(temp.path() / "late.res.json"));
}

// --- 取り出し ---------------------------------------------------------------------

TEST(serve_answers_in_name_order_only_what_was_listed_at_the_start) {
	const TempDir temp;
	Caller caller(temp.path());
	std::string order;
	ToolTable tools = FakeTools();
	tools.push_back(Tool{"mark", "印を付ける", "{}", ToolKind::Read,
	  [&order, &caller](const Json& args, const ToolContext&, ToolError&) {
		  order += args.at("m").asString();
		  // 回の途中で置かれた要求は次の回に回る。
		  if (order.size() == 1)
			  caller.put("0000", R"({"tool":"mark","args":{"m":"z"}})");
		  return Json::object();
	  }});
	caller.put("0003", R"({"tool":"mark","args":{"m":"c"}})");
	caller.put("0001", R"({"tool":"mark","args":{"m":"a"}})");
	caller.put("0002", R"({"tool":"mark","args":{"m":"b"}})");

	Server server(temp.path());
	server.serve(tools);
	CHECK_EQ(order, std::string("abc"));
	CHECK(Exists(temp.path() / "0000.req.json"));
	server.serve(tools);
	CHECK_EQ(order, std::string("abcz"));
}

TEST(serve_does_not_touch_requests_with_bad_ids) {
	const TempDir temp;
	Server server(temp.path());
	server.serve(FakeTools());
	// ロックを取ったあとに置かれたもの（取る前のものは掃除が消す）。
	WriteFile(temp.path() / "bad.id.req.json", R"({"tool":"ping"})");
	server.serve(FakeTools());
	CHECK_EQ(Joined(ListNames(temp.path())), std::string("bad.id.req.json"));
}

// --- 待つ印 -----------------------------------------------------------------------

TEST(serve_skips_requests_nobody_waits_for) {
	const TempDir temp;
	Caller caller(temp.path());
	int calls = 0;
	const ToolTable tools = CountingTools(calls);
	Server server(temp.path());
	server.serve(tools);

	// 呼ぶ側が放した（強制終了された）要求は実行も応答もしない。待つ印を消す。
	caller.put("a", R"({"tool":"count"})");
	caller.release("a");
	server.serve(tools);
	CHECK_EQ(calls, 0);
	CHECK(ListNames(temp.path()).empty());
}

TEST(serve_refuses_requests_without_a_wait_file) {
	const TempDir temp;
	Caller caller(temp.path());
	int calls = 0;
	const ToolTable tools = CountingTools(calls);
	Server server(temp.path());
	server.serve(tools);

	caller.putWithoutWait("a", R"({"tool":"count"})");
	server.serve(tools);
	CHECK_EQ(calls, 0);
	CHECK_EQ(caller.code("a"), std::string("no_wait"));
}

// --- 読めない要求 -------------------------------------------------------------------

TEST(serve_answers_unreadable_requests) {
	const TempDir temp;
	Caller caller(temp.path());
	caller.put("a", "{");
	caller.put("b", R"({"args":{}})");
	caller.put("c", R"({"tool":"echo","args":[]})");
	caller.put("d", R"({"tool":"echo","args":{"a":)" + std::string(70, '[') + std::string(70, ']') +
	                  "}}");
	caller.put("e", std::string(kBridgeMaxRequestBytes + 1, ' '));
	caller.put("f", R"({"tool":"echo","session":7})");
	Server server(temp.path());
	server.serve(FakeTools());
	for (const char* id : {"a", "b", "c", "d", "e", "f"}) {
		const Json response = caller.response(id);
		CHECK(!response.at("ok").asBool(true));
		CHECK_EQ(response.at("code").asString(), std::string("invalid_request"));
		CHECK(!response.at("error").asString().empty());
	}
	CHECK_EQ(Joined(ListNames(temp.path())),
	  std::string("a.wait b.wait c.wait d.wait e.wait f.wait"));
}

// --- 道具 -------------------------------------------------------------------------

TEST(serve_runs_tools_and_reports_their_failures) {
	const TempDir temp;
	Caller caller(temp.path());
	caller.put("a", R"({"tool":"echo","args":{"x":[1,"あ"]}})");
	caller.put("b", R"({"tool":"echo"})");
	caller.put("c", R"({"tool":"nope"})");
	caller.put("d", R"({"tool":"fail"})");
	caller.put("e", R"({"tool":"boom"})");
	caller.put("f", R"({"tool":"ping","unknown":true})");
	Server server(temp.path());
	server.serve(FakeTools());

	CHECK_EQ(caller.response("a").dump(),
	  std::string(R"({"ok":true,"result":{"args":{"x":[1,"あ"]}}})"));
	CHECK_EQ(caller.response("b").dump(), std::string(R"({"ok":true,"result":{"args":{}}})"));
	const Json unknown = caller.response("c");
	CHECK_EQ(unknown.at("code").asString(), std::string("unknown_tool"));
	CHECK(unknown.at("error").asString().find("nope") != std::string::npos);
	CHECK_EQ(caller.code("d"), std::string("invalid_args"));
	const Json boom = caller.response("e");
	CHECK_EQ(boom.at("code").asString(), std::string("internal"));
	CHECK(boom.at("error").asString().find("boom") != std::string::npos);
	CHECK_EQ(caller.response("f").dump(), std::string(R"({"ok":true,"result":{"pong":true}})"));
}

TEST(serve_limits_the_codes_a_tool_may_set) {
	const TempDir temp;
	Caller caller(temp.path());
	ToolTable tools = FakeTools();
	tools.push_back(Tool{"odd", "作法の種別を付けようとする", "{}", ToolKind::Read,
	  [](const Json&, const ToolContext&, ToolError& error) {
		  error.failed = true;
		  error.code = BridgeCode::Busy;
		  return Json::null();
	  }});
	tools.push_back(Tool{"thrown", "例外でないものを投げる", "{}", ToolKind::Read,
	  [](const Json&, const ToolContext&, ToolError&) -> Json { throw 1; }});
	tools.push_back(Tool{"internal", "internal で失敗する", "{}", ToolKind::Read,
	  [](const Json&, const ToolContext&, ToolError& error) { return error.internal("図面が開かれていません"); }});
	caller.put("a", R"({"tool":"odd"})");
	caller.put("b", R"({"tool":"thrown"})");
	caller.put("c", R"({"tool":"internal"})");
	Server server(temp.path());
	server.serve(tools);
	const Json odd = caller.response("a");
	CHECK_EQ(odd.at("code").asString(), std::string("internal"));
	CHECK(!odd.at("error").asString().empty());
	CHECK_EQ(caller.code("b"), std::string("internal"));
	const Json internal = caller.response("c");
	CHECK_EQ(internal.at("code").asString(), std::string("internal"));
	CHECK_EQ(internal.at("error").asString(), std::string("図面が開かれていません"));
}

TEST(serve_lists_tools) {
	const TempDir temp;
	Caller caller(temp.path());
	caller.put("a", R"({"tool":"tools"})");
	Server server(temp.path());
	server.serve(FakeTools());
	const Json response = caller.response("a");
	CHECK(response.at("ok").asBool());
	const Json& result = response.at("result");
	CHECK_EQ(result.at("protocol").asNumber(), 5.0);
	const auto& items = result.at("tools").items();
	CHECK_EQ(items.size(), FakeTools().size());
	std::map<std::string, std::string> kinds;
	for (const Json& item : items) {
		kinds[item.at("name").asString()] = item.at("kind").asString();
		CHECK(item.at("inputSchema").isObject());
		CHECK(!item.at("description").asString().empty());
	}
	CHECK_EQ(kinds["ping"], std::string("read"));
	CHECK_EQ(kinds["quit"], std::string("app"));
	// 予約された道具は載せない。
	CHECK(kinds.count("tools") == 0);
	CHECK(kinds.count("session_start") == 0);
	CHECK_EQ(items[1].at("inputSchema").dump(), std::string(R"({"type":"object"})"));
}

// --- 占有 -------------------------------------------------------------------------

TEST(serve_sessions) {
	const TempDir temp;
	Caller caller(temp.path());
	Server server(temp.path());
	const ToolTable tools = FakeTools();

	// 占有されていない: 印の無い要求は実行し、印を載せた要求・占有の中だけの道具は断る。
	caller.put("01", R"({"tool":"echo"})");
	caller.put("02", R"({"tool":"echo","session":"stale"})");
	caller.put("03", R"({"tool":"quit"})");
	caller.put("04", R"({"tool":"session_end"})");
	caller.put("05", R"({"tool":"session_start","session":"stale"})");
	caller.put("06", R"({"tool":"ping","session":"stale"})");
	server.serve(tools);
	CHECK(caller.response("01").at("ok").asBool());
	CHECK_EQ(caller.code("02"), std::string("no_session"));
	CHECK_EQ(caller.code("03"), std::string("no_session"));
	CHECK_EQ(caller.code("04"), std::string("no_session"));
	CHECK_EQ(caller.code("05"), std::string("no_session"));
	CHECK(caller.response("06").at("ok").asBool());
	CHECK(!server.sessionActive());

	// 占有を始める。
	caller.put("10", R"({"tool":"session_start"})");
	server.serve(tools);
	const std::string session = Session(caller.response("10"));
	CHECK(isValidBridgeId(session));
	CHECK(server.sessionActive());

	// 占有されている: 同じ印の要求だけを実行する。tools と ping は誰にでも答える。
	const std::string with = R"(,"session":")" + session + "\"}";
	caller.put("11", R"({"tool":"echo")" + with);
	caller.put("12", R"({"tool":"echo"})");
	caller.put("13", R"({"tool":"echo","session":"other"})");
	caller.put("14", R"({"tool":"ping"})");
	caller.put("15", R"({"tool":"tools"})");
	caller.put("16", R"({"tool":"session_start"})");
	caller.put("17", R"({"tool":"session_start")" + with);
	caller.put("18", R"({"tool":"session_end","session":"other"})");
	caller.put("19", R"({"tool":"quit"})");
	caller.put("20", R"({"tool":"nope"})");
	caller.put("21", R"({"tool":"nope")" + with);
	server.serve(tools);
	CHECK(caller.response("11").at("ok").asBool());
	CHECK_EQ(caller.code("12"), std::string("busy"));
	CHECK_EQ(caller.code("13"), std::string("busy"));
	CHECK(caller.response("14").at("ok").asBool());
	CHECK(caller.response("15").at("ok").asBool());
	CHECK_EQ(caller.code("16"), std::string("busy"));
	CHECK_EQ(caller.code("17"), std::string("busy"));
	CHECK_EQ(caller.code("18"), std::string("busy"));
	CHECK_EQ(caller.code("19"), std::string("busy"));
	CHECK_EQ(caller.code("20"), std::string("busy"));
	CHECK_EQ(caller.code("21"), std::string("unknown_tool"));

	// 終える。古い印は no_session で断られる。
	caller.put("30", R"({"tool":"session_end")" + with);
	caller.put("31", R"({"tool":"echo")" + with);
	server.serve(tools);
	CHECK_EQ(caller.response("30").dump(), std::string(R"({"ok":true,"result":{"ended":true}})"));
	CHECK_EQ(caller.code("31"), std::string("no_session"));
	CHECK(!server.sessionActive());

	// 次の占有の印は前と違う。
	caller.put("40", R"({"tool":"session_start"})");
	server.serve(tools);
	const std::string next = Session(caller.response("40"));
	CHECK(isValidBridgeId(next));
	CHECK(next != session);
}

TEST(serve_session_is_lost_with_the_bridge) {
	// Vectorworks を起動し直すと占有は消え、前の印は no_session で断られる。
	const TempDir temp;
	Caller caller(temp.path());
	std::string session;
	{
		Server first(temp.path());
		caller.put("a", R"({"tool":"session_start"})");
		first.serve(FakeTools());
		session = Session(caller.response("a"));
	}
	Server second(temp.path());
	caller.put("b", R"({"tool":"echo","session":")" + session + "\"}");
	second.serve(FakeTools());
	CHECK_EQ(caller.code("b"), std::string("no_session"));
}

TEST(serve_tells_tools_whether_a_session_is_active) {
	const TempDir temp;
	Caller caller(temp.path());
	ToolTable tools = FakeTools();
	tools.push_back(Tool{"context", "状態を返す", "{}", ToolKind::Read,
	  [](const Json&, const ToolContext& context, ToolError&) { return Json::boolean(context.session); }});
	Server server(temp.path());
	caller.put("a", R"({"tool":"context"})");
	caller.put("b", R"({"tool":"session_start"})");
	server.serve(tools);
	CHECK_EQ(caller.response("a").at("result").dump(), std::string("false"));
	const std::string session = Session(caller.response("b"));
	caller.put("c", R"({"tool":"context","session":")" + session + "\"}");
	server.serve(tools);
	CHECK_EQ(caller.response("c").at("result").dump(), std::string("true"));
}

// --- 終了の依頼 ---------------------------------------------------------------------

TEST(serve_quit_answers_then_stops_the_round) {
	const TempDir temp;
	Caller caller(temp.path());
	Server server(temp.path());
	caller.put("a", R"({"tool":"session_start"})");
	server.serve(FakeTools());
	const std::string session = Session(caller.response("a"));

	caller.put("b", R"({"tool":"quit","session":")" + session + "\"}");
	caller.put("c", R"({"tool":"ping"})");
	const ServeResult result = server.serve(FakeTools());
	CHECK(result.quit);
	CHECK_EQ(caller.response("b").dump(), std::string(R"({"ok":true,"result":{"quitting":true}})"));
	// 残りは取り出さずに置いたまま（終了したら応えられない）。
	CHECK_EQ(Joined(ListNames(temp.path())), std::string("a.wait b.wait c.req.json c.wait"));
}

TEST(serve_refused_quit_does_not_stop_the_round) {
	const TempDir temp;
	Caller caller(temp.path());
	Server server(temp.path());
	caller.put("a", R"({"tool":"quit"})");
	caller.put("b", R"({"tool":"ping"})");
	const ServeResult result = server.serve(FakeTools());
	CHECK(!result.quit);
	CHECK_EQ(caller.code("a"), std::string("no_session"));
	CHECK(caller.response("b").at("ok").asBool());
}

// --- 応答を書けない -------------------------------------------------------------------

TEST(serve_reports_responses_it_cannot_write) {
	const TempDir temp;
	Caller caller(temp.path());
	Server server(temp.path());
	server.serve(FakeTools());
	std::filesystem::create_directory(temp.path() / "a.res.json");
	WriteFile(temp.path() / "a.res.json" / "x", "x");
	caller.put("a", R"({"tool":"ping"})");
	caller.put("b", R"({"tool":"ping"})");
	const ServeResult result = server.serve(FakeTools());
	CHECK(result.phase == ServePhase::Serving);
	CHECK(!result.message.empty());
	// ほかの要求には応える。
	CHECK(caller.response("b").at("ok").asBool());
}

// --- 表 ---------------------------------------------------------------------------

TEST(serve_tool_names) {
	CHECK(isValidToolName("ping"));
	CHECK(isValidToolName("layer_objects"));
	CHECK(isValidToolName("structure.import_ifc"));
	CHECK(isValidToolName("a.b.c9"));
	CHECK(!isValidToolName(""));
	CHECK(!isValidToolName("Ping"));
	CHECK(!isValidToolName("a..b"));
	CHECK(!isValidToolName(".a"));
	CHECK(!isValidToolName("a."));
	CHECK(!isValidToolName("a-b"));
	CHECK(isBuiltinToolName("tools"));
	CHECK(isBuiltinToolName("session_start"));
	CHECK(isBuiltinToolName("session_end"));
	CHECK(!isBuiltinToolName("ping"));
	CHECK_EQ(std::string(toolKindName(ToolKind::Read)), "read");
	CHECK_EQ(std::string(toolKindName(ToolKind::Write)), "write");
	CHECK_EQ(std::string(toolKindName(ToolKind::App)), "app");
}

TEST(serve_validate_tool_table) {
	std::string error;
	CHECK(validateToolTable(FakeTools(), error));
	CHECK(error.empty());

	const auto broken = [](auto change) {
		ToolTable tools = FakeTools();
		change(tools);
		std::string why;
		return !validateToolTable(tools, why) && !why.empty();
	};
	CHECK(broken([](ToolTable& t) { t[0].name = "Bad"; }));
	CHECK(broken([](ToolTable& t) { t[1].name = "tools"; }));
	CHECK(broken([](ToolTable& t) { t[1].name = "ping"; }));
	CHECK(broken([](ToolTable& t) { t[1].description.clear(); }));
	CHECK(broken([](ToolTable& t) { t[1].run = nullptr; }));
	CHECK(broken([](ToolTable& t) { t[1].inputSchema = "[]"; }));
	CHECK(broken([](ToolTable& t) { t[1].inputSchema = "{"; }));
	CHECK(broken([](ToolTable& t) { t[0].kind = ToolKind::Write; }));
}

TEST(serve_session_ids_are_random_ids) {
	std::set<std::string> seen;
	for (int i = 0; i < 100; ++i) {
		const std::string id = newSessionId();
		CHECK_EQ(id.size(), std::size_t(32));
		CHECK(isValidBridgeId(id));
		seen.insert(id);
	}
	CHECK_EQ(seen.size(), std::size_t(100));
}

TEST_MAIN();
