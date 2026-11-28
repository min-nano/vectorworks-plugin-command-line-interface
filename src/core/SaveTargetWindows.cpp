#include "core/SaveTarget.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

// save の保存先をファイルシステムに問い合わせる関数の Windows の実装
// （docs/plugin/save-target.md「状態の検証」）。いずれも \\?\ を付けて問い合わせるため、CON.vwx
// などをデバイスではなくファイルの名前として問い合わせ、MAX_PATH で失敗することもない。

namespace VwCli::core::save_target {

namespace {

Probe failure(DWORD error) {
	auto code = static_cast<std::int64_t>(error);
	return {isMissing(code) ? Entry::Missing : Entry::Failed, code};
}

// unformed は、\\?\ の形にできないパスの答え（相対・ドライブ相対・\\.\ など）。
// OS に聞かずに、確かめられない（ERROR_INVALID_NAME）とする。
// 空のパスを OS に渡すと「無い」と答えうるので、確かめを素通りして、既にある
// ファイルを上書きしないように、失敗する側に倒す。checkWindowsTo がこうした形を
// 先に断るので、普通は来ない（2 つの関数の綴りが食い違ったときの歯止め）。
Probe unformed() { return {Entry::Failed, static_cast<std::int64_t>(ERROR_INVALID_NAME)}; }

}

bool isMissing(std::int64_t osError) {
	// 2 ERROR_FILE_NOT_FOUND・3 ERROR_PATH_NOT_FOUND・67 ERROR_BAD_NET_NAME（サーバーに共有が
	// 存在しない）・267 ERROR_DIRECTORY（途中の段がファイル）。
	return osError == ERROR_FILE_NOT_FOUND || osError == ERROR_PATH_NOT_FOUND ||
	       osError == ERROR_BAD_NET_NAME || osError == ERROR_DIRECTORY;
}

Probe probeTo(const std::string &path) {
	// GetFileAttributesW はリパースポイントをたどらない
	// （壊れたリンク・ジャンクションも存在するものとする）。
	std::wstring wide = windows_path::extended(path);
	if (wide.empty()) {
		return unformed();
	}
	DWORD attributes = ::GetFileAttributesW(wide.c_str());
	if (attributes == INVALID_FILE_ATTRIBUTES) {
		return failure(::GetLastError());
	}
	return {(attributes & FILE_ATTRIBUTE_DIRECTORY) != 0 ? Entry::Directory : Entry::File, 0};
}

Probe probeParent(const std::string &path) {
	// リパースポイントをたどって開く（参照先の無いリンク・ジャンクションの親は「無い」）。要求する
	// アクセスを 0 とし、共有を全て許可するため、他のプロセスが共有を制限して開いているフォルダでも
	// 32・5 とならない。
	std::wstring wide = windows_path::extended(path);
	if (wide.empty()) {
		return unformed();
	}
	HANDLE handle =
	    ::CreateFileW(wide.c_str(), 0, FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
		              nullptr, OPEN_EXISTING, FILE_FLAG_BACKUP_SEMANTICS, nullptr);
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
