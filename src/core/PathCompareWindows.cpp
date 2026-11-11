#include "core/PathCompare.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <algorithm>
#include <string>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC の UTF-16 にする。widen は正しくない
// UTF-8 を U+FFFD に置き換えるので、NormalizeString が文字の誤りで
// 失敗することはない。それでも失敗したら正規化せずに返す（比べ方が
// 厳しくなるだけで、警告のダイアログが出るのは完全に一致するときだけ）。
std::wstring normalized(std::string_view text) {
	std::wstring wide = windows_path::widen(text);
	if (wide.empty()) {
		return wide;
	}
	int size = static_cast<int>(wide.size());
	// NFC で長くなる文字もある（U+0958 は 2 つになる）。領域が足りなければ、返された見積もりと
	// いまの倍の大きいほうで繰り返す（見積もりがいまの長さ以下でも、正規化せずに諦めない）。回数に
	// 上限を置き、止まらなくなることは避ける（2^16 倍で、要求の大きさの上限を十分に超える）。
	int length = size;
	for (int attempt = 0; attempt < 16; ++attempt) {
		std::wstring out(static_cast<std::size_t>(length), L'\0');
		int written = ::NormalizeString(NormalizationC, wide.data(), size, out.data(), length);
		if (written > 0) {
			out.resize(static_cast<std::size_t>(written));
			return out;
		}
		if (::GetLastError() != ERROR_INSUFFICIENT_BUFFER) {
			break;
		}
		length = std::max(-written, length * 2);
	}
	return wide;
}

}

bool equivalent(std::string_view a, std::string_view b) {
	std::wstring left = normalized(a);
	std::wstring right = normalized(b);
	return ::CompareStringOrdinal(left.data(), static_cast<int>(left.size()), right.data(),
	                              static_cast<int>(right.size()), TRUE) == CSTR_EQUAL;
}

}
