//
// CoreBridgeTests.cpp
//
// スプール（src/core/Bridge.h）の単体テスト。場所・ロック・掃除・要求の取り出し・待つ印・
// 応答と、要求と応答の形を確かめる。呼ぶ側の真似は tests/SpoolHelpers.h。
//

#include "SpoolHelpers.h"
#include "TestFramework.h"
#include "core/Bridge.h"
#include "core/Json.h"

#include <cstddef>
#include <filesystem>
#include <string>
#include <vector>

#if defined(_WIN32)
#	include <windows.h>
#else
#	include <sys/stat.h>
#endif

using namespace VwCli::core;
using vwtest::Exists;
using vwtest::HeldFile;
using vwtest::Joined;
using vwtest::ListNames;
using vwtest::ReadFile;
using vwtest::TempDir;
using vwtest::WriteFile;

namespace {
std::filesystem::path In(const TempDir& temp, const std::string& name) {
	return temp.path() / name;
}
} // namespace

// --- 場所 -----------------------------------------------------------------------

TEST(bridge_spool_dir_is_under_the_cli_dir) {
	const std::filesystem::path base = std::filesystem::path("base");
	CHECK(bridgeSpoolDirUnder(base) == base / "vectorworks2026-cli" / "spool");

	std::filesystem::path dir;
	std::string error;
	CHECK(bridgeSpoolDir(dir, error));
	CHECK(error.empty());
	CHECK(dir.is_absolute());
	CHECK_EQ(utf8FromPath(dir.filename()), std::string("spool"));
	CHECK_EQ(utf8FromPath(dir.parent_path().filename()), std::string("vectorworks2026-cli"));
#if !defined(_WIN32)
	// mac はホームの Library/Application Support（環境変数 HOME は読まない）。
	CHECK_EQ(utf8FromPath(dir.parent_path().parent_path().filename()),
		std::string("Application Support"));
#endif
}

TEST(bridge_path_round_trips_utf8) {
	const std::string name = "利用者/スプール";
	CHECK_EQ(utf8FromPath(pathFromUtf8(name)), name);
}

// --- id と形 ---------------------------------------------------------------------

TEST(bridge_valid_ids) {
	CHECK(isValidBridgeId("a"));
	CHECK(isValidBridgeId("1760000000000000000-0a1b2c3d"));
	CHECK(isValidBridgeId("A_z-9"));
	CHECK(isValidBridgeId(std::string(64, 'x')));
	CHECK(!isValidBridgeId(""));
	CHECK(!isValidBridgeId(std::string(65, 'x')));
	CHECK(!isValidBridgeId("../x"));
	CHECK(!isValidBridgeId("a.b"));
	CHECK(!isValidBridgeId("a b"));
	CHECK(!isValidBridgeId("あ"));
}

TEST(bridge_code_names) {
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::InvalidRequest)), "invalid_request");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::UnknownTool)), "unknown_tool");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::InvalidArgs)), "invalid_args");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::NoWait)), "no_wait");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::Busy)), "busy");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::NoSession)), "no_session");
	CHECK_EQ(std::string(bridgeCodeName(BridgeCode::Internal)), "internal");
}

TEST(bridge_parse_request) {
	BridgeRequest request;
	std::string error;
	CHECK(
		parseBridgeRequest(R"({"tool":"layers","args":{"include_sheets":false}})", request, error));
	CHECK_EQ(request.tool, std::string("layers"));
	CHECK_EQ(request.args.dump(), std::string(R"({"include_sheets":false})"));
	CHECK(request.session.empty());

	// args の省略・null は {}。session の null・空は無いもの。知らないキーは無視する。
	CHECK(parseBridgeRequest(R"({"tool":"ping","session":null,"extra":[1]})", request, error));
	CHECK_EQ(request.args.dump(), std::string("{}"));
	CHECK(request.session.empty());
	CHECK(parseBridgeRequest(R"({"tool":"ping","args":null,"session":""})", request, error));
	CHECK_EQ(request.args.dump(), std::string("{}"));
	CHECK(request.session.empty());
	CHECK(parseBridgeRequest(R"({"tool":"ping","session":"s-1"})", request, error));
	CHECK_EQ(request.session, std::string("s-1"));
}

TEST(bridge_parse_request_rejects_unreadable_requests) {
	for (const char* text :
		{"", "{", "[]", "null", "\"ping\"", R"({})", R"({"tool":null})", R"({"tool":""})",
			R"({"tool":1})", R"({"TOOL":"ping"})", R"({"tool":"ping","args":[]})",
			R"({"tool":"ping","args":"x"})", R"({"tool":"ping","args":1})",
			R"({"tool":"ping","session":1})", R"({"tool":"ping","session":{}})"}) {
		BridgeRequest request;
		std::string error;
		CHECK(!parseBridgeRequest(text, request, error));
		CHECK(!error.empty());
	}
	// キーは大文字と小文字を区別する（ARGS は知らないキーとして無視される）。
	BridgeRequest request;
	std::string error;
	CHECK(parseBridgeRequest(R"({"tool":"ping","ARGS":[]})", request, error));
	CHECK_EQ(request.args.dump(), std::string("{}"));
}

TEST(bridge_dump_response) {
	Json result = Json::object();
	result.set("n", Json::integer(1));
	CHECK_EQ(dumpBridgeResponse(BridgeResponse::success(result)),
		std::string(R"({"ok":true,"result":{"n":1}})"));
	CHECK_EQ(dumpBridgeResponse(BridgeResponse::success(Json::null())),
		std::string(R"({"ok":true,"result":{}})"));
	CHECK_EQ(dumpBridgeResponse(BridgeResponse::failure(BridgeCode::UnknownTool, "知らない")),
		std::string(R"({"ok":false,"code":"unknown_tool","error":"知らない"})"));
}

// --- 用意とロック -------------------------------------------------------------------

TEST(bridge_prepare_creates_the_spool_and_its_parents) {
	const TempDir temp;
	BridgeSpool spool(temp.path() / "app" / "spool");
	std::string error;
	CHECK(spool.prepare(error));
	CHECK(std::filesystem::is_directory(spool.dir()));
	// あれば何もしない。
	CHECK(spool.prepare(error));
#if !defined(_WIN32)
	// 作ったものは 0700（親も）。
	for (const auto& dir : {spool.dir(), spool.dir().parent_path()}) {
		struct stat info {};
		CHECK(::stat(dir.c_str(), &info) == 0);
		CHECK_EQ(static_cast<int>(info.st_mode & 0777), 0700);
	}
#endif
}

TEST(bridge_prepare_fails_on_a_file_in_the_way) {
	const TempDir temp;
	WriteFile(In(temp, "blocked"), "x");
	BridgeSpool spool(In(temp, "blocked") / "spool");
	std::string error;
	CHECK(!spool.prepare(error));
	CHECK(!error.empty());

	BridgeSpool empty{std::filesystem::path()};
	CHECK(!empty.prepare(error));
	CHECK(!error.empty());
}

TEST(bridge_lock_is_exclusive_and_released_on_unlock) {
	const TempDir temp;
	BridgeSpool first(temp.path());
	BridgeSpool second(temp.path());
	std::string error;
	CHECK(first.lock(error) == LockResult::Locked);
	CHECK(first.locked());
	// 掴んでいれば何もしない。
	CHECK(first.lock(error) == LockResult::Locked);
	CHECK(Exists(In(temp, kBridgeLockFile)));

	// 同じスプールを見るもう 1 つの橋は取れない（2 つ目の Vectorworks）。
	CHECK(second.lock(error) == LockResult::HeldElsewhere);
	CHECK(error.empty());
	CHECK(!second.locked());

	// 前の橋が終われば引き継ぐ。
	first.unlock();
	CHECK(!first.locked());
	CHECK(second.lock(error) == LockResult::Locked);
	// bridge.lock は消さない。
	second.unlock();
	CHECK(Exists(In(temp, kBridgeLockFile)));
}

TEST(bridge_lock_is_released_when_the_spool_goes_away) {
	const TempDir temp;
	std::string error;
	{
		BridgeSpool first(temp.path());
		CHECK(first.lock(error) == LockResult::Locked);
	}
	BridgeSpool second(temp.path());
	CHECK(second.lock(error) == LockResult::Locked);
}

TEST(bridge_lock_seen_by_a_caller) {
	// 呼ぶ側が bridge.lock を確かめている間（掴んでいる間）は取れない。
	const TempDir temp;
	HeldFile caller(In(temp, kBridgeLockFile));
	CHECK(caller.held());
	BridgeSpool spool(temp.path());
	std::string error;
	CHECK(spool.lock(error) == LockResult::HeldElsewhere);
	caller.release();
	CHECK(spool.lock(error) == LockResult::Locked);
}

TEST(bridge_lock_fails_without_the_directory) {
	const TempDir temp;
	BridgeSpool spool(In(temp, "missing"));
	std::string error;
	CHECK(spool.lock(error) == LockResult::Failed);
	CHECK(!error.empty());
}

// --- 待つ印 -----------------------------------------------------------------------

TEST(bridge_wait_state) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	CHECK(spool.waitState("a") == WaitState::Missing);

	HeldFile caller(In(temp, "a.wait"));
	CHECK(caller.held());
	CHECK(spool.waitState("a") == WaitState::Held);
	// 確かめても掴みを奪わない。
	CHECK(spool.waitState("a") == WaitState::Held);

	caller.release();
	CHECK(spool.waitState("a") == WaitState::Released);
	CHECK(Exists(In(temp, "a.wait")));
	spool.removeWait("a");
	CHECK(!Exists(In(temp, "a.wait")));
	CHECK(spool.waitState("a") == WaitState::Missing);
}

// --- 掃除 -------------------------------------------------------------------------

TEST(bridge_sweep_keeps_only_files_of_waiting_callers) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	// 待っている呼ぶ側（a）のファイルは残す。
	HeldFile waiting(In(temp, "a.wait"));
	WriteFile(In(temp, "a.req.json"), "{}");
	WriteFile(In(temp, "a.res.json"), "{}");
	WriteFile(In(temp, "a.res.json.tmp"), "{}");
	// 放した呼ぶ側（b）のファイルは消す。
	{ const HeldFile released(In(temp, "b.wait")); }
	WriteFile(In(temp, "b.req.json"), "{}");
	// 待つ印の無い id（c）のファイルも消す。
	WriteFile(In(temp, "c.req.json"), "{}");
	WriteFile(In(temp, "c.res.json"), "{}");
	WriteFile(In(temp, "c.req.json.tmp"), "{}");
	// 作法のファイルでないもの・ロックは残す。
	WriteFile(In(temp, "notes.txt"), "x");
	std::string error;
	CHECK(spool.lock(error) == LockResult::Locked);

	CHECK_EQ(spool.sweep(), std::size_t(5));
	CHECK_EQ(Joined(ListNames(temp.path())),
		std::string("a.req.json a.res.json a.res.json.tmp a.wait notes.txt"));
	CHECK(Exists(In(temp, kBridgeLockFile)));
}

TEST(bridge_sweep_of_a_missing_directory_does_nothing) {
	const TempDir temp;
	BridgeSpool spool(In(temp, "missing"));
	CHECK_EQ(spool.sweep(), std::size_t(0));
}

// --- 取り出し -----------------------------------------------------------------------

TEST(bridge_poll_lists_valid_ids_in_name_order) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	WriteFile(In(temp, "0002.req.json"), "{}");
	WriteFile(In(temp, "0001.req.json"), "{}");
	WriteFile(In(temp, "B.req.json"), "{}");
	WriteFile(In(temp, "a.req.json"), "{}");
	// 綴りを満たさない id・書きかけ・応答・待つ印は並べない。
	WriteFile(In(temp, "bad.id.req.json"), "{}");
	WriteFile(In(temp, ".req.json"), "{}");
	WriteFile(In(temp, std::string(65, 'x') + ".req.json"), "{}");
	WriteFile(In(temp, "0003.req.json.tmp"), "{}");
	WriteFile(In(temp, "0004.res.json"), "{}");
	WriteFile(In(temp, "0005.wait"), "");
	CHECK_EQ(Joined(spool.poll()), std::string("0001 0002 B a"));

	BridgeSpool missing(In(temp, "missing"));
	CHECK(missing.poll().empty());
}

TEST(bridge_take_reads_then_removes) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	WriteFile(In(temp, "a.req.json"), R"({"tool":"ping"})");
	std::string text;
	CHECK(spool.take("a", text) == TakeResult::Taken);
	CHECK_EQ(text, std::string(R"({"tool":"ping"})"));
	CHECK(!Exists(In(temp, "a.req.json")));
	// 無くなっていた要求は黙って飛ばす。
	CHECK(spool.take("a", text) == TakeResult::Skipped);
}

TEST(bridge_take_caps_the_size) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	std::string text;
	// ちょうど上限は読む。
	WriteFile(In(temp, "a.req.json"), std::string(kBridgeMaxRequestBytes, ' '));
	CHECK(spool.take("a", text) == TakeResult::Taken);
	CHECK_EQ(text.size(), kBridgeMaxRequestBytes);
	// 超えるものは読まずに消す。
	WriteFile(In(temp, "b.req.json"), std::string(kBridgeMaxRequestBytes + 1, ' '));
	CHECK(spool.take("b", text) == TakeResult::TooLarge);
	CHECK(text.empty());
	CHECK(!Exists(In(temp, "b.req.json")));
}

#if defined(_WIN32)
TEST(bridge_take_skips_a_request_it_cannot_remove) {
	// ほかのプロセス（ウイルス対策ソフトなど）が消すことを許さずに開いている要求は、
	// 消せないので実行しない（次の回にやり直す）。
	const TempDir temp;
	BridgeSpool spool(temp.path());
	WriteFile(In(temp, "a.req.json"), R"({"tool":"ping"})");
	const HANDLE other = CreateFileW(In(temp, "a.req.json").c_str(), GENERIC_READ, FILE_SHARE_READ,
		nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
	CHECK(other != INVALID_HANDLE_VALUE);
	std::string text;
	CHECK(spool.take("a", text) == TakeResult::Skipped);
	CloseHandle(other);
	CHECK(spool.take("a", text) == TakeResult::Taken);
}
#endif

// --- 応答 -------------------------------------------------------------------------

TEST(bridge_reply_publishes_by_rename) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	std::string error;
	CHECK(spool.reply("a", BridgeResponse::success(Json::object()), error));
	CHECK_EQ(ReadFile(In(temp, "a.res.json")), std::string(R"({"ok":true,"result":{}})"));
	CHECK(!Exists(In(temp, "a.res.json.tmp")));

	// 残っていた応答は置き換える。
	CHECK(spool.reply("a", BridgeResponse::failure(BridgeCode::Internal, "x"), error));
	CHECK_EQ(ReadFile(In(temp, "a.res.json")),
		std::string(R"({"ok":false,"code":"internal","error":"x"})"));
}

TEST(bridge_reply_refuses_bad_ids_and_reports_failures) {
	const TempDir temp;
	BridgeSpool spool(temp.path());
	std::string error;
	CHECK(!spool.reply("../a", BridgeResponse::success(Json::object()), error));
	CHECK(!error.empty());

	BridgeSpool missing(In(temp, "missing"));
	CHECK(!missing.reply("a", BridgeResponse::success(Json::object()), error));
	CHECK(!error.empty());

	// 公開先がディレクトリで塞がっている。書きかけは残さない。
	std::filesystem::create_directory(In(temp, "b.res.json"));
	WriteFile(In(temp, "b.res.json") / "x", "x");
	CHECK(!spool.reply("b", BridgeResponse::success(Json::object()), error));
	CHECK(!error.empty());
	CHECK(!Exists(In(temp, "b.res.json.tmp")));
}

TEST_MAIN();
