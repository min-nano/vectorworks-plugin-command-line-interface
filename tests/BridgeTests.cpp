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

// inCi は CI の環境変数が存在するか（MSVC は std::getenv を非推奨とし、警告をエラーにするため
// _dupenv_s を使用する）。
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
// setForeignReparsePoint は path（既存のファイル）に、名前の代替にならない独自の reparse point
// を付ける（OneDrive などのクラウドのファイルに相当するものを、テストで作成するため）。付けられ
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
	// Microsoft のものでも名前の代替でもない tag（上位のビットを立てない）。
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

// join は区切りを 1 つのみ追加する（dir が区切りで終わっていれば追加せず、空なら name のみ）。
// Windows は \ と / のいずれも区切りとみなし、追加するのは \。
TEST(FilesJoinAddsOneSeparator) {
	CHECK(files::isSeparator('/'));
	CHECK(!files::isSeparator('a'));
	CHECK_EQ(files::join("", "b"), std::string("b"));
	CHECK_EQ(files::join("/a/", "b"), std::string("/a/b"));
#ifdef _WIN32
	CHECK(files::isSeparator('\\'));
	CHECK_EQ(files::join("C:\\a", "b"), std::string("C:\\a\\b"));
	CHECK_EQ(files::join("C:\\a\\", "b"), std::string("C:\\a\\b"));
#else
	CHECK(!files::isSeparator('\\'));
	CHECK_EQ(files::join("/a", "b"), std::string("/a/b"));
	CHECK_EQ(files::join("/a\\", "b"), std::string("/a\\/b"));
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
	CHECK_EQ(info.st_mode & 0777u, 0700u);
	CHECK(::stat(dir.sub("a").c_str(), &info) == 0);
	CHECK_EQ(info.st_mode & 0777u, 0700u);
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

// 実行中にスプールが削除されても（利用者が削除したなど）、整理と巡回は何もせずに終了する
// （スプールを再作成しない。作成するのは prepare のみ）。
TEST(BridgeSweepAndPollTolerateAMissingSpool) {
	TempDir dir;
	Bridge bridge(dir.sub("missing"));
	bridge.sweep();
	CHECK(bridge.poll().empty());
	CHECK(!exists(dir, "missing"));
}

TEST(BridgePollReturnsValidIdsInFileNameOrder) {
	// 大文字と小文字のみが異なる名前は、macOS と Windows では同一のファイルになるため使用しない。
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
	// 消失していた要求は飛ばす。
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
	// 同じ id の応答があれば置換する。
	CHECK(bridge.reply("r", R"({"ok":false})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "r.res.json"), std::string(R"({"ok":false})"));
}

// 応答の名前にディレクトリがあると差し替えられない。失敗を返し、書き込み途中のものを残さない。
TEST(BridgeReplyFailsWithoutLeavingTempWhenItCannotRename) {
	TempDir dir;
	Bridge bridge(dir.utf8Path());
	std::filesystem::create_directory(dir.path() / "r.res.json");
	put(dir, "r.res.json/inside");
	// 書き込み途中の名前には書き込める（失敗するのは書き込みではなく差し替え）。
	put(dir, "r.res.json.tmp", "old");
	CHECK(!bridge.reply("r", R"({"ok":true})"));
	CHECK(!exists(dir, "r.res.json.tmp"));
	CHECK(std::filesystem::is_directory(dir.path() / "r.res.json"));
}

#ifndef _WIN32
// FIFO などの通常のファイルでないものを配置されても、開いて待機しない（待機するとメインスレッドが
// 停止する）。待機すればこのテストが終了しない。
TEST(BridgeDoesNotWaitOnFifos) {
	TempDir dir;
	for (const char *name : {"f.req.json", "f.wait", "g.res.json.tmp"}) {
		REQUIRE(::mkfifo(dir.sub(name).c_str(), 0600) == 0);
	}
	Bridge bridge(dir.utf8Path());
	// 読み取れない要求として飛ばし、削除しない。
	CHECK(!bridge.take("f"));
	CHECK(exists(dir, "f.req.json"));
	// 通常のファイルでない待機標識は存在しないものとする（実行しない）。
	CHECK(bridge.waitState("f") == files::WaitState::Missing);
	// 書き込み途中の FIFO は削除して再作成するため、応答を書き込める。
	CHECK(bridge.reply("g", R"({"ok":true})"));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "g.res.json"), std::string(R"({"ok":true})"));
}

// bridge.lock が通常のファイルでなければ、開いて待機せずに、削除して再作成してから取得する。
// 再作成するのは 1 つの Lock につき 1 度のみで、2 度目は拒否した理由を返す（受付処理の度に
// 繰り返さない）。
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

// 書き込み途中のシンボリックリンクはたどらない（たどると、リンクの先を切り詰めてしまう）。
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

// スプールのファイルがシンボリックリンクであれば、たどらずに拒否する（リンクの先がデバイスや
// 名前付きパイプの場合、開くのみで副作用が生じる・停止するため）。
TEST(BridgeDoesNotFollowSymlinks) {
	TempDir dir;
	put(dir, "target", R"({"tool":"ping"})");
	std::error_code linked;
	std::filesystem::create_symlink(dir.path() / "target", dir.path() / "s.req.json", linked);
	// Windows でシンボリックリンクを作成する権限が無ければ確認できない。暗黙に通過しないよう、
	// 飛ばしたことを出力に残し、CI（runner は作成できる）では失敗とする。
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
	// bridge.lock のリンクは、たどらずに名前を削除して再作成する（リンクの先は残る）。
	std::string error;
	CHECK(bridge.lock(error) == files::Lock::Result::Taken);
	CHECK(std::filesystem::is_regular_file(
	    std::filesystem::symlink_status(dir.path() / "bridge.lock")));
	CHECK_EQ(VwCli::test::readFile(dir.path() / "target"), std::string(R"({"tool":"ping"})"));
}

// 書き込み途中のものが hard link でも、リンクの先（スプールの外のファイル）は変更しない。
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
	// 存在しない待機標識は、開けてしまう NUL デバイスではなく、存在しないと判別される。
	CHECK(bridge.waitState("NUL") == files::WaitState::Missing);
	put(dir, "CON.wait");
	CHECK(bridge.waitState("CON") == files::WaitState::Released);
	bridge.removeWait("CON");
	CHECK(bridge.reply("AUX", R"({"ok":true})"));
	std::optional<std::string> response = files::read(dir.sub("AUX.res.json"), 1024);
	CHECK(response && *response == R"({"ok":true})");
	// 拡張子の無い名前は、Windows 11 でもデバイスを指す（\\?\ を付けなければ、NUL へ書き込んでも
	// 破棄され、読み取っても空になる）。
	CHECK(files::write(dir.sub("NUL"), "file"));
	std::optional<std::string> bare = files::read(dir.sub("NUL"), 1024);
	CHECK(bare && *bare == "file");
	CHECK(files::probe(dir.sub("NUL")) == files::WaitState::Released);
	// 一時ディレクトリを削除する std::filesystem が予約された名前を削除できるとは限らないため、
	// ここで削除する。
	CHECK(files::remove(dir.sub("AUX.res.json")));
	CHECK(files::remove(dir.sub("NUL")));
	std::optional<std::vector<std::string>> names = files::list(dir.utf8Path());
	CHECK(names && names->empty());
}

// リンクでない reparse point（OneDrive などのクラウドのファイルに相当する）も、開き直さずに
// 拒否する。bridge.lock のみは、1 度のみ削除して再作成する。
TEST(BridgeRejectsReparsePointsThatAreNotLinks) {
	TempDir dir;
	std::vector<std::string> made;
	// 作成したものは、テストの終了時に削除する（一時ディレクトリを削除する std::filesystem が
	// 未知の tag の reparse point を削除できるとは限らない。DeleteFileW は reparse point
	// そのものを削除する）。
	auto cleanUp = [&] {
		for (const std::string &name : made) {
			CHECK(files::remove(dir.sub(name)));
		}
	};
	for (const char *name : {"r.req.json", "r.wait", "bridge.lock"}) {
		put(dir, name, R"({"tool":"ping"})");
		if (DWORD failure = setForeignReparsePoint(dir.path() / name); failure != 0) {
			cleanUp();
			// 作成できなければ確認できない。暗黙に通過しないよう、CI では失敗とする。
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
	// 要求と待機標識は、開き直さずに拒否するため、reparse point のまま残る（取り出していない）。
	for (const char *name : {"r.req.json", "r.wait"}) {
		DWORD attributes = ::GetFileAttributesW((dir.path() / name).c_str());
		CHECK(attributes != INVALID_FILE_ATTRIBUTES &&
		      (attributes & FILE_ATTRIBUTE_REPARSE_POINT) != 0);
	}
	// bridge.lock は削除して再作成してから取得する。
	std::string lockPath = dir.sub("bridge.lock");
	files::Lock lock;
	std::string error;
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Taken);
	DWORD attributes = ::GetFileAttributesW((dir.path() / "bridge.lock").c_str());
	CHECK(attributes != INVALID_FILE_ATTRIBUTES &&
	      (attributes & FILE_ATTRIBUTE_REPARSE_POINT) == 0);
	// 2 度目は再作成せず、reparse point を「通常のファイルでない」として拒否したことを返す
	// （開き直して開けなかったのであれば、CreateFile の error の番号になる）。
	lock.release();
	REQUIRE(setForeignReparsePoint(dir.path() / "bridge.lock") == 0);
	CHECK(lock.take(lockPath, error) == files::Lock::Result::Failed);
	CHECK(error.find("通常のファイルではありません") != std::string::npos);
	cleanUp();
}

// UNC のパス（\\server\share\…）は \\?\UNC\ を付けて開き、
// share の後ろから区切りごとに作成する。管理用の共有 \\localhost\C$ など
// （一時ディレクトリのドライブ）を経由して、一時ディレクトリの下に作成する。
// 共有を使用できなければ飛ばしたことを出力に残し、CI（runner は使用できる）では
// 失敗とする。
TEST(FilesMakeDirectoriesOnUncPathOnWindows) {
	TempDir dir;
	std::wstring local = dir.path().wstring();
	REQUIRE(local.size() > 3 && local[1] == L':' && local[2] == L'\\');
	std::filesystem::path unc =
	    L"\\\\localhost\\" + std::wstring(1, local[0]) + L"$" + local.substr(2);
	std::error_code ec;
	if (!std::filesystem::is_directory(unc, ec)) {
		REQUIRE(!inCi());
		std::fprintf(stderr, "skip FilesMakeDirectoriesOnUncPathOnWindows: %s\n",
		             VwCli::test::utf8(unc).c_str());
		return;
	}
	std::string error;
	CHECK(files::makeDirectories(VwCli::test::utf8(unc / "a" / "b"), error));
	CHECK(std::filesystem::is_directory(dir.path() / "a" / "b"));
	// 既にあれば何もしない。
	CHECK(files::makeDirectories(VwCli::test::utf8(unc / "a" / "b"), error));
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

// mac のプラグインはバンドルの中の実行ファイルであるため、バンドルのあるフォルダ（<Plug-Ins>/cli）
// の下に配置する。
TEST(BridgeSpoolDirOfBundleIsNextToTheBundle) {
	using VwCli::core::spoolDirOf;
	std::string plugIns = "/Users/a/Library/Application Support/Vectorworks/2026/Plug-Ins";
	CHECK_EQ(spoolDirOf(plugIns + "/cli/cli.vwlibrary/Contents/MacOS/cli").value_or(""),
	         files::join(plugIns + "/cli", "spool"));
	CHECK_EQ(spoolDirOf("/x.vwlibrary/cli/cli.vwlibrary/Contents/MacOS/cli").value_or(""),
	         files::join("/x.vwlibrary/cli", "spool"));
	CHECK_EQ(spoolDirOf("/cli.vwlibrary/Contents/MacOS/cli").value_or(""), std::string("/spool"));
	// <X>.vwlibrary/Contents/MacOS/<実行ファイル> の形の場合のみ上位へ移る。パスの途中に .vwlibrary
	// で終わるフォルダがあっても上位へ移らない（呼び出し元は bin の親を取得するため、上位へ移ると
	// 不一致が生じる）。
	CHECK_EQ(spoolDirOf("/x.vwlibrary/Plug-ins/cli/cli.vlb").value_or(""),
	         files::join("/x.vwlibrary/Plug-ins/cli", "spool"));
	CHECK_EQ(spoolDirOf("/a/cli.vwlibrary/MacOS/cli").value_or(""),
	         files::join("/a/cli.vwlibrary/MacOS", "spool"));
	CHECK_EQ(spoolDirOf("/a/cli.vwlibrary/Contents/cli").value_or(""),
	         files::join("/a/cli.vwlibrary/Contents", "spool"));
	CHECK_EQ(spoolDirOf("/a/cli/Contents/MacOS/cli").value_or(""),
	         files::join("/a/cli/Contents/MacOS", "spool"));
	// モジュールそのものの名前は参照しない。
	CHECK_EQ(spoolDirOf("/a/cli/cli.vwlibrary").value_or(""), files::join("/a/cli", "spool"));
}

// Windows のプラグイン（cli.vlb）とテストの実行ファイルは、モジュールのあるフォルダの下に配置する。
TEST(BridgeSpoolDirOfModuleIsNextToTheModule) {
	using VwCli::core::spoolDirOf;
	CHECK_EQ(spoolDirOf("/a/b/VwCliTests").value_or(""), files::join("/a/b", "spool"));
	CHECK_EQ(spoolDirOf("/VwCliTests").value_or(""), std::string("/spool"));
	CHECK(!spoolDirOf("VwCliTests"));
	CHECK(!spoolDirOf(""));
#ifdef _WIN32
	std::string plugIns = "C:\\Users\\a\\AppData\\Roaming\\Nemetschek\\Vectorworks\\2026\\Plug-ins";
	CHECK_EQ(spoolDirOf(plugIns + "\\cli\\cli.vlb").value_or(""), plugIns + "\\cli\\spool");
	CHECK_EQ(spoolDirOf("\\\\?\\C:\\cli\\cli.vlb").value_or(""),
	         std::string("\\\\?\\C:\\cli\\spool"));
	CHECK_EQ(spoolDirOf("C:\\cli.vlb").value_or(""), std::string("C:\\spool"));
#else
	// POSIX では \ は名前の一部。
	CHECK_EQ(spoolDirOf("/a/b\\c").value_or(""), std::string("/a/spool"));
#endif
}

// いずれのモジュールの中にも無い番地（スタック）には、モジュールのパスが存在しない。
TEST(FilesModulePathOfAddressOutsideModules) {
	int local = 0;
	CHECK(!files::modulePathOf(&local));
}

#ifndef _WIN32
// 相対パスは現在の作業ディレクトリの下とする（Linux の実行ファイルでは dladdr が argv[0] を返す）。
TEST(FilesAbsoluteResolvesAgainstTheWorkingDirectory) {
	CHECK_EQ(files::absolute("/a/b").value_or(""), std::string("/a/b"));
	CHECK(!files::absolute(""));
	std::string cwd = VwCli::test::utf8(std::filesystem::current_path());
	CHECK_EQ(files::absolute("build/VwCliTests").value_or(""),
	         files::join(cwd, "build/VwCliTests"));
}
#endif

// テストでは、このコードを含むモジュールはテストの実行ファイルであるため、そのフォルダの下になる。
TEST(BridgeDefaultSpoolDirIsNextToTheModule) {
	static const char anchor = 0;
	std::optional<std::string> module = files::modulePathOf(&anchor);
	REQUIRE(module);
	std::filesystem::path modulePath = std::filesystem::path(
	    std::u8string(reinterpret_cast<const char8_t *>(module->data()), module->size()));
	CHECK(modulePath.is_absolute());
	CHECK(std::filesystem::is_regular_file(modulePath));
	CHECK_EQ(VwCli::test::utf8(modulePath.stem()), std::string("VwCliTests"));

	std::optional<std::string> dir = VwCli::core::defaultSpoolDir();
	REQUIRE(dir);
	std::filesystem::path path = std::filesystem::path(
	    std::u8string(reinterpret_cast<const char8_t *>(dir->data()), dir->size()));
	CHECK_EQ(VwCli::test::utf8(path.filename()), std::string("spool"));
	CHECK(path.parent_path() == modulePath.parent_path());
}
