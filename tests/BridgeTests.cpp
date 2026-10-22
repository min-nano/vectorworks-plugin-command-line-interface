#include "TestSupport.h"

#include "core/Bridge.h"
#include "core/Files.h"
#include "core/Protocol.h"

#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <string>
#include <vector>

#ifndef _WIN32
#include <sys/stat.h>
#include <unistd.h>
#else
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <winioctl.h>
#endif

using VwCli::core::Bridge;
namespace files = VwCli::core::files;
using VwCli::test::TempDir;

namespace {

void put(const TempDir &dir, const std::string &name, const std::string &text = "") {
	CHECK(files::write(dir.sub(name), text));
}

// inCi は CI の環境変数があるか（MSVC は std::getenv を非推奨とし、警告をエラーにするので
// _dupenv_s を使う）。
bool inCi() {
#ifdef _WIN32
	char *value = nullptr;
	std::size_t size = 0;
	bool set = _dupenv_s(&value, &size, "CI") == 0 && value != nullptr;
	std::free(value);
	return set;
#else
	return std::getenv("CI") != nullptr;
#endif
}

bool exists(const TempDir &dir, const std::string &name) {
	return std::filesystem::exists(dir.path() / name);
}

#ifdef _WIN32
// setForeignReparsePoint は path（既にあるファイル）に、名前の代わりにならない独自の reparse
// point を付ける（OneDrive などのクラウドのファイルにあたるものを、テストで作るため）。付けられ
// なければ、その理由（0 なら付けた）。
DWORD setForeignReparsePoint(const std::filesystem::path &path) {
	HANDLE file = ::CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, OPEN_EXISTING,
	                            FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
	if (file == INVALID_HANDLE_VALUE) {
		return ::GetLastError();
	}
	constexpr DWORD kData = 4;
	std::vector<BYTE> buffer(REPARSE_GUID_DATA_BUFFER_HEADER_SIZE + kData);
	auto *reparse = reinterpret_cast<REPARSE_GUID_DATA_BUFFER *>(buffer.data());
	// Microsoft のものでも名前の代わりでもない tag（上位のビットを立てない）。
	reparse->ReparseTag = 0x00001234;
	reparse->ReparseDataLength = kData;
	reparse->ReparseGuid =
	    GUID{0x6f1d3c2a, 0x8b4e, 0x4f7a, {0x9c, 0x1d, 0x2e, 0x3f, 0x4a, 0x5b, 0x6c, 0x7d}};
	DWORD returned = 0;
	DWORD failure = 0;
	if (!::DeviceIoControl(file, FSCTL_SET_REPARSE_POINT, buffer.data(),
	                       static_cast<DWORD>(buffer.size()), nullptr, 0, &returned, nullptr)) {
		failure = ::GetLastError();
	}
	::CloseHandle(file);
	return failure;
}
#endif

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

// 動いている間にスプールが消されても（利用者が消したなど）、掃除と見回りは何もせずに終わる
// （スプールを作り直さない。作るのは prepare だけ）。
TEST(BridgeSweepAndPollTolerateAMissingSpool) {
	TempDir dir;
	Bridge bridge(dir.sub("missing"));
	bridge.sweep();
	CHECK(bridge.poll().empty());
	CHECK(!exists(dir, "missing"));
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

// 応答の名前にディレクトリがあると付け替えられない。失敗を返し、書きかけを残さない。
TEST(BridgeReplyFailsWithoutLeavingTempWhenItCannotRename) {
	TempDir dir;
	Bridge bridge(dir.utf8Path());
	std::filesystem::create_directory(dir.path() / "r.res.json");
	put(dir, "r.res.json/inside");
	// 書きかけの名前には書ける（失敗するのは書き込みではなく付け替え）。
	put(dir, "r.res.json.tmp", "old");
	CHECK(!bridge.reply("r", R"({"ok":true})"));
	CHECK(!exists(dir, "r.res.json.tmp"));
	CHECK(std::filesystem::is_directory(dir.path() / "r.res.json"));
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
	// 書きかけの FIFO は消して作り直すので、応答を書ける。
	CHECK(bridge.reply("g", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "g.res.json"), std::string(R"({"ok":true})"));
}

// bridge.lock が通常のファイルでなければ、開いて待たずに、消して作り直してから掴む。作り直すのは
// 1 つの Lock につき 1 度だけで、2 度目は断った理由を返す（受け付けのたびに繰り返さない）。
TEST(BridgeLockRecreatesAFifoOnce) {
	TempDir dir;
	std::string lockPath = dir.sub("bridge.lock");
	REQUIRE(::mkfifo(lockPath.c_str(), 0600) == 0);
	files::Lock lock;
	std::string error;
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Taken);
	CHECK(std::filesystem::is_regular_file(std::filesystem::symlink_status(lockPath)));
	lock.release();
	REQUIRE(::unlink(lockPath.c_str()) == 0);
	REQUIRE(::mkfifo(lockPath.c_str(), 0600) == 0);
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Failed);
	CHECK(error.find("通常のファイルではありません") != std::string::npos);
	CHECK(!lock.held());
}

// 書きかけのシンボリックリンクはたどらない（たどると、リンクの先を切り詰めてしまう）。
TEST(BridgeReplyDoesNotFollowSymlinks) {
	TempDir dir;
	put(dir, "target", "keep");
	REQUIRE(::symlink(dir.sub("target").c_str(), dir.sub("s.res.json.tmp").c_str()) == 0);
	Bridge bridge(dir.utf8Path());
	CHECK(bridge.reply("s", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "target"), std::string("keep"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "s.res.json"), std::string(R"({"ok":true})"));
}
#endif

// スプールのファイルがシンボリックリンクなら、たどらずに断る（リンクの先がデバイスや名前付き
// パイプだと、開くだけで副作用が起きる・止まるため）。
TEST(BridgeDoesNotFollowSymlinks) {
	TempDir dir;
	put(dir, "target", R"({"tool":"ping"})");
	std::error_code linked;
	std::filesystem::create_symlink(dir.path() / "target", dir.path() / "s.req.json", linked);
	// Windows でシンボリックリンクを作る権限が無ければ確かめようがない。黙って通らないよう、
	// 飛ばしたことを出力に残し、CI（runner は作れる）では失敗にする。
	if (linked) {
		REQUIRE(!inCi());
		std::fprintf(stderr, "skip BridgeDoesNotFollowSymlinks: %s\n", linked.message().c_str());
		return;
	}
	std::filesystem::create_symlink(dir.path() / "target", dir.path() / "s.wait", linked);
	REQUIRE(!linked);
	std::filesystem::create_symlink(dir.path() / "target", dir.path() / "bridge.lock", linked);
	REQUIRE(!linked);
	Bridge bridge(dir.utf8Path());
	CHECK(!bridge.take("s"));
	CHECK(bridge.waitState("s") == files::WaitState::Missing);
	// bridge.lock のリンクは、たどらずに名前を消して作り直す（リンクの先は残る）。
	std::string error;
	CHECK(bridge.lock(error) == files::Lock::Result::Taken);
	CHECK(std::filesystem::is_regular_file(
	    std::filesystem::symlink_status(dir.path() / "bridge.lock")));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "target"), std::string(R"({"tool":"ping"})"));
}

// 書きかけが hard link でも、リンクの先（スプールの外のファイル）は変えない。
TEST(BridgeReplyDoesNotWriteThroughHardLinks) {
	TempDir dir;
	put(dir, "target", "keep");
	std::error_code linked;
	std::filesystem::create_hard_link(dir.path() / "target", dir.path() / "h.res.json.tmp", linked);
	REQUIRE(!linked);
	Bridge bridge(dir.utf8Path());
	CHECK(bridge.reply("h", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "target"), std::string("keep"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "h.res.json"), std::string(R"({"ok":true})"));
}

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
	// 拡張子の無い名前は、Windows 11 でもデバイスを指す（\\?\ を付けなければ、NUL へ書いても
	// 捨てられ、読んでも空になる）。
	CHECK(files::write(dir.sub("NUL"), "file"));
	std::optional<std::string> bare = files::read(dir.sub("NUL"), 1024);
	CHECK(bare && *bare == "file");
	CHECK(files::probe(dir.sub("NUL")) == files::WaitState::Released);
	// 一時ディレクトリを消す std::filesystem が予約された名前を消せるとは限らないので、ここで消す。
	CHECK(files::remove(dir.sub("AUX.res.json")));
	CHECK(files::remove(dir.sub("NUL")));
	std::optional<std::vector<std::string>> names = files::list(dir.utf8Path());
	CHECK(names && names->empty());
}

// リンクでない reparse point（OneDrive などのクラウドのファイルにあたる）も、開き直さずに断る。
// bridge.lock だけは、1 度だけ消して作り直す。
TEST(BridgeRejectsReparsePointsThatAreNotLinks) {
	TempDir dir;
	std::vector<std::string> made;
	// 作ったものは、テストの終わりに消す（一時ディレクトリを消す std::filesystem が未知の tag の
	// reparse point を消せるとは限らない。DeleteFileW は reparse point そのものを消す）。
	auto cleanUp = [&] {
		for (const std::string &name : made) {
			CHECK(files::remove(dir.sub(name)));
		}
	};
	for (const char *name : {"r.req.json", "r.wait", "bridge.lock"}) {
		put(dir, name, R"({"tool":"ping"})");
		if (DWORD failure = setForeignReparsePoint(dir.path() / name); failure != 0) {
			cleanUp();
			// 作れなければ確かめようがない。黙って通らないよう、CI では失敗にする。
			REQUIRE(!inCi());
			std::fprintf(stderr, "skip BridgeRejectsReparsePointsThatAreNotLinks: error %lu\n",
			             failure);
			return;
		}
		made.emplace_back(name);
	}
	Bridge bridge(dir.utf8Path());
	CHECK(!bridge.take("r"));
	CHECK(bridge.waitState("r") == files::WaitState::Missing);
	// 要求と待つ印は、開き直さずに断るので、reparse point のまま残る（取り出していない）。
	for (const char *name : {"r.req.json", "r.wait"}) {
		DWORD attributes = ::GetFileAttributesW((dir.path() / name).c_str());
		CHECK(attributes != INVALID_FILE_ATTRIBUTES &&
		      (attributes & FILE_ATTRIBUTE_REPARSE_POINT) != 0);
	}
	// bridge.lock は消して作り直してから掴む。
	std::string lockPath = dir.sub("bridge.lock");
	files::Lock lock;
	std::string error;
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Taken);
	DWORD attributes = ::GetFileAttributesW((dir.path() / "bridge.lock").c_str());
	CHECK(attributes != INVALID_FILE_ATTRIBUTES &&
	      (attributes & FILE_ATTRIBUTE_REPARSE_POINT) == 0);
	// 2 度目は作り直さず、reparse point を「通常のファイルでない」として断ったことを返す
	// （開き直して開けなかったのなら、CreateFile の error の番号になる）。
	lock.release();
	REQUIRE(setForeignReparsePoint(dir.path() / "bridge.lock") == 0);
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Failed);
	CHECK(error.find("通常のファイルではありません") != std::string::npos);
	cleanUp();
}

// 絶対パスでないパスは開かない（\\?\ を付けられず、予約された名前がデバイスを指すため）。
TEST(FilesRejectRelativePathsOnWindows) {
	CHECK(!files::read("NUL", 1024));
	CHECK(!files::read("\\\\.\\NUL", 1024));
	CHECK(files::probe("NUL") == files::WaitState::Missing);
	std::string error;
	CHECK(!files::makeDirectories("relative-spool", error));
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
