#include "TestSupport.h"

#include "core/Bridge.h"
#include "core/Files.h"
#include "core/Protocol.h"

#include <filesystem>
#include <string>
#include <vector>

#ifndef _WIN32
#include <sys/stat.h>
#include <unistd.h>
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

}

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

#ifndef _WIN32
// FIFO などの通常のファイルでないものを置かれても、開いて待たない（待つとメインスレッドが止まる）。
// 待てばこのテストが終わらない。
TEST(BridgeDoesNotWaitOnFifos) {
	TempDir dir;
	for (const char *name : {"f.req.json", "f.wait", "g.res.json.tmp"}) {
		REQUIRE(::mkfifo(dir.sub(name).c_str(), 0600) == 0);
	}
	Bridge bridge(dir.utf8Path());
	// 読めない要求として飛ばし、消さない。
	CHECK(!bridge.take("f"));
	CHECK(exists(dir, "f.req.json"));
	// 通常のファイルでない待つ印は無いものとする（実行しない）。
	CHECK(bridge.waitState("f") == files::WaitState::Missing);
	// 書きかけに FIFO があれば応答を書けない。
	CHECK(!bridge.reply("g", R"({"ok":true})"));
	CHECK(!exists(dir, "g.res.json"));
}

// 書きかけのシンボリックリンクはたどらない（たどると、リンクの先を切り詰めてしまう）。
TEST(BridgeReplyDoesNotFollowSymlinks) {
	TempDir dir;
	put(dir, "target", "keep");
	REQUIRE(::symlink(dir.sub("target").c_str(), dir.sub("s.res.json.tmp").c_str()) == 0);
	Bridge bridge(dir.utf8Path());
	CHECK(!bridge.reply("s", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "target"), std::string("keep"));
	CHECK(!exists(dir, "s.res.json"));
}
#endif

#ifdef _WIN32
// CON・NUL などの名前も、デバイスではなくスプールのファイルとして扱う（\\?\ を付けて開く）。
TEST(BridgeTreatsReservedNamesAsFiles) {
	TempDir dir;
	Bridge bridge(dir.utf8Path());
	put(dir, "NUL.req.json", R"({"tool":"ping"})");
	CHECK_EQ(bridge.poll(), (std::vector<std::string>{"NUL"}));
	std::optional<std::string> text = bridge.take("NUL");
	CHECK(text && *text == R"({"tool":"ping"})");
	CHECK(bridge.poll().empty());
	// 無い待つ印は、開けてしまう NUL デバイスではなく、無いと分かる。
	CHECK(bridge.waitState("NUL") == files::WaitState::Missing);
	put(dir, "CON.wait");
	CHECK(bridge.waitState("CON") == files::WaitState::Released);
	bridge.removeWait("CON");
	CHECK(bridge.reply("AUX", R"({"ok":true})"));
	std::optional<std::string> response = files::read(dir.sub("AUX.res.json"), 1024);
	CHECK(response && *response == R"({"ok":true})");
	// 一時ディレクトリを消す std::filesystem が予約された名前を消せるとは限らないので、ここで消す。
	CHECK(files::remove(dir.sub("AUX.res.json")));
	std::optional<std::vector<std::string>> names = files::list(dir.utf8Path());
	CHECK(names && names->empty());
}
#endif

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
