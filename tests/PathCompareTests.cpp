#include "TestSupport.h"

// save の to の比べ方の包み（docs/plugin/tools.md「パスの一致の判定」）。包みは mac と Windows
// でだけ ビルドし、CMake がそのとき VWCLI_HAS_PATH_COMPARE を定義する。このファイルはどの OS
// でもテストの ソースに並べ、Linux では空になる。

#if (defined(__APPLE__) || defined(_WIN32)) && !defined(VWCLI_HAS_PATH_COMPARE)
#error "the path comparison wrapper must be built on macOS and Windows (CMakeLists.txt)"
#endif

#ifdef VWCLI_HAS_PATH_COMPARE

#include "core/PathCompare.h"

#include <string>

using VwCli::core::path_compare::equivalent;

// NFC と NFD だけが違うパスは一致する（macOS のファイル名は NFD で返ることがある）。
TEST(PathCompareMatchesNfcAndNfd) {
	// é（U+00E9）と e＋結合アクセント（U+0065 U+0301）、が（U+304C）と か＋濁点（U+304B U+3099）。
	CHECK(equivalent("/Users/me/caf\xC3\xA9.vwx", "/Users/me/cafe\xCC\x81.vwx"));
	CHECK(equivalent("/Users/me/\xE3\x81\x8C.vwx", "/Users/me/\xE3\x81\x8B\xE3\x82\x99.vwx"));
	CHECK(equivalent("C:\\x\\caf\xC3\xA9.vwx", "C:\\x\\cafe\xCC\x81.vwx"));
}

// 大文字と小文字だけが違うパスは一致する。
TEST(PathCompareIgnoresCase) {
	CHECK(equivalent("/Users/me/Plan.vwx", "/users/ME/plan.VWX"));
	CHECK(equivalent("C:\\Users\\Me\\A.vwx", "c:\\users\\me\\a.VWX"));
	CHECK(equivalent("/x/\xC3\x89t\xC3\xA9.vwx", "/x/\xC3\xA9T\xC3\x89.vwx"));
	CHECK(equivalent("/x/CAF\xC3\x89.vwx", "/x/cafe\xCC\x81.vwx"));
	CHECK(equivalent("", ""));
}

// NFC で長くなる文字（U+0958 は U+0915 U+093C になる）も正規化して比べる（Windows は足りない
// 領域を広げてやり直す）。
TEST(PathCompareNormalizesCharactersThatGrow) {
	CHECK(equivalent("/x/\xE0\xA5\x98.vwx", "/x/\xE0\xA4\x95\xE0\xA4\xBC.vwx"));
	CHECK(equivalent(std::string("/x/") + std::string(40, 'a') + "\xE0\xA5\x98\xE0\xA5\x98.vwx",
	                 std::string("/x/") + std::string(40, 'A') +
	                     "\xE0\xA4\x95\xE0\xA4\xBC\xE0\xA4\x95\xE0\xA4\xBC.vwx"));
}

// 正しくない UTF-8（プラグインには届かない）は、mac は完全に一致するかで決め、Windows は U+FFFD に
// して比べる。
TEST(PathCompareHandlesInvalidUtf8) {
	CHECK(equivalent("/x/\xFF.vwx", "/x/\xFF.vwx"));
	CHECK(!equivalent("/x/\xFF.vwx", "/x/a.vwx"));
	CHECK(!equivalent("/x/a.vwx", "/x/\xFF.vwx"));
#ifdef __APPLE__
	CHECK(!equivalent("/x/\xFF.vwx", "/x/\xFE.vwx"));
	CHECK(!equivalent("/X/\xFF.vwx", "/x/\xFF.vwx"));
#else
	CHECK(equivalent("/x/\xFF.vwx", "/X/\xFE.vwx"));
#endif
}

TEST(PathCompareSeparatesDifferentPaths) {
	CHECK(!equivalent("/Users/me/plan.vwx", "/Users/me/plan2.vwx"));
	CHECK(!equivalent("/Users/me/plan.vwx", "/Users/me/old/plan.vwx"));
	CHECK(!equivalent("/Users/me/caf\xC3\xA9.vwx", "/Users/me/cafe.vwx"));
	CHECK(!equivalent("/a.vwx", ""));
	CHECK(!equivalent("C:\\x\\a.vwx", "C:\\x\\a.vwx "));
}

#endif
