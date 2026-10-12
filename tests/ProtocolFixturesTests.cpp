//
// ProtocolFixturesTests.cpp
//
// 作法の見本（protocol/fixtures/）を C++ の実装に照らす。Go の偽のプラグインのテスト
// （cli/internal/fakeplugin/fixtures_test.go）も同じ見本を読むので、両側の振る舞いが
// 食い違えばどちらかが落ちる。見本の形は protocol/fixtures/README.md。
//

#include "FakeTools.h"
#include "SpoolHelpers.h"
#include "TestFramework.h"
#include "core/Bridge.h"
#include "core/Json.h"
#include "core/Serve.h"

#include <algorithm>
#include <cstddef>
#include <filesystem>
#include <iostream>
#include <map>
#include <memory>
#include <string>
#include <vector>

using namespace VwCli::core;
using vwtest::HeldFile;
using vwtest::Joined;
using vwtest::ListNames;
using vwtest::ReadFile;
using vwtest::TempDir;
using vwtest::WriteFile;

namespace {
const std::filesystem::path kFixtures = pathFromUtf8(VW_PROTOCOL_FIXTURES_DIR);

Json Load(const std::filesystem::path& path) {
	Json value;
	std::string error;
	if (!Json::parse(ReadFile(path), value, error))
		std::cout << "    cannot read " << utf8FromPath(path) << ": " << error << "\n";
	return value;
}

std::string Base64Decode(const std::string& text) {
	static const std::string kAlphabet =
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
	std::string out;
	unsigned int buffer = 0;
	int bits = 0;
	for (const char ch : text) {
		const std::size_t value = kAlphabet.find(ch);
		if (value == std::string::npos)
			continue;
		buffer = (buffer << 6U) | static_cast<unsigned int>(value);
		bits += 6;
		if (bits >= 8) {
			bits -= 8;
			out += static_cast<char>((buffer >> static_cast<unsigned int>(bits)) & 0xFFU);
		}
	}
	return out;
}

// 見本の文字列のうち "$session" を、session_start が返した印に置き換える。
Json Substitute(const Json& value, const std::string& session) {
	if (value.isString() && value.asString() == "$session")
		return Json::string(session);
	if (value.isObject()) {
		Json made = Json::object();
		for (const auto& member : value.members())
			made.set(member.first, Substitute(member.second, session));
		return made;
	}
	if (value.isArray()) {
		Json made = Json::array();
		for (const Json& item : value.items())
			made.push(Substitute(item, session));
		return made;
	}
	return value;
}

// 要求のテキスト（見本の json / text / base64 / padded / nested のどれか）。
std::string RequestText(const Json& request, const std::string& session) {
	if (request.has("json"))
		return Substitute(request.at("json"), session).dump();
	if (request.has("text"))
		return request.at("text").asString();
	if (request.has("base64"))
		return Base64Decode(request.at("base64").asString());
	if (request.has("padded")) {
		// ちょうどその大きさの ping の要求。
		const std::string head = R"({"tool":"ping","args":{"pad":")";
		const std::string tail = R"("}})";
		const auto size = static_cast<std::size_t>(request.at("padded").asNumber());
		return head + std::string(size - head.size() - tail.size(), 'x') + tail;
	}
	if (request.has("nested")) {
		// ちょうどその深さの ping の要求（外側と args で 2、残りを配列で重ねる）。
		const auto depth = static_cast<std::size_t>(request.at("nested").asNumber());
		return R"({"tool":"ping","args":{"a":)" + std::string(depth - 2, '[') +
			   std::string(depth - 2, ']') + "}}";
	}
	return {};
}

// 応答を見本と比べる。"$any" は何にでも合い、"$session" は印として受け取る。
bool Matches(const Json& expected, const Json& actual, std::string& session) {
	if (expected.isString() && expected.asString() == "$any")
		return true;
	if (expected.isString() && expected.asString() == "$session") {
		if (!actual.isString() || !isValidBridgeId(actual.asString()))
			return false;
		session = actual.asString();
		return true;
	}
	if (expected.isObject() && actual.isObject()) {
		if (expected.members().size() != actual.members().size())
			return false;
		return std::all_of(
			expected.members().begin(), expected.members().end(), [&](const auto& member) {
				return actual.has(member.first) &&
					   Matches(member.second, actual.at(member.first), session);
			});
	}
	return expected.equals(actual);
}

// 応答のうち見本が比べるもの（ok・code・result）。error は人向けの文言なので、空でない
// ことだけを確かめる。
Json Comparable(const Json& response, bool& errorOk) {
	Json made = Json::object();
	made.set("ok", response.at("ok"));
	if (response.has("code"))
		made.set("code", response.at("code"));
	if (response.has("result"))
		made.set("result", response.at("result"));
	const bool ok = response.at("ok").asBool(false);
	errorOk = ok ? !response.has("error") : !response.at("error").asString().empty();
	return made;
}

const char* PhaseName(ServePhase phase) {
	switch (phase) {
	case ServePhase::Serving:
		return "serving";
	case ServePhase::Waiting:
		return "waiting";
	case ServePhase::Error:
		return "error";
	}
	return "?";
}

// 見本 1 つを走らせる。食い違いの数を返す。
int RunCase(const std::filesystem::path& path) {
	int failures = 0;
	const std::string name = utf8FromPath(path.filename());
	const Json fixture = Load(path);
	const TempDir temp;
	const std::filesystem::path& dir = temp.path();
	Server server(dir);
	std::map<std::string, std::unique_ptr<HeldFile>> waits;
	std::string session;

	int roundNumber = 0;
	for (const Json& round : fixture.at("rounds").items()) {
		++roundNumber;
		const std::string where = name + " round " + std::to_string(roundNumber);
		for (const Json& file : round.at("files").items())
			WriteFile(dir / pathFromUtf8(file.at("name").asString()), file.at("text").asString());
		for (const auto& wait : round.at("waits").members()) {
			const std::filesystem::path waitPath = dir / (wait.first + kBridgeWaitSuffix);
			if (wait.second.asString() == "held")
				waits[wait.first] = std::make_unique<HeldFile>(waitPath);
			else
				WriteFile(waitPath, "");
		}
		for (const Json& request : round.at("requests").items()) {
			const std::string id = request.at("id").asString();
			const std::string wait = request.at("wait").asString("held");
			const std::filesystem::path waitPath = dir / (id + kBridgeWaitSuffix);
			if (wait == "held")
				waits[id] = std::make_unique<HeldFile>(waitPath);
			else if (wait == "released")
				WriteFile(waitPath, "");
			WriteFile(dir / (id + kBridgeRequestSuffix), RequestText(request, session));
		}

		std::unique_ptr<HeldFile> other;
		if (round.at("lock_held_elsewhere").asBool(false))
			other = std::make_unique<HeldFile>(dir / kBridgeLockFile);
		const ServeResult result = server.serve(vwtest::FakeTools());
		other.reset();

		const Json& expect = round.at("expect");
		if (std::string(PhaseName(result.phase)) != expect.at("phase").asString()) {
			++failures;
			std::cout << "    " << where << ": phase " << PhaseName(result.phase) << "\n";
		}
		if (result.quit != expect.at("quit").asBool(false)) {
			++failures;
			std::cout << "    " << where << ": quit " << result.quit << "\n";
		}

		std::vector<std::string> files;
		for (const Json& file : expect.at("files").items())
			files.push_back(file.asString());
		std::sort(files.begin(), files.end());
		const std::string actualFiles = Joined(ListNames(dir));
		if (actualFiles != Joined(files)) {
			++failures;
			std::cout << "    " << where << ": files [" << actualFiles << "] want ["
					  << Joined(files) << "]\n";
		}

		// 応えられたものは呼ぶ側の真似として読んで消し、待つ印を放して消す。
		for (const auto& expected : expect.at("responses").members()) {
			const std::string& id = expected.first;
			const std::filesystem::path responsePath = dir / (id + kBridgeResponseSuffix);
			const Json response = Load(responsePath);
			bool errorOk = false;
			const Json comparable = Comparable(response, errorOk);
			if (!Matches(expected.second, comparable, session) || !errorOk) {
				++failures;
				std::cout << "    " << where << ": " << id << " " << response.dump() << " want "
						  << expected.second.dump() << "\n";
			}
			std::filesystem::remove(responsePath);
			waits.erase(id);
			std::filesystem::remove(dir / (id + kBridgeWaitSuffix));
		}
	}
	return failures;
}
} // namespace

TEST(fixtures_constants) {
	const Json constants = Load(kFixtures / "constants.json");
	CHECK_EQ(constants.at("protocol").asNumber(), double(kBridgeProtocolVersion));
	CHECK_EQ(constants.at("app_dir_name").asString(), std::string(kBridgeAppDirName));
	CHECK_EQ(constants.at("spool_dir_name").asString(), std::string(kBridgeSpoolDirName));
	CHECK_EQ(constants.at("lock_file").asString(), std::string(kBridgeLockFile));
	CHECK_EQ(constants.at("request_suffix").asString(), std::string(kBridgeRequestSuffix));
	CHECK_EQ(constants.at("response_suffix").asString(), std::string(kBridgeResponseSuffix));
	CHECK_EQ(constants.at("wait_suffix").asString(), std::string(kBridgeWaitSuffix));
	CHECK_EQ(constants.at("temp_suffix").asString(), std::string(kBridgeTempSuffix));
	CHECK_EQ(constants.at("max_request_bytes").asNumber(), double(kBridgeMaxRequestBytes));
	CHECK_EQ(constants.at("max_nesting_depth").asNumber(), double(kBridgeMaxNestingDepth));
	CHECK_EQ(constants.at("max_id_length").asNumber(), double(kBridgeMaxIdLength));

	for (const Json& id : constants.at("valid_ids").items())
		CHECK(isValidBridgeId(id.asString()));
	for (const Json& id : constants.at("invalid_ids").items())
		CHECK(!isValidBridgeId(id.asString()));

	// 予約された道具: tools・session_* は受け付けが答え、ping は表に載せる。
	std::vector<std::string> reserved;
	for (const Json& tool : constants.at("reserved_tools").items())
		reserved.push_back(tool.asString());
	CHECK_EQ(Joined(reserved), std::string("tools ping session_start session_end"));
	for (const std::string& tool : reserved)
		CHECK(isBuiltinToolName(tool) == (tool != "ping"));

	std::vector<std::string> codes;
	for (const BridgeCode code :
		{BridgeCode::InvalidRequest, BridgeCode::UnknownTool, BridgeCode::InvalidArgs,
			BridgeCode::NoWait, BridgeCode::Busy, BridgeCode::NoSession, BridgeCode::Internal})
		codes.emplace_back(bridgeCodeName(code));
	std::vector<std::string> expectedCodes;
	for (const Json& code : constants.at("codes").items())
		expectedCodes.push_back(code.asString());
	CHECK_EQ(Joined(codes), Joined(expectedCodes));

	std::vector<std::string> kinds;
	for (const ToolKind kind : {ToolKind::Read, ToolKind::Write, ToolKind::App})
		kinds.emplace_back(toolKindName(kind));
	std::vector<std::string> expectedKinds;
	for (const Json& kind : constants.at("kinds").items())
		expectedKinds.push_back(kind.asString());
	CHECK_EQ(Joined(kinds), Joined(expectedKinds));
}

TEST(fixtures_serve) {
	std::vector<std::filesystem::path> cases;
	for (const auto& entry : std::filesystem::directory_iterator(kFixtures / "serve"))
		if (entry.path().extension() == ".json")
			cases.push_back(entry.path());
	std::sort(cases.begin(), cases.end());
	CHECK(!cases.empty());
	for (const auto& path : cases) {
		const int mismatches = RunCase(path);
		if (mismatches != 0)
			std::cout << "    " << utf8FromPath(path.filename()) << ": " << mismatches
					  << " mismatch(es)\n";
		failures += mismatches;
	}
}

TEST_MAIN();
