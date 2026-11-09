#include "TestSupport.h"

#include "core/Json.h"
#include "core/SaveTarget.h"

#include <cerrno>
#include <filesystem>
#include <fstream>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

#ifdef _WIN32
#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <cstdlib>
#endif

// save の保存先の確かめ（docs/plugin/tools.md「図面を扱う道具」の save）。to の形の規則は mac と
// Windows の両方を、どの OS でも名指しして呼ぶ。ファイルシステムに聞く関数は、この OS
// の本物を呼ぶ。

namespace save_target = VwCli::core::save_target;
using save_target::Entry;
using save_target::Probe;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::ToolStatus;
using VwCli::test::must;
using VwCli::test::TempDir;
using VwCli::test::utf8;

namespace {

std::optional<std::string> mac(std::string_view to) {
	std::string problem;
	std::optional<std::string> parent = save_target::checkMacTo(to, problem);
	CHECK(parent.has_value() == problem.empty());
	return parent;
}

std::optional<std::string> windows(std::string_view to) {
	std::string problem;
	std::optional<std::string> parent = save_target::checkWindowsTo(to, problem);
	CHECK(parent.has_value() == problem.empty());
	return parent;
}

void macParent(std::string_view to, std::string_view parent) {
	std::optional<std::string> got = mac(to);
	CHECK_EQ(got.value_or("<refused>"), std::string(parent));
}

void windowsParent(std::string_view to, std::string_view parent) {
	std::optional<std::string> got = windows(to);
	CHECK_EQ(got.value_or("<refused>"), std::string(parent));
}

std::string repeat(std::string_view text, std::size_t times) {
	std::string out;
	for (std::size_t i = 0; i < times; ++i) {
		out += text;
	}
	return out;
}

}

TEST(MacToAcceptsAbsolutePaths) {
	macParent("/Users/me/tmp/a.vwx", "/Users/me/tmp");
	macParent("/Users/me/tmp/A.VWX", "/Users/me/tmp");
	macParent("/Users/me/図面 1.Vwx", "/Users/me");
	// ルートの直下の親はルート。
	macParent("/a.vwx", "/");
	// mac では \ も : もファイル名の文字。
	macParent("/x/a\\b:c.vwx", "/x");
	macParent("/x/" + repeat("a", 251) + ".vwx", "/x");
}

TEST(MacToRejectsBadForms) {
	for (std::string to : {
	         std::string(""),
	         std::string("a.vwx"),
	         std::string("./a.vwx"),
	         std::string("~/a.vwx"),
	         std::string("C:\\x\\a.vwx"),
	         std::string("//a.vwx"),
	         std::string("/x//a.vwx"),
	         std::string("/x/./a.vwx"),
	         std::string("/x/../a.vwx"),
	         std::string("/x/."),
	         std::string("/x/a.vwx/"),
	         std::string("/x/"),
	         std::string("/"),
	         std::string("/x/a.txt"),
	         std::string("/x/a.dwg"),
	         std::string("/x/a.vwx."),
	         std::string("/x/a.vw"),
	         std::string("/x/a\tb.vwx"),
	         std::string("/x/a\nb.vwx"),
	         std::string("/x/a\x1f.vwx"),
	         std::string("/x/a\0b.vwx", 10),
	         // 255 バイトを超える段と、1024 バイト以上（mac は UTF-8 のバイトで数える）。
	         "/x/" + repeat("a", 252) + ".vwx",
	         "/x/" + repeat("あ", 84) + ".vwx",
	     }) {
		CHECK(!mac(to));
	}
	// 全体は 1023 バイトまで受け、1024 バイト以上を断る（段はどれも 255 バイト以下）。
	std::string base = "/" + repeat(repeat("a", 200) + "/", 4);
	std::string fits = base + repeat("b", 1023 - base.size() - 4) + ".vwx";
	CHECK_EQ(fits.size(), std::size_t{1023});
	CHECK(mac(fits));
	CHECK(!mac(base + repeat("b", 1024 - base.size() - 4) + ".vwx"));
}

TEST(WindowsToAcceptsDrivePathsAndUnc) {
	windowsParent("C:\\Users\\me\\a.vwx", "C:\\Users\\me");
	windowsParent("d:\\x\\A.VWX", "d:\\x");
	// ルートの直下の親はルート（区切りを含む）。
	windowsParent("C:\\a.vwx", "C:\\");
	windowsParent("\\\\server\\share\\a.vwx", "\\\\server\\share\\");
	windowsParent("\\\\server\\share\\x\\a.vwx", "\\\\server\\share\\x");
	windowsParent("\\\\localhost\\共有\\図面.vwx", "\\\\localhost\\共有\\");
	// 予約された名前は断らない（SDK がふつうに保存した）。
	windowsParent("C:\\x\\CON.vwx", "C:\\x");
	// 段の途中の . と空白はよい（末尾だけを見る）。
	windowsParent("C:\\x.y\\a b\\a.vwx", "C:\\x.y\\a b");
	windowsParent("C:\\" + repeat("a", 251) + ".vwx", "C:\\");
}

TEST(WindowsToRejectsBadForms) {
	for (std::string to : {
	         std::string(""),
	         std::string("a.vwx"),
	         std::string("x\\a.vwx"),
	         // / を含む（直して渡さない）。
	         std::string("C:/x/a.vwx"),
	         std::string("C:\\x/a.vwx"),
	         std::string("//server/share/a.vwx"),
	         std::string("/a.vwx"),
	         // \\?\・\\.\・ドライブ相対・ルート相対。
	         std::string("\\\\?\\C:\\x\\a.vwx"),
	         std::string("\\\\?\\UNC\\server\\share\\a.vwx"),
	         std::string("\\\\.\\C:\\x\\a.vwx"),
	         std::string("C:a.vwx"),
	         std::string("\\a.vwx"),
	         std::string("1:\\a.vwx"),
	         // サーバーか共有かファイル名の無い UNC と、先頭の区切りが 3 つ以上。
	         std::string("\\\\server\\a.vwx"),
	         std::string("\\\\server\\share"),
	         std::string("\\\\server\\share\\"),
	         std::string("\\\\\\server\\share\\a.vwx"),
	         std::string("\\\\"),
	         // . と .. と空の段。
	         std::string("C:\\x\\.\\a.vwx"),
	         std::string("C:\\x\\..\\a.vwx"),
	         std::string("C:\\x\\\\a.vwx"),
	         std::string("C:\\"),
	         std::string("C:\\x\\"),
	         // 途中の段の末尾が . か空白（UNC のサーバーと共有の段も）。
	         std::string("C:\\x.\\a.vwx"),
	         std::string("C:\\x..\\a.vwx"),
	         std::string("C:\\x \\a.vwx"),
	         std::string("\\\\fs.example.\\share\\a.vwx"),
	         std::string("\\\\server \\share\\a.vwx"),
	         std::string("\\\\server\\share.\\a.vwx"),
	         // 拡張子と末尾の . か空白。
	         std::string("C:\\x\\a.txt"),
	         std::string("C:\\x\\a.vwx."),
	         std::string("C:\\x\\a.vwx "),
	         // 使えない文字（UNC のサーバーと共有の段も）。
	         std::string("C:\\x\\a<b.vwx"),
	         std::string("C:\\x\\a>b.vwx"),
	         std::string("C:\\x\\a:b.vwx"),
	         std::string("C:\\x\\a\"b.vwx"),
	         std::string("C:\\x\\a|b.vwx"),
	         std::string("C:\\x\\a?b.vwx"),
	         std::string("C:\\x\\a*b.vwx"),
	         std::string("\\\\ser:ver\\share\\a.vwx"),
	         std::string("\\\\server\\sh*re\\a.vwx"),
	         // 制御文字。
	         std::string("C:\\x\\a\tb.vwx"),
	         std::string("C:\\x\\a\0b.vwx", 12),
	         // 255 文字を超える段（UTF-16 の単位。サロゲートペアは 2 つ）。
	         "C:\\" + repeat("a", 252) + ".vwx",
	         "C:\\" + repeat("\xF0\x9F\x98\x80", 126) + ".vwx",
	     }) {
		CHECK(!windows(to));
	}
	// 段は 255 文字（UTF-16 の単位）まで受ける。日本語の 1 文字は UTF-8 で 3 バイトでも 1
	// つと数える。
	CHECK(windows("C:\\" + repeat("あ", 251) + ".vwx"));
	CHECK(windows("C:\\" + repeat("\xF0\x9F\x98\x80", 125) + "a.vwx"));
}

// 全体が 260 文字（UTF-16 の単位）以上なら断り、259 文字までは受ける。
TEST(WindowsToRejectsMaxPath) {
	std::string base = "C:\\" + repeat("abcdefghi\\", 24);
	std::string fits = base + repeat("a", 259 - base.size() - 4) + ".vwx";
	CHECK_EQ(fits.size(), std::size_t{259});
	CHECK(windows(fits));
	CHECK(!windows(base + repeat("a", 260 - base.size() - 4) + ".vwx"));
	// サロゲートペアは 2 つに数える（バイトでも文字でもない）。
	std::string surrogates =
	    base + repeat("\xF0\x9F\x98\x80", (259 - base.size() - 4) / 2) + ".vwx";
	CHECK(windows(surrogates));
	CHECK(!windows(surrogates.substr(0, surrogates.size() - 4) + "a.vwx"));
}

// プラグインが使う規則は OS のビルドで決まる。
TEST(NativeFormFollowsTheBuild) {
#ifdef _WIN32
	CHECK(save_target::kNativeForm == &save_target::checkWindowsTo);
#else
	CHECK(save_target::kNativeForm == &save_target::checkMacTo);
#endif
}

namespace {

// Recorder は差し替えの比べ方と聞く関数。呼ばれた順を覚える。
struct Recorder {
	std::vector<std::string> calls;
	Probe to{Entry::Missing, 0};
	Probe parent{Entry::Directory, 0};

	save_target::Checks checks() {
		return save_target::Checks{[this](std::string_view open, std::string_view target) {
			                           calls.push_back("same " + std::string(open));
			                           // 差し替えの比べ方: 大文字と小文字だけを畳む（ASCII）。
			                           if (open.size() != target.size()) {
				                           return false;
			                           }
			                           for (std::size_t i = 0; i < open.size(); ++i) {
				                           auto lower = [](char c) {
					                           return c >= 'A' && c <= 'Z'
					                                      ? static_cast<char>(c - 'A' + 'a')
														  : c;
				                           };
				                           if (lower(open[i]) != lower(target[i])) {
					                           return false;
				                           }
			                           }
			                           return true;
		                           },
		                           [this](const std::string &path) {
			                           calls.push_back("to " + path);
			                           return to;
		                           },
		                           [this](const std::string &path) {
			                           calls.push_back("parent " + path);
			                           return parent;
		                           }};
	}
};

std::string reasonOf(const ToolStatus &status) {
	const Json *reason = status.detail.find("reason");
	return reason && reason->isString() ? reason->asString() : "";
}

bool runCheck(Recorder &recorder, ToolStatus &status,
              std::vector<std::string> open = {"/u/plan.vwx", "/Applications/VW/名称未設定 1"}) {
	return save_target::check("/u/tmp/a.vwx", "/u/tmp", open, recorder.checks(), status);
}

}

TEST(SaveCheckPassesWhenTheTargetIsNew) {
	Recorder recorder;
	ToolStatus status;
	CHECK(runCheck(recorder, status));
	CHECK(status.code == ErrorCode::None);
	std::vector<std::string> expected = {"same /u/plan.vwx", "same /Applications/VW/名称未設定 1",
	                                     "to /u/tmp/a.vwx", "parent /u/tmp"};
	CHECK(recorder.calls == expected);
}

// 開いている図面と一致する to は、ファイルシステムに聞く前に to_is_open（親が無くても）。
TEST(SaveCheckFindsAnOpenDocumentFirst) {
	Recorder recorder;
	recorder.to = {Entry::Failed, 53};
	recorder.parent = {Entry::Missing, 0};
	ToolStatus status;
	CHECK(!runCheck(recorder, status, {"/U/TMP/A.vwx"}));
	CHECK(status.code == ErrorCode::Internal);
	CHECK_EQ(status.detail, must(R"({"reason":"to_is_open"})"));
	std::vector<std::string> expected = {"same /U/TMP/A.vwx"};
	CHECK(recorder.calls == expected);
}

TEST(SaveCheckRefusesAnExistingTarget) {
	for (Entry entry : {Entry::File, Entry::Directory}) {
		Recorder recorder;
		recorder.to = {entry, 0};
		ToolStatus status;
		CHECK(!runCheck(recorder, status));
		CHECK_EQ(reasonOf(status), std::string("file_exists"));
		// 親は聞かない。
		CHECK_EQ(recorder.calls.back(), std::string("to /u/tmp/a.vwx"));
	}
}

TEST(SaveCheckRefusesAMissingParent) {
	for (Entry entry : {Entry::File, Entry::Missing}) {
		Recorder recorder;
		recorder.parent = {entry, 0};
		ToolStatus status;
		CHECK(!runCheck(recorder, status));
		CHECK(status.code == ErrorCode::Internal);
		CHECK_EQ(status.detail, must(R"({"reason":"parent_missing"})"));
	}
}

// OS の呼び出しが「無い」以外の理由で失敗したら、その段で to_check_failed（番号を整数で載せる）。
TEST(SaveCheckReportsFailedChecks) {
	Recorder recorder;
	recorder.to = {Entry::Failed, 53};
	ToolStatus status;
	CHECK(!runCheck(recorder, status));
	CHECK_EQ(status.detail, must(R"({"reason":"to_check_failed","check":"to","os_error":53})"));
	CHECK_EQ(recorder.calls.back(), std::string("to /u/tmp/a.vwx"));

	Recorder parent;
	parent.parent = {Entry::Failed, 64};
	ToolStatus parentStatus;
	CHECK(!runCheck(parent, parentStatus));
	CHECK_EQ(parentStatus.detail,
	         must(R"({"reason":"to_check_failed","check":"parent","os_error":64})"));
}

// 共有が無い（67）は、Windows の「無い」の表で parent_missing になる（本物の 67 は実機で見る）。
TEST(SaveCheckTreatsAMissingShareAsMissing) {
#ifdef _WIN32
	CHECK(save_target::isMissing(67));
	CHECK(save_target::isMissing(2));
	CHECK(save_target::isMissing(3));
	CHECK(save_target::isMissing(267));
	CHECK(!save_target::isMissing(53));
	CHECK(!save_target::isMissing(64));
	CHECK(!save_target::isMissing(5));
	CHECK(!save_target::isMissing(32));
	Recorder recorder;
	recorder.parent = {save_target::isMissing(67) ? Entry::Missing : Entry::Failed, 67};
	ToolStatus status;
	CHECK(!runCheck(recorder, status));
	CHECK_EQ(reasonOf(status), std::string("parent_missing"));
#else
	CHECK(save_target::isMissing(ENOENT));
	CHECK(save_target::isMissing(ENOTDIR));
	CHECK(!save_target::isMissing(EACCES));
	CHECK(!save_target::isMissing(ENAMETOOLONG));
	CHECK(!save_target::isMissing(EIO));
#endif
}

namespace {

std::string native(const TempDir &dir, std::string_view name) { return dir.sub(std::string(name)); }

void touch(const std::filesystem::path &path) { std::ofstream(path) << "x"; }

}

// この OS
// の本物の呼び出しで、ファイルが無い・親のフォルダが無い・途中の段がファイルを「無い」と読む。
TEST(ProbesReadTheFileSystem) {
	TempDir dir;
	std::filesystem::create_directories(dir.path() / "folder");
	touch(dir.path() / "file.vwx");
	CHECK(save_target::probeTo(native(dir, "file.vwx")).entry == Entry::File);
	CHECK(save_target::probeTo(native(dir, "folder")).entry == Entry::Directory);
	CHECK(save_target::probeTo(native(dir, "none.vwx")).entry == Entry::Missing);
	CHECK(save_target::probeParent(native(dir, "folder")).entry == Entry::Directory);
	CHECK(save_target::probeParent(native(dir, "file.vwx")).entry == Entry::File);
	CHECK(save_target::probeParent(dir.utf8Path()).entry == Entry::Directory);
	// 親のフォルダが無い。
	std::string deep = utf8(dir.path() / "none" / "a.vwx");
	CHECK(save_target::probeTo(deep).entry == Entry::Missing);
	CHECK(save_target::probeParent(native(dir, "none")).entry == Entry::Missing);
	// 途中の段がファイル（Windows は 3 か 267、POSIX は ENOTDIR）。
	std::string through = utf8(dir.path() / "file.vwx" / "a.vwx");
	Probe probe = save_target::probeTo(through);
	CHECK(probe.entry == Entry::Missing);
	CHECK(save_target::probeParent(utf8(dir.path() / "file.vwx" / "x")).entry == Entry::Missing);
	// ルートはフォルダ。
#ifdef _WIN32
	std::string root = dir.utf8Path().substr(0, 3);
	CHECK_EQ(root.substr(1), std::string(":\\"));
#else
	std::string root = "/";
#endif
	CHECK(save_target::probeParent(root).entry == Entry::Directory);
}

// 壊れたリンクの to は既にあるとし、先の無いリンクの親は「無い」とする（OS で揃う）。
TEST(ProbesDoNotFollowTheTargetButFollowTheParent) {
	TempDir dir;
	std::filesystem::create_directories(dir.path() / "target");
	std::error_code error;
#ifdef _WIN32
	// ジャンクションは特権なしで作れる（シンボリックリンクは特権か開発者モードが要る）。
	std::string command = "mklink /J \"" + utf8(dir.path() / "link") + "\" \"" +
	                      utf8(dir.path() / "target") + "\" >NUL";
	REQUIRE(std::system(command.c_str()) == 0);
#else
	std::filesystem::create_directory_symlink(dir.path() / "target", dir.path() / "link", error);
	REQUIRE(!error);
	std::filesystem::create_symlink(dir.path() / "nowhere.vwx", dir.path() / "broken.vwx", error);
	REQUIRE(!error);
	CHECK(save_target::probeTo(native(dir, "broken.vwx")).entry == Entry::File);
#endif
	CHECK(save_target::probeParent(native(dir, "link")).entry == Entry::Directory);
	std::filesystem::remove(dir.path() / "target", error);
	REQUIRE(!error);
	CHECK(save_target::probeTo(native(dir, "link")).entry != Entry::Missing);
	CHECK(save_target::probeParent(native(dir, "link")).entry == Entry::Missing);
	ToolStatus status;
	CHECK(!save_target::check(utf8(dir.path() / "link"), dir.utf8Path(), {},
	                          {[](std::string_view, std::string_view) { return false; },
	                           save_target::probeTo, save_target::probeParent},
	                          status));
	CHECK_EQ(reasonOf(status), std::string("file_exists"));
	ToolStatus parentStatus;
	CHECK(!save_target::check(utf8(dir.path() / "link" / "a.vwx"), native(dir, "link"), {},
	                          {[](std::string_view, std::string_view) { return false; },
	                           save_target::probeTo, save_target::probeParent},
	                          parentStatus));
	CHECK_EQ(reasonOf(parentStatus), std::string("parent_missing"));
}

#ifdef _WIN32
// CON.vwx は装置ではなくファイルの名前として聞く（\\?\ を付けるので「無い」）。
TEST(ProbesAskReservedNamesAsFiles) {
	TempDir dir;
	CHECK(save_target::probeTo(native(dir, "CON.vwx")).entry == Entry::Missing);
	CHECK(save_target::probeTo(native(dir, "NUL.vwx")).entry == Entry::Missing);
}

// ほかのプロセスが共有を絞って開いている親でも、共有違反（32）・拒否（5）にならない。
TEST(ProbesSeeAParentThatIsHeldWithoutSharing) {
	TempDir dir;
	std::filesystem::create_directories(dir.path() / "held");
	std::wstring held = (dir.path() / "held").wstring();
	HANDLE handle = ::CreateFileW(held.c_str(), GENERIC_READ, 0, nullptr, OPEN_EXISTING,
	                              FILE_FLAG_BACKUP_SEMANTICS, nullptr);
	REQUIRE(handle != INVALID_HANDLE_VALUE);
	Probe probe = save_target::probeParent(native(dir, "held"));
	CHECK(probe.entry == Entry::Directory);
	CHECK_EQ(probe.osError, std::int64_t{0});
	CHECK(save_target::probeTo(utf8(dir.path() / "held" / "a.vwx")).entry == Entry::Missing);
	::CloseHandle(handle);
}
#endif
