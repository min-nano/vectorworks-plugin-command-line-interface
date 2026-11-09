#include "core/PathCompare.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <optional>
#include <string>

namespace VwCli::core::path_compare {

namespace {

// normalized は UTF-8 を NFC の UTF-16 にする。できなければ空。
std::optional<std::wstring> normalized(std::string_view text) {
	std::wstring wide = windows_path::widen(text);
	if (wide.empty()) {
		return wide;
	}
	int size = static_cast<int>(wide.size());
	int length = ::NormalizeString(NormalizationC, wide.data(), size, nullptr, 0);
	// 返る長さは見積もりなので、足りなければ言われた長さで繰り返す。
	for (int attempt = 0; attempt < 8 && length > 0; ++attempt) {
		std::wstring out(static_cast<std::size_t>(length), L'\0');
		int written = ::NormalizeString(NormalizationC, wide.data(), size, out.data(), length);
		if (written > 0) {
			out.resize(static_cast<std::size_t>(written));
			return out;
		}
		if (::GetLastError() != ERROR_INSUFFICIENT_BUFFER) {
			return std::nullopt;
		}
		length = -written;
	}
	return std::nullopt;
}

}

bool equivalent(std::string_view a, std::string_view b) {
	std::optional<std::wstring> left = normalized(a);
	std::optional<std::wstring> right = normalized(b);
	if (!left || !right) {
		return a == b;
	}
	return ::CompareStringOrdinal(left->data(), static_cast<int>(left->size()), right->data(),
	                              static_cast<int>(right->size()), TRUE) == CSTR_EQUAL;
}

}
