#include "TestSupport.h"

#include "core/Files.h"
#include "core/Json.h"
#include "core/Protocol.h"
#include "core/Serve.h"

#include <algorithm>
#include <array>
#include <filesystem>
#include <map>
#include <memory>
#include <set>
#include <stdexcept>
#include <string>
#include <utility>
#include <vector>

// 通信規則の見本（protocol/fixtures。形は docs/fixtures.md）を読み、共通部がそのとおりに
// 振る舞うかを確かめる。CLI 側（cli/internal/spool・cli/internal/fakeplugin）のテストも
// 同じ見本を読む。

namespace protocol = VwCli::core::protocol;
namespace files = VwCli::core::files;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::Phase;
using VwCli::core::Server;
using VwCli::core::Tool;
using VwCli::core::ToolContext;
using VwCli::core::ToolError;
using VwCli::core::ToolFn;
using VwCli::core::ToolKind;
using VwCli::test::TempDir;

namespace {

std::filesystem::path fixtures() { return VWCLI_FIXTURES_DIR; }

void failAt(const std::string &where, const std::string &what) {
	VwCli::test::fail(__FILE__, __LINE__, where + ": " + what);
}

// load は見本のファイルを読む。見本は要求を入れ子に含む（深さ 64 の要求の結果など）ので、要求の
// 上限より深く読む。
Json load(const std::filesystem::path &path) {
	std::optional<Json> value = Json::parse(VwCli::test::readFile(path), 128);
	if (!value) {
		failAt(VwCli::test::utf8(path), "not JSON");
		return Json::object();
	}
	return *value;
}

const Json &get(const Json &object, const char *key) {
	static const Json null;
	const Json *value = object.find(key);
	return value ? *value : null;
}

std::string text(const Json &object, const char *key) {
	const Json &value = get(object, key);
	return value.isString() ? value.asString() : "";
}

// Key は見本のオブジェクトに書いてよいキーと、その値の型（空なら型を問わない）。
struct Key {
	std::string_view name;
	std::optional<Json::Type> type;
};

// shaped は見本のオブジェクトが、知らないキーと型の違う値を持たないかを確かめる。見本のキーの
// 綴りを誤ると、その確かめが黙って省かれるため（Go 側は DisallowUnknownFields で読む）。
bool shaped(const Json &value, std::initializer_list<Key> keys, const std::string &where) {
	if (!value.isObject()) {
		failAt(where, "not an object: " + value.dump());
		return false;
	}
	bool good = true;
	for (const Json::Member &member : value.members()) {
		const Key *key = nullptr;
		for (const Key &candidate : keys) {
			if (candidate.name == member.first) {
				key = &candidate;
			}
		}
		if (!key) {
			failAt(where, "unknown key: " + member.first);
			good = false;
		} else if (key->type && member.second.type() != *key->type) {
			failAt(where, "wrong type: " + member.first);
			good = false;
		}
	}
	return good;
}

// strings は配列の要素かオブジェクトの値が、すべて文字列かを確かめる。
bool strings(const Json &value, const std::string &where) {
	bool good = true;
	auto check = [&](const Json &item) {
		if (!item.isString()) {
			failAt(where, "not a string: " + item.dump());
			good = false;
		}
	};
	if (value.isArray()) {
		for (const Json &item : value.items()) {
			check(item);
		}
	} else if (value.isObject()) {
		for (const Json::Member &member : value.members()) {
			check(member.second);
		}
	}
	return good;
}

// checkCase は受け付けの見本の形（docs/fixtures.md）を確かめる。
bool checkCase(const Json &fixture, const std::string &where) {
	using T = Json::Type;
	bool good = shaped(fixture, {{"description", T::String}, {"steps", T::Array}}, where);
	const Json &steps = get(fixture, "steps");
	if (!steps.isArray() || steps.items().empty()) {
		failAt(where, "no steps");
		return false;
	}
	int index = 0;
	for (const Json &step : steps.items()) {
		std::string at = where + " step " + std::to_string(++index);
		good = shaped(step,
		              {{"description", T::String},
		               {"other_bridge", T::Bool},
		               {"activity", T::String},
		               {"waits", T::Object},
		               {"files", T::Object},
		               {"requests", T::Object},
		               {"expect", T::Object}},
		              at) &&
		       good;
		good = strings(get(step, "waits"), at) && strings(get(step, "files"), at) && good;
		if (get(step, "requests").isObject()) {
			for (const Json::Member &member : get(step, "requests").members()) {
				if (!member.second.isString()) {
					good = shaped(member.second, {{"fill", T::Object}}, at) &&
					       shaped(get(member.second, "fill"),
					              {{"head", T::String},
					               {"pad", T::String},
					               {"tail", T::String},
					               {"size", T::Number}},
					              at) &&
					       good;
				}
			}
		}
		const Json &expect = get(step, "expect");
		if (expect.isNull()) {
			continue;
		}
		good = shaped(expect,
		              {{"phase", T::String},
		               {"quit", T::Bool},
		               {"calls", T::Array},
		               {"responses", T::Object},
		               {"files", T::Array}},
		              at) &&
		       strings(get(expect, "files"), at) && good;
		if (get(expect, "calls").isArray()) {
			for (const Json &call : get(expect, "calls").items()) {
				good = shaped(call, {{"tool", T::String}, {"args", std::nullopt}}, at) && good;
				if (!call.find("tool")) {
					failAt(at, "a call has no tool");
					good = false;
				}
			}
		}
		if (get(expect, "responses").isObject()) {
			for (const Json::Member &member : get(expect, "responses").members()) {
				good =
				    shaped(member.second,
					       {{"ok", T::Bool}, {"code", T::String}, {"result", std::nullopt}}, at) &&
				    good;
				if (!member.second.find("ok")) {
					failAt(at, "the expected response for " + member.first + " has no ok");
					good = false;
				}
			}
		}
	}
	return good;
}

// --- 偽の道具の表（fake-tools.json） ---------------------------------------------------------

struct Fake {
	std::string name;
	std::string description;
	std::string inputSchema;
	ToolKind kind;
	std::string does;
};

struct Call {
	std::string tool;
	Json args;
};

std::vector<Fake> gFakes;
std::vector<Call> gCalls;

// runFake は I 番目の偽の道具。道具の中身は自分の名前を受け取らないので、番号ごとに作る。
template <std::size_t I> Json runFake(const Json &args, const ToolContext &, ToolError &error) {
	const Fake &fake = gFakes.at(I);
	gCalls.push_back({fake.name, args});
	if (fake.does == "echo") {
		return args;
	}
	if (fake.does == "empty") {
		return Json::object();
	}
	if (fake.does == "invalid_args") {
		error = ToolError{ErrorCode::InvalidArgs, "引数が誤っています"};
		return Json();
	}
	if (fake.does == "internal") {
		error = ToolError{ErrorCode::Internal, "道具が失敗しました"};
		return Json();
	}
	if (fake.does == "throw") {
		throw std::runtime_error("道具が例外を投げました");
	}
	failAt("fake-tools.json", "unknown does: " + fake.does);
	return Json();
}

template <std::size_t... I>
constexpr std::array<ToolFn, sizeof...(I)> makeRunners(std::index_sequence<I...>) {
	return {runFake<I>...};
}

constexpr auto kRunners = makeRunners(std::make_index_sequence<16>{});

std::vector<Tool> loadTools() {
	using T = Json::Type;
	gFakes.clear();
	Json table = load(fixtures() / "fake-tools.json");
	shaped(table, {{"tools", T::Array}}, "fake-tools.json");
	if (!get(table, "tools").isArray()) {
		return {};
	}
	for (const Json &entry : get(table, "tools").items()) {
		shaped(entry,
		       {{"name", T::String},
		        {"description", T::String},
		        {"inputSchema", T::Object},
		        {"kind", T::String},
		        {"does", T::String}},
		       "fake-tools.json");
		std::string kind = text(entry, "kind");
		gFakes.push_back({text(entry, "name"), text(entry, "description"),
		                  get(entry, "inputSchema").dump(),
		                  kind == "read"    ? ToolKind::Read
		                  : kind == "write" ? ToolKind::Write
		                                    : ToolKind::App,
		                  text(entry, "does")});
		if (kind != "read" && kind != "write" && kind != "app") {
			failAt("fake-tools.json", "unknown kind: " + kind);
		}
	}
	CHECK(gFakes.size() <= kRunners.size());
	std::vector<Tool> tools;
	for (std::size_t i = 0; i < gFakes.size() && i < kRunners.size(); ++i) {
		const Fake &fake = gFakes[i];
		tools.push_back({fake.name.c_str(), fake.description.c_str(), fake.inputSchema.c_str(),
		                 fake.kind, kRunners[i]});
	}
	return tools;
}

// --- 受け付けの見本（serve/*.json） -----------------------------------------------------------

constexpr std::int64_t kNow = 1760000000000000000;
constexpr std::string_view kSessionMark = "$session";

// match は期待する値と実際の値を比べる。期待する値の文字列 $session は、id の綴りを満たす
// 文字列に合い、その値を発行された印として覚える。
bool match(const Json &expected, const Json &actual, std::string &session) {
	if (expected.isString() && expected.asString() == kSessionMark) {
		if (!actual.isString() || !protocol::validId(actual.asString())) {
			return false;
		}
		session = actual.asString();
		return true;
	}
	if (expected.type() != actual.type()) {
		return false;
	}
	if (expected.isObject()) {
		if (expected.members().size() != actual.members().size()) {
			return false;
		}
		for (const Json::Member &member : expected.members()) {
			const Json *other = actual.find(member.first);
			if (!other || !match(member.second, *other, session)) {
				return false;
			}
		}
		return true;
	}
	if (expected.isArray()) {
		if (expected.items().size() != actual.items().size()) {
			return false;
		}
		for (std::size_t i = 0; i < expected.items().size(); ++i) {
			if (!match(expected.items()[i], actual.items()[i], session)) {
				return false;
			}
		}
		return true;
	}
	return expected == actual;
}

class CaseRunner {
public:
	CaseRunner(std::string name, const std::vector<Tool> &tools)
	    : name_(std::move(name)), tools_(tools), spool_(dir_.sub("spool")), server_(spool_) {
		std::string error;
		CHECK(files::makeDirectories(spool_, error));
	}

	~CaseRunner() {
		// 一時ディレクトリを消す前に、掴んでいる待つ印を放す（Windows は開いたままでは消せない）。
		held_.clear();
	}

	void run(const Json &steps) {
		int index = 0;
		for (const Json &step : steps.items()) {
			runStep(step, index++);
		}
	}

private:
	std::string path(const std::string &name) const { return files::join(spool_, name); }

	void hold(const std::string &id) {
		auto lock = std::make_unique<files::Lock>();
		std::string error;
		if (lock->take(path(id + ".wait"), error) != files::Lock::Result::Taken) {
			failAt(where_, "cannot hold " + id + ".wait: " + error);
		}
		held_[id] = std::move(lock);
	}

	std::string request(const Json &spec) {
		if (spec.isString()) {
			std::string body = VwCli::test::readFile(fixtures() / "requests" / spec.asString());
			for (std::size_t at = body.find(kSessionMark); at != std::string::npos;
			     at = body.find(kSessionMark, at)) {
				if (session_.empty()) {
					failAt(where_, "no session has been issued for " + spec.asString());
				}
				body.replace(at, kSessionMark.size(), session_);
				at += session_.size();
			}
			return body;
		}
		const Json &fill = get(spec, "fill");
		std::string head = text(fill, "head");
		std::string tail = text(fill, "tail");
		std::string pad = text(fill, "pad");
		std::size_t size = static_cast<std::size_t>(get(fill, "size").asInt64().value_or(0));
		if (pad.size() != 1 || size < head.size() + tail.size()) {
			failAt(where_, "bad fill");
			return {};
		}
		return head + std::string(size - head.size() - tail.size(), pad[0]) + tail;
	}

	std::vector<std::string> listing() const {
		std::vector<std::string> names = files::list(spool_).value_or(std::vector<std::string>{});
		std::sort(names.begin(), names.end());
		return names;
	}

	void runStep(const Json &step, int index) {
		where_ = name_ + " step " + std::to_string(index + 1);

		files::Lock other;
		if (get(step, "other_bridge").isBool() && get(step, "other_bridge").asBool()) {
			std::string error;
			CHECK(other.take(path(std::string(protocol::kLockFile)), error) ==
			      files::Lock::Result::Taken);
		}

		// 置く: 待つ印 → ファイル → 要求。要求の待つ印は、waits に無ければ掴む。
		std::map<std::string, std::string> waits;
		if (get(step, "waits").isObject()) {
			for (const Json::Member &member : get(step, "waits").members()) {
				waits[member.first] = member.second.asString();
			}
		}
		if (get(step, "requests").isObject()) {
			for (const Json::Member &member : get(step, "requests").members()) {
				waits.emplace(member.first, "held");
			}
		}
		for (const auto &[id, state] : waits) {
			if (state == "held") {
				hold(id);
			} else if (state == "released") {
				CHECK(files::write(path(id + ".wait"), ""));
			} else if (state != "missing") {
				failAt(where_, "unknown wait state: " + state);
			}
		}
		std::set<std::string> put;
		if (get(step, "files").isObject()) {
			for (const Json::Member &member : get(step, "files").members()) {
				CHECK(files::write(path(member.first), member.second.asString()));
				put.insert(member.first);
			}
		}
		if (get(step, "requests").isObject()) {
			for (const Json::Member &member : get(step, "requests").members()) {
				CHECK(files::write(path(member.first + ".req.json"), request(member.second)));
			}
		}

		gCalls.clear();
		VwCli::core::Activity activity = VwCli::core::Activity::Idle;
		if (std::string name = text(step, "activity"); name == "quitting") {
			activity = VwCli::core::Activity::Quitting;
		} else if (!name.empty()) {
			failAt(where_, "unknown activity: " + name);
		}
		VwCli::core::ServeResult result = server_.serve(tools_, kNow + index, activity);
		const Json &expect = get(step, "expect");

		std::string phase = text(expect, "phase");
		std::string actualPhase = result.phase == Phase::Serving   ? "serving"
		                          : result.phase == Phase::Waiting ? "waiting"
		                                                           : "error";
		if ((phase.empty() ? "serving" : phase) != actualPhase) {
			failAt(where_, "phase " + actualPhase + " (" + result.message + ")");
		}
		bool quit = get(expect, "quit").isBool() && get(expect, "quit").asBool();
		if (quit != result.quit) {
			failAt(where_, "quit " + std::to_string(result.quit));
		}
		checkCalls(get(expect, "calls"));
		std::vector<std::string> answered = checkResponses(get(expect, "responses"), put);
		if (get(expect, "files").isArray()) {
			std::vector<std::string> expected;
			for (const Json &name : get(expect, "files").items()) {
				expected.push_back(name.asString());
			}
			std::sort(expected.begin(), expected.end());
			if (listing() != expected) {
				failAt(where_, "files " + VwCli::test::show(listing()) + ", expected " +
				                   VwCli::test::show(expected));
			}
		}

		// 呼ぶ側が応答を受け取ったことを真似る（応答を消し、待つ印を放して消す）。
		for (const std::string &id : answered) {
			files::remove(path(id + std::string(protocol::kResponseSuffix)));
			held_.erase(id);
			files::remove(path(id + std::string(protocol::kWaitSuffix)));
		}
	}

	void checkCalls(const Json &expected) {
		std::size_t count = expected.isArray() ? expected.items().size() : 0;
		if (gCalls.size() != count) {
			failAt(where_,
			       std::to_string(gCalls.size()) + " calls, expected " + std::to_string(count));
			return;
		}
		for (std::size_t i = 0; i < count; ++i) {
			const Json &call = expected.items()[i];
			if (gCalls[i].tool != text(call, "tool")) {
				failAt(where_, "call " + std::to_string(i + 1) + " is " + gCalls[i].tool);
			}
			if (call.find("args") && !(gCalls[i].args == get(call, "args"))) {
				failAt(where_, "call " + std::to_string(i + 1) + " args " + gCalls[i].args.dump());
			}
		}
	}

	// checkResponses は応答を確かめ、確かめた id を返す。この回より前に置いたファイルを除き、
	// スプールにある応答はすべて期待に載っていなければならない。
	std::vector<std::string> checkResponses(const Json &expected,
	                                        const std::set<std::string> &put) {
		std::string suffix(protocol::kResponseSuffix);
		for (const std::string &name : listing()) {
			if (name.size() > suffix.size() && name.ends_with(suffix) && !put.count(name)) {
				std::string id = name.substr(0, name.size() - suffix.size());
				if (!expected.isObject() || !expected.find(id)) {
					failAt(where_, "unexpected response " + name);
				}
			}
		}
		std::vector<std::string> answered;
		if (!expected.isObject()) {
			return answered;
		}
		for (const Json::Member &member : expected.members()) {
			const std::string &id = member.first;
			const Json &want = member.second;
			answered.push_back(id);
			std::optional<std::string> body =
			    files::read(path(id + suffix), protocol::kMaxRequestBytes * 2);
			std::optional<Json> response =
			    body ? Json::parse(*body, protocol::kMaxNestingDepth) : std::nullopt;
			if (!response) {
				failAt(where_, "no response for " + id);
				continue;
			}
			bool ok = get(*response, "ok").isBool() && get(*response, "ok").asBool();
			bool wantOk = get(want, "ok").isBool() && get(want, "ok").asBool();
			bool good = ok == wantOk;
			if (!ok) {
				good = good && get(*response, "error").isString() &&
				       text(*response, "code") == text(want, "code");
			} else {
				good = good && response->find("result") &&
				       (!want.find("result") ||
				        match(get(want, "result"), get(*response, "result"), session_));
			}
			if (!good) {
				failAt(where_, id + " responded " + response->dump());
			}
		}
		return answered;
	}

	std::string name_;
	const std::vector<Tool> &tools_;
	TempDir dir_;
	std::string spool_;
	Server server_;
	std::map<std::string, std::unique_ptr<files::Lock>> held_;
	std::string session_;
	std::string where_;
};

std::set<std::string> stringSet(const Json &list) {
	std::set<std::string> values;
	for (const Json &value : list.items()) {
		values.insert(value.asString());
	}
	return values;
}

}

TEST(FixtureConstantsMatchTheCode) {
	using T = Json::Type;
	Json constants = load(fixtures() / "constants.json");
	CHECK(shaped(constants,
	             {{"protocol", T::Number},
	              {"app_dir", T::String},
	              {"spool_dir", T::String},
	              {"lock_file", T::String},
	              {"request_suffix", T::String},
	              {"response_suffix", T::String},
	              {"wait_suffix", T::String},
	              {"temp_suffix", T::String},
	              {"max_request_bytes", T::Number},
	              {"max_nesting_depth", T::Number},
	              {"reserved_tools", T::Array},
	              {"kinds", T::Array},
	              {"codes", T::Array}},
	             "constants.json"));
	CHECK_EQ(get(constants, "protocol").asInt64().value_or(0), std::int64_t{protocol::kVersion});
	CHECK_EQ(text(constants, "app_dir"), std::string(protocol::kAppDirName));
	CHECK_EQ(text(constants, "spool_dir"), std::string(protocol::kSpoolDirName));
	CHECK_EQ(text(constants, "lock_file"), std::string(protocol::kLockFile));
	CHECK_EQ(text(constants, "request_suffix"), std::string(protocol::kRequestSuffix));
	CHECK_EQ(text(constants, "response_suffix"), std::string(protocol::kResponseSuffix));
	CHECK_EQ(text(constants, "wait_suffix"), std::string(protocol::kWaitSuffix));
	CHECK_EQ(text(constants, "temp_suffix"), std::string(protocol::kTempSuffix));
	CHECK_EQ(get(constants, "max_request_bytes").asInt64().value_or(0),
	         static_cast<std::int64_t>(protocol::kMaxRequestBytes));
	CHECK_EQ(get(constants, "max_nesting_depth").asInt64().value_or(0),
	         std::int64_t{protocol::kMaxNestingDepth});
	CHECK(stringSet(get(constants, "reserved_tools")) ==
	      (std::set<std::string>{
	          std::string(protocol::kToolTools), std::string(protocol::kToolPing),
	          std::string(protocol::kToolSessionStart), std::string(protocol::kToolSessionEnd)}));
	std::vector<std::string> kinds;
	for (const Json &kind : get(constants, "kinds").items()) {
		kinds.push_back(kind.asString());
	}
	CHECK_EQ(kinds, (std::vector<std::string>{std::string(VwCli::core::kindName(ToolKind::Read)),
	                                          std::string(VwCli::core::kindName(ToolKind::Write)),
	                                          std::string(VwCli::core::kindName(ToolKind::App))}));
	CHECK(stringSet(get(constants, "codes")) ==
	      (std::set<std::string>{
	          std::string(protocol::kCodeInvalidRequest), std::string(protocol::kCodeUnknownTool),
	          std::string(protocol::kCodeInvalidArgs), std::string(protocol::kCodeNoWait),
	          std::string(protocol::kCodeBusy), std::string(protocol::kCodeOccupied),
	          std::string(protocol::kCodeNoSession), std::string(protocol::kCodeInternal)}));
}

TEST(FixtureIdsMatchValidId) {
	using T = Json::Type;
	Json ids = load(fixtures() / "ids.json");
	CHECK(shaped(ids, {{"valid", T::Array}, {"invalid", T::Array}}, "ids.json"));
	for (const Json &id : get(ids, "valid").items()) {
		if (!protocol::validId(id.asString())) {
			failAt("ids.json", "should be valid: " + id.asString());
		}
	}
	for (const Json &id : get(ids, "invalid").items()) {
		if (protocol::validId(id.asString())) {
			failAt("ids.json", "should be invalid: " + id.asString());
		}
	}
}

TEST(FixtureServeCases) {
	std::vector<Tool> tools = loadTools();
	std::vector<std::filesystem::path> cases;
	for (const auto &entry : std::filesystem::directory_iterator(fixtures() / "serve")) {
		cases.push_back(entry.path());
	}
	std::sort(cases.begin(), cases.end());
	CHECK(!cases.empty());
	for (const std::filesystem::path &path : cases) {
		Json fixture = load(path);
		std::string name = VwCli::test::utf8(path.filename());
		if (checkCase(fixture, name)) {
			CaseRunner(name, tools).run(get(fixture, "steps"));
		}
	}
}
