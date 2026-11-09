#pragma once

// save の to を開いている図面のパスと比べる、OS の呼び出しの薄い包み（docs/plugin/tools.md「パスの
// 一致の判定」）。mac は CoreFoundation、Windows は NormalizeString・CompareStringOrdinal。APPLE と
// WIN32 のときだけビルドし、CMake がそのとき VWCLI_HAS_PATH_COMPARE を定義する（Linux 用の代わりの
// 実装は置かない。出荷しない実装をテストしても、出荷するものについて何も分からないため）。

#include <string_view>

namespace VwCli::core::path_compare {

// equivalent は 2 つのパス（UTF-8）を NFC に正規化し、大文字と小文字を区別せずに比べる。畳み方は
// OS の呼び出しのとおりで、mac と Windows で一部の文字が違いうる。mac は UTF-8 として読めなければ
// 文字列が完全に一致するかで決め、Windows は正しくない UTF-8 を U+FFFD にして比べる（要求の文字列は
// 解析で正しい UTF-8 になっているので、プラグインからはどちらも起きない）。
bool equivalent(std::string_view a, std::string_view b);

}
