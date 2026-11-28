#pragma once

// save の to を開いている図面のパスと比較する、OS の呼び出しの薄いラッパー
// （docs/plugin/save-target.md「パスの一致の判定」）。mac は CoreFoundation、Windows は
// NormalizeString・CompareStringOrdinal。APPLE と WIN32 の場合のみビルドし、CMake がその場合に
// VWCLI_HAS_PATH_COMPARE を定義する（Linux 用の代替の実装は配置しない。
// 出荷しない実装をテストしても、出荷するものについて何も判明しないため）。

#include <string_view>

namespace VwCli::core::path_compare {

// equivalent は 2 つのパス（UTF-8）を NFC に正規化し、大文字と小文字を区別せずに比較する。畳み方は
// OS の呼び出しの通りで、mac と Windows で一部の文字が異なりうる。mac は UTF-8 として読み取れ
// なければ文字列が完全に一致するか否かで決定し、Windows は不正な UTF-8 を U+FFFD に置換して比較する
// （要求の文字列は解析により正しい UTF-8 となっているため、プラグインからはいずれも発生しない）。
bool equivalent(std::string_view a, std::string_view b);

}
