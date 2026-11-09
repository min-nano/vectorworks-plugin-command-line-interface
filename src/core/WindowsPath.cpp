#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <algorithm>

namespace VwCli::core::windows_path {

std::wstring widen(std::string_view text) {
	if (text.empty()) {
		return {};
	}
	int size = static_cast<int>(text.size());
	int length = ::MultiByteToWideChar(CP_UTF8, 0, text.data(), size, nullptr, 0);
	std::wstring wide(static_cast<std::size_t>(length), L'\0');
	::MultiByteToWideChar(CP_UTF8, 0, text.data(), size, wide.data(), length);
	return wide;
}

std::string narrow(std::wstring_view wide) {
	if (wide.empty()) {
		return {};
	}
	int size = static_cast<int>(wide.size());
	int length = ::WideCharToMultiByte(CP_UTF8, 0, wide.data(), size, nullptr, 0, nullptr, nullptr);
	std::string text(static_cast<std::size_t>(length), '\0');
	::WideCharToMultiByte(CP_UTF8, 0, wide.data(), size, text.data(), length, nullptr, nullptr);
	return text;
}

std::wstring extended(std::string_view path) {
	std::wstring wide = widen(path);
	std::replace(wide.begin(), wide.end(), L'/', L'\\');
	if (wide.starts_with(kExtendedPrefix)) {
		return wide;
	}
	if (wide.starts_with(L"\\\\.\\")) {
		return {};
	}
	if (wide.size() >= 3 && wide[1] == L':' && wide[2] == L'\\') {
		return std::wstring(kExtendedPrefix) + wide;
	}
	if (wide.starts_with(L"\\\\")) {
		return std::wstring(kExtendedUncPrefix) + wide.substr(2);
	}
	return {};
}

}
