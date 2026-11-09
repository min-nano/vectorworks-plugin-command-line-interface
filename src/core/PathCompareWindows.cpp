#include "core/PathCompare.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <string>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC の UTF-16 にする。widen は正しくない UTF-8 を U+FFFD
// に置き換えるので、 NormalizeString
// が文字の誤りで失敗することはない。失敗したら正規化せずに返す（比べ方が厳しくなる
// だけで、警告のダイアログが出るのは完全に一致するときだけ）。
std::wstring normalized(std::string_view text) {
	std::wstring wide = windows_path::widen(text);
	if (wide.empty()) {
		return wide;
	}
	int size = static_cast<int>(wide.size());
	// NFC で長くなる文字もある（U+0958 は 2 つになる）。足りなければ、返された見積もりで繰り返す。
	int length = size;
	for (;;) {
		std::wstring out(static_cast<std::size_t>(length), L'\0');
		int written = ::NormalizeString(NormalizationC, wide.data(), size, out.data(), length);
		if (written <= 0 && ::GetLastError() == ERROR_INSUFFICIENT_BUFFER && -written > length) {
			length = -written;
			continue;
		}
		return written > 0 ? out.substr(0, static_cast<std::size_t>(written)) : wide;
	}
}

}

bool equivalent(std::string_view a, std::string_view b) {
	std::wstring left = normalized(a);
	std::wstring right = normalized(b);
	return ::CompareStringOrdinal(left.data(), static_cast<int>(left.size()), right.data(),
	                              static_cast<int>(right.size()), TRUE) == CSTR_EQUAL;
}

}
