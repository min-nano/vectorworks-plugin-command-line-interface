#include "core/SaveTarget.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

// save の保存先をファイルシステムに聞く関数の Windows の実装（docs/plugin/tools.md「パスの一致の
// 判定」）。どちらも \\?\ を付けて聞くので、CON.vwx などを装置ではなくファイルの名前として聞き、
// MAX_PATH で失敗することもない。

namespace VwCli::core::save_target {

namespace {

Probe failure(DWORD error) {
	auto code = static_cast<std::int64_t>(error);
	return {isMissing(code) ? Entry::Missing : Entry::Failed, code};
}

}

bool isMissing(std::int64_t osError) {
	// 2 ERROR_FILE_NOT_FOUND・3 ERROR_PATH_NOT_FOUND・67
	// ERROR_BAD_NET_NAME（サーバーに共有が無い）・ 267 ERROR_DIRECTORY（途中の段がファイル）。
	return osError == ERROR_FILE_NOT_FOUND || osError == ERROR_PATH_NOT_FOUND ||
	       osError == ERROR_BAD_NET_NAME || osError == ERROR_DIRECTORY;
}

Probe probeTo(const std::string &path) {
	// GetFileAttributesW はリパースポイントをたどらない
	// （壊れたリンク・ジャンクションもあるとする）。
	DWORD attributes = ::GetFileAttributesW(windows_path::extended(path).c_str());
	if (attributes == INVALID_FILE_ATTRIBUTES) {
		return failure(::GetLastError());
	}
	return {(attributes & FILE_ATTRIBUTE_DIRECTORY) != 0 ? Entry::Directory : Entry::File, 0};
}

Probe probeParent(const std::string &path) {
	// リパースポイントをたどって開く（先の無いリンク・ジャンクションの親は「無い」）。望むアクセスを
	// 0 にし、共有をすべて許すので、ほかのプロセスが共有を絞って開いているフォルダでも 32・5 に
	// ならない。
	HANDLE handle = ::CreateFileW(windows_path::extended(path).c_str(), 0,
	                              FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE, nullptr,
	                              OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS, nullptr);
	if (handle == INVALID_HANDLE_VALUE) {
		return failure(::GetLastError());
	}
	BY_HANDLE_FILE_INFORMATION info{};
	BOOL read = ::GetFileInformationByHandle(handle, &info);
	DWORD error = read ? 0 : ::GetLastError();
	::CloseHandle(handle);
	if (!read) {
		return failure(error);
	}
	return {(info.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) != 0 ? Entry::Directory
	                                                                : Entry::File,
	        0};
}

}
