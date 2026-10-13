#include "core/Files.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <knownfolders.h>
#include <objbase.h>
#include <shlobj.h>

#include <algorithm>

namespace VwCli::core::files {

namespace {

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

// extended はパスを \\?\ を付けた UTF-16 にする（スプールのファイルはすべてこれで開く）。
//
// 付けないと、CON・NUL・AUX・COM1 などの名前は（Windows 10 までは拡張子があっても）ファイルでは
// なくデバイスを指す。id の綴りはそうした名前を除かないので、\\?\ を付けて作られた NUL.req.json
// を読むつもりで、NUL デバイスを開くことになる。付けると名前を解釈せずにそのまま使う。
//
// \\?\ のパスは / を区切りとして読まず、. と .. も解かないので、/ を区切りに直す。スプールの場所は
// SHGetKnownFolderPath の返す絶対パスで、. と .. を含まない。相対パスには付けられないので、
// そのまま返す。
std::wstring extended(std::string_view path) {
	std::wstring wide = widen(path);
	std::replace(wide.begin(), wide.end(), L'/', L'\\');
	if (wide.starts_with(L"\\\\?\\")) {
		return wide;
	}
	if (wide.size() >= 3 && wide[1] == L':' && wide[2] == L'\\') {
		return L"\\\\?\\" + wide;
	}
	if (wide.starts_with(L"\\\\")) {
		return L"\\\\?\\UNC\\" + wide.substr(2);
	}
	return wide;
}

std::string describe(const char *what, const std::string &path, DWORD error) {
	return std::string(what) + " " + path + ": error " + std::to_string(error);
}

bool isDirectory(const std::wstring &path) {
	DWORD attributes = ::GetFileAttributesW(path.c_str());
	return attributes != INVALID_FILE_ATTRIBUTES && (attributes & FILE_ATTRIBUTE_DIRECTORY);
}

// openFile は継承できないハンドルで開く（lpSecurityAttributes に null を渡す）。
HANDLE openFile(const std::string &path, DWORD access, DWORD share, DWORD disposition) {
	return ::CreateFileW(extended(path).c_str(), access, share, nullptr, disposition,
	                     FILE_ATTRIBUTE_NORMAL, nullptr);
}

}

std::string join(const std::string &dir, std::string_view name) {
	std::string path = dir;
	if (!path.empty() && path.back() != '\\' && path.back() != '/') {
		path += '\\';
	}
	path.append(name);
	return path;
}

std::optional<std::string> appRoot() {
	// 環境変数 LOCALAPPDATA は読まない（起動のしかたで変わりうるので、CLI と食い違うと要求が
	// 届かないため）。失敗しても返された領域は解放する（SHGetKnownFolderPath の約束）。
	PWSTR path = nullptr;
	HRESULT result = ::SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &path);
	std::optional<std::string> root;
	if (SUCCEEDED(result) && path) {
		root = narrow(path);
	}
	::CoTaskMemFree(path);
	return root;
}

bool makeDirectories(const std::string &path, std::string &error) {
	// 区切りごとに作る。\\?\ の部分・ドライブ（C:）・既にあるものは失敗するが、構わない。最後に
	// path がディレクトリかで成否を決める。
	std::wstring wide = extended(path);
	for (std::size_t at = wide.find_first_of(L"\\/", 1); at != std::wstring::npos;
	     at = wide.find_first_of(L"\\/", at + 1)) {
		(void)::CreateDirectoryW(wide.substr(0, at).c_str(), nullptr);
	}
	if (::CreateDirectoryW(wide.c_str(), nullptr)) {
		return true;
	}
	// isDirectory が上書きする前に、作れなかった理由を残す。
	DWORD failure = ::GetLastError();
	if (isDirectory(wide)) {
		return true;
	}
	error = describe("CreateDirectory", path, failure);
	return false;
}

std::optional<std::vector<std::string>> list(const std::string &dir) {
	WIN32_FIND_DATAW entry{};
	HANDLE find = ::FindFirstFileExW(extended(join(dir, "*")).c_str(), FindExInfoBasic, &entry,
	                                 FindExSearchNameMatch, nullptr, 0);
	if (find == INVALID_HANDLE_VALUE) {
		return std::nullopt;
	}
	std::vector<std::string> names;
	do {
		std::wstring_view name = entry.cFileName;
		if (name != L"." && name != L"..") {
			names.push_back(narrow(name));
		}
	} while (::FindNextFileW(find, &entry));
	::FindClose(find);
	return names;
}

std::optional<std::string> read(const std::string &path, std::size_t limit) {
	// 読む間は消させない（呼ぶ側の取り下げは、消せないことでプラグインが受け取ったと分かる。作法
	// 「待つのをやめる」）。
	HANDLE file = openFile(path, GENERIC_READ, FILE_SHARE_READ, OPEN_EXISTING);
	if (file == INVALID_HANDLE_VALUE) {
		return std::nullopt;
	}
	std::string data;
	char buffer[65536];
	while (data.size() < limit) {
		DWORD want = static_cast<DWORD>(std::min(sizeof buffer, limit - data.size()));
		DWORD got = 0;
		if (!::ReadFile(file, buffer, want, &got, nullptr)) {
			::CloseHandle(file);
			return std::nullopt;
		}
		if (got == 0) {
			break;
		}
		data.append(buffer, got);
	}
	::CloseHandle(file);
	return data;
}

bool write(const std::string &path, std::string_view data) {
	HANDLE file = openFile(path, GENERIC_WRITE, 0, CREATE_ALWAYS);
	if (file == INVALID_HANDLE_VALUE) {
		return false;
	}
	bool ok = true;
	while (ok && !data.empty()) {
		DWORD put = 0;
		DWORD size = static_cast<DWORD>(std::min<std::size_t>(data.size(), 1u << 30));
		ok = ::WriteFile(file, data.data(), size, &put, nullptr) != 0;
		data.remove_prefix(put);
	}
	return ::CloseHandle(file) && ok;
}

bool rename(const std::string &from, const std::string &to) {
	std::wstring source = extended(from);
	std::wstring target = extended(to);
	return ::MoveFileExW(source.c_str(), target.c_str(), MOVEFILE_REPLACE_EXISTING) != 0;
}

bool remove(const std::string &path) { return ::DeleteFileW(extended(path).c_str()) != 0; }

WaitState probe(const std::string &path) {
	// 共有なしで開ければ、呼ぶ側は放している。共有違反なら掴んでいる。
	HANDLE file = openFile(path, GENERIC_READ, 0, OPEN_EXISTING);
	if (file != INVALID_HANDLE_VALUE) {
		::CloseHandle(file);
		return WaitState::Released;
	}
	return ::GetLastError() == ERROR_SHARING_VIOLATION ? WaitState::Held : WaitState::Missing;
}

Lock::~Lock() { release(); }

Lock::Result Lock::take(const std::string &path, std::string &error) {
	release();
	HANDLE file = openFile(path, GENERIC_READ | GENERIC_WRITE, 0, OPEN_ALWAYS);
	if (file == INVALID_HANDLE_VALUE) {
		DWORD failure = ::GetLastError();
		if (failure == ERROR_SHARING_VIOLATION) {
			return Result::Busy;
		}
		error = describe("CreateFile", path, failure);
		return Result::Failed;
	}
	handle_ = reinterpret_cast<std::intptr_t>(file);
	return Result::Taken;
}

void Lock::release() {
	if (handle_ != kNone) {
		::CloseHandle(reinterpret_cast<HANDLE>(handle_));
		handle_ = kNone;
	}
}

}
