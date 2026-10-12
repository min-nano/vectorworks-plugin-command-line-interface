#include "TestSupport.h"

#include "core/Bridge.h"
#include "core/Files.h"
#include "core/Protocol.h"

#include <filesystem>
#include <string>
#include <vector>

#ifndef _WIN32
#include <sys/stat.h>
#endif

using VwCli::core::Bridge;
namespace files = VwCli::core::files;
using VwCli::test::TempDir;

namespace {

void put(const TempDir &dir, const std::string &name, const std::string &text = "") {
	CHECK(files::write(dir.sub(name), text));
}

bool exists(const TempDir &dir, const std::string &name) {
	return std::filesystem::exists(dir.path() / name);
}

} // namespace

TEST(BridgePrepareCreatesMissingParents) {
	TempDir dir;
	Bridge bridge(dir.sub("a/b/spool"));
	std::string error;
	CHECK(bridge.prepare(error));
	CHECK(std::filesystem::is_directory(dir.path() / "a" / "b" / "spool"));
	// 既にあれば何もしない。
	CHECK(bridge.prepare(error));
#ifndef _WIN32
	struct stat info{};
	CHECK(::stat(dir.sub("a/b/spool").c_str(), &info) == 0);
	CHECK_EQ(info.st_mode & 0777, 0700u);
	CHECK(::stat(dir.sub("a").c_str(), &info) == 0);
	CHECK_EQ(info.st_mode & 0777, 0700u);
#endif
}

TEST(BridgePrepareFailsWhenThePathIsAFile) {
	TempDir dir;
	put(dir, "spool");
	Bridge bridge(dir.sub("spool"));
	std::string error;
	CHECK(!bridge.prepare(error));
	CHECK(!error.empty());
}

TEST(BridgeLockIsExclusiveAndReleasedOnDestruction) {
	TempDir dir;
	std::string error;
	{
		Bridge first(dir.utf8Path());
		CHECK(first.lock(error) == files::Lock::Result::Taken);
		CHECK(first.locked());
		Bridge second(dir.utf8Path());
		CHECK(second.lock(error) == files::Lock::Result::Busy);
		CHECK(!second.locked());
	}
	Bridge third(dir.utf8Path());
	CHECK(third.lock(error) == files::Lock::Result::Taken);
	// bridge.lock は消さない。
	CHECK(exists(dir, "bridge.lock"));
}

TEST(BridgeLockFailsWhenTheSpoolIsMissing) {
	TempDir dir;
	Bridge bridge(dir.sub("missing"));
	std::string error;
	CHECK(bridge.lock(error) == files::Lock::Result::Failed);
	CHECK(!error.empty());
}

TEST(BridgePollReturnsValidIdsInFileNameOrder) {
	// 大文字と小文字だけが違う名前は、macOS と Windows では同じファイルになるので使わない。
	TempDir dir;
	for (const char *name :
	     {"b.req.json", "a.req.json", "a-b.req.json", "C.req.json", "0.req.json", "a.b.req.json",
	      ".req.json", "c.res.json", "d.req.json.tmp", "e.wait"}) {
		put(dir, name);
	}
	Bridge bridge(dir.utf8Path());
	CHECK_EQ(bridge.poll(), (std::vector<std::string>{"0", "C", "a-b", "a", "b"}));
}

TEST(BridgeTakeReadsThenRemoves) {
	TempDir dir;
	put(dir, "r.req.json", R"({"tool":"ping"})");
	Bridge bridge(dir.utf8Path());
	std::optional<std::string> text = bridge.take("r");
	CHECK(text && *text == R"({"tool":"ping"})");
	CHECK(!exists(dir, "r.req.json"));
	// 無くなっていた要求は飛ばす。
	CHECK(!bridge.take("r"));
}

TEST(BridgeTakeReadsOneByteOverTheLimit) {
	TempDir dir;
	std::size_t limit = VwCli::core::protocol::kMaxRequestBytes;
	put(dir, "big.req.json", std::string(limit + 100, 'x'));
	Bridge bridge(dir.utf8Path());
	std::optional<std::string> text = bridge.take("big");
	CHECK(text && text->size() == limit + 1);
	CHECK(!exists(dir, "big.req.json"));
}

TEST(BridgeWaitStateFollowsTheCaller) {
	TempDir dir;
	Bridge bridge(dir.utf8Path());
	CHECK(bridge.waitState("x") == files::WaitState::Missing);
	put(dir, "x.wait");
	CHECK(bridge.waitState("x") == files::WaitState::Released);
	{
		files::Lock caller;
		std::string error;
		CHECK(caller.take(dir.sub("y.wait"), error) == files::Lock::Result::Taken);
		CHECK(bridge.waitState("y") == files::WaitState::Held);
	}
	CHECK(bridge.waitState("y") == files::WaitState::Released);
	bridge.removeWait("y");
	CHECK(!exists(dir, "y.wait"));
}

TEST(BridgeReplyPublishesWithoutLeavingTemp) {
	TempDir dir;
	Bridge bridge(dir.utf8Path());
	CHECK(bridge.reply("r", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "r.res.json"), std::string(R"({"ok":true})"));
	CHECK(!exists(dir, "r.res.json.tmp"));
	// 同じ id の応答があれば置き換える。
	CHECK(bridge.reply("r", R"({"ok":false})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "r.res.json"), std::string(R"({"ok":false})"));
}

TEST(BridgeDefaultSpoolDirIsUnderTheAppDir) {
	std::optional<std::string> dir = VwCli::core::defaultSpoolDir();
	REQUIRE(dir);
	std::filesystem::path path = std::filesystem::path(
	    std::u8string(reinterpret_cast<const char8_t *>(dir->data()), dir->size()));
	CHECK_EQ(VwCli::test::utf8(path.filename()), std::string("spool"));
	CHECK_EQ(VwCli::test::utf8(path.parent_path().filename()), std::string("vectorworks2026-cli"));
#ifdef _WIN32
	CHECK(path.is_absolute());
#else
	CHECK_EQ(VwCli::test::utf8(path.parent_path().parent_path().filename()),
	         std::string("Application Support"));
	CHECK_EQ(VwCli::test::utf8(path.parent_path().parent_path().parent_path().filename()),
	         std::string("Library"));
#endif
}
