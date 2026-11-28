#include "core/Files.h"

#include "core/WindowsPath.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

#include <algorithm>

namespace VwCli::core::files {

namespace {

using windows_path::extended;
using windows_path::kExtendedPrefix;
using windows_path::kExtendedUncPrefix;
using windows_path::narrow;

std::string describe(const char *what, const std::string &path, DWORD error) {
	return std::string(what) + " " + path + ": error " + std::to_string(error);
}

bool isDirectory(const std::wstring &path) {
	DWORD attributes = ::GetFileAttributesW(path.c_str());
	return attributes != INVALID_FILE_ATTRIBUTES && (attributes & FILE_ATTRIBUTE_DIRECTORY);
}

// Refusal は openFile が拒否した理由。
enum class Refusal {
	None,
	// 通常のファイルでない（reparse point・ディレクトリ・ディスクに存在しないもの）
	NotRegular,
	// reparse point ではないが、内容がローカルに存在しない（オフラインファイル・HSM などから
	// 取得する。クラウドのファイルは reparse point であるため NotRegular となる）
	NotLocal,
};

// Opened は openFile の結果。
struct Opened {
	// 開けなければ INVALID_HANDLE_VALUE。
	HANDLE handle = INVALID_HANDLE_VALUE;
	// 拒否した場合は、その理由。
	Refusal refused = Refusal::None;
	// 開けなかった理由（拒否した場合は 0）。
	DWORD error = 0;
};

// 古い SDK の winnt.h に存在しない綴り。
#ifndef FILE_ATTRIBUTE_RECALL_ON_OPEN
#define FILE_ATTRIBUTE_RECALL_ON_OPEN 0x00040000
#endif
#ifndef FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
#define FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS 0x00400000
#endif

// 内容がローカルに存在しない（オフラインファイル・HSM などから取得する）ことを示す属性。読み取ると
// 取得の処理がメインスレッドで実行され、ネットワークを待機して停止しうるため拒否する。
constexpr DWORD kNotLocal =
    FILE_ATTRIBUTE_OFFLINE | FILE_ATTRIBUTE_RECALL_ON_OPEN | FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS;

// rejected はハンドルの指すものを拒否するか、その理由を返す（拒否しなければ None）。
Refusal rejected(HANDLE file) {
	FILE_ATTRIBUTE_TAG_INFO tag{};
	if (::GetFileType(file) != FILE_TYPE_DISK ||
	    !::GetFileInformationByHandleEx(file, FileAttributeTagInfo, &tag, sizeof tag) ||
	    (tag.FileAttributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_REPARSE_POINT)) != 0) {
		return Refusal::NotRegular;
	}
	return (tag.FileAttributes & kNotLocal) != 0 ? Refusal::NotLocal : Refusal::None;
}

// openFile はディスクの通常のファイルのみを、継承できないハンドルで開く（lpSecurityAttributes に
// null を渡す）。
//
// FILE_FLAG_OPEN_REPARSE_POINT で、reparse point を辿らずにそれ自体を開き、拒否する（rejected）。
// 辿ると、名前付きパイプへのリンクを配置された場合、パイプに接続して ReadFile でメインスレッドが
// 停止する（POSIX の O_NOFOLLOW に相当する）。リンクでない reparse point（OneDrive などのクラウドの
// ファイル・重複除去）も拒否する。内容を読み取るには辿って開き直すほかなく、その間の差し替えへの
// 対策が、スプールに配置される可能性の低さに見合わないため（スプールは Vectorworks の利用者
// フォルダの中にあり、そこを同期の対象とした環境には対応しない。通信規則「プラグイン側の義務」）。
Opened openFile(const std::string &path, DWORD access, DWORD share, DWORD disposition) {
	HANDLE file = ::CreateFileW(extended(path).c_str(), access, share, nullptr, disposition,
	                            FILE_ATTRIBUTE_NORMAL | FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
	if (file == INVALID_HANDLE_VALUE) {
		return {INVALID_HANDLE_VALUE, Refusal::None, ::GetLastError()};
	}
	if (Refusal reason = rejected(file); reason != Refusal::None) {
		::CloseHandle(file);
		return {INVALID_HANDLE_VALUE, reason, 0};
	}
	return {file, Refusal::None, 0};
}

}

std::optional<std::string> modulePathOf(const void *address) {
	// 参照の数は変更しない（固定するのは Clock.cpp の startClock の担当）。
	HMODULE module = nullptr;
	constexpr DWORD kFromAddress =
	    GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT;
	if (::GetModuleHandleExW(kFromAddress, static_cast<LPCWSTR>(address), &module) == 0) {
		return std::nullopt;
	}
	// パスの長さの上限（\\?\ のパスで 32767 文字）が収まる領域を 1 度で渡す。MAX_PATH を超える
	// パスも存在し、不足すれば切り詰めて領域の大きさを返すため。
	std::wstring path(32768, L'\0');
	DWORD size = static_cast<DWORD>(path.size());
	DWORD length = ::GetModuleFileNameW(module, path.data(), size);
	if (length == 0 || length >= size) {
		return std::nullopt;
	}
	path.resize(length);
	return narrow(path);
}

bool makeDirectories(const std::string &path, std::string &error) {
	// 既に存在すれば何もしない。存在しなければ、ルート（\\?\C:\ か \\?\UNC\server\share\）の
	// 後から、区切りごとに作成し、ルートは作成しない。prepare は受付処理の度に呼ばれうるので、
	// 毎回失敗する呼び出しをメインスレッドで実行しないため（UNC ではネットワークに接続すること
	// もある）。途中で既に存在するものは失敗するが、差し支えない。最後に path がディレクトリか
	// 否かで成否を判定する。区切りは extended が \ に統一してある。
	std::wstring wide = extended(path);
	if (isDirectory(wide)) {
		return true;
	}
	// root はルートの末尾の \ の位置。UNC で share の後方に \ が無ければ npos で、
	// 途中のフォルダは作成しない（path そのものは下の CreateDirectoryW が作成を試みる）。
	std::size_t root = 0;
	if (wide.starts_with(kExtendedUncPrefix)) {
		// \\?\UNC\server\share\ の share の後方の \。
		std::size_t server = wide.find(L'\\', kExtendedUncPrefix.size());
		root = server == std::wstring::npos ? server : wide.find(L'\\', server + 1);
	} else if (std::size_t drive = kExtendedPrefix.size();
	           wide.size() > drive + 2 && wide[drive + 1] == L':') {
		// \\?\C:\ の : の後方の \。
		root = drive + 2;
	}
	for (std::size_t at = root == std::wstring::npos ? root : wide.find(L'\\', root + 1);
	     at != std::wstring::npos; at = wide.find(L'\\', at + 1)) {
		(void)::CreateDirectoryW(wide.substr(0, at).c_str(), nullptr);
	}
	if (::CreateDirectoryW(wide.c_str(), nullptr)) {
		return true;
	}
	// isDirectory が上書きする前に、作成できなかった理由を保持する。
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
	// 読み取りの間は削除させない（呼び出し元の取り下げは、削除できないことでプラグインが
	// 受け取ったと判明する。通信規則「待機の中止」）。
	HANDLE file = openFile(path, GENERIC_READ, FILE_SHARE_READ, OPEN_EXISTING).handle;
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
	// 先に削除してから新たに作成する（CREATE_NEW）。既存のものを開いて切り詰めると
	// （CREATE_ALWAYS）、配置されたリンク（シンボリックリンクでも hard link でも）の先、すなわち
	// スプールの外のファイルを破壊する。削除するのは名前のみで、リンクの先は残る。読み取り専用の
	// 書き込み途中のファイルが残存していると削除できず、その id には応答できない（読み取り専用の
	// 属性を除去することはリンクの先に作用しうるため、行わない。スプールに書き込めるのは同一の
	// 利用者のみで、通信規則の通りの呼び出し元はそうしたファイルを作成しない）。
	(void)files::remove(path);
	HANDLE file = openFile(path, GENERIC_WRITE, 0, CREATE_NEW).handle;
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
	// 共有なしで開ければ、呼び出し元は解放している。共有違反ならば保持している。通常のファイルで
	// ない待機標識は存在しないものとする（実行しない側に倒す）。絶対パスでなければ開かない（\\?\ を
	// 付けられない）ため、存在しないものとする。
	std::wstring wide = extended(path);
	if (wide.empty()) {
		return WaitState::Missing;
	}
	Opened opened = openFile(path, GENERIC_READ, 0, OPEN_EXISTING);
	if (opened.handle != INVALID_HANDLE_VALUE) {
		::CloseHandle(opened.handle);
		return WaitState::Released;
	}
	switch (opened.error) {
	case ERROR_SHARING_VIOLATION:
		return WaitState::Held;
	// 0 は断った（通常のファイルでない・中身がローカルにない）。他は無い（名前が長すぎて置けない・
	// 名前に使えないものも）。
	case 0:
	case ERROR_FILE_NOT_FOUND:
	case ERROR_PATH_NOT_FOUND:
	case ERROR_INVALID_NAME:
	case ERROR_FILENAME_EXCED_RANGE:
		return WaitState::Missing;
	case ERROR_ACCESS_DENIED:
		// フォルダは、POSIX（開けてから通常のファイルでないと分かる）と同じく無いものとする（Windows
		// では FILE_FLAG_BACKUP_SEMANTICS なしにフォルダを開けず、断られる）。
		if (isDirectory(wide)) {
			return WaitState::Missing;
		}
		break;
	}
	// 無い以外の理由で開けなければ、確かめられない（掃除が、待っている呼び出し元のものを消さないように）。
	return WaitState::Unknown;
}

Lock::Result Lock::take(const std::string &path, std::string &error) {
	release();
	Opened opened = openFile(path, GENERIC_READ | GENERIC_WRITE, 0, OPEN_ALWAYS);
	if (opened.refused != Refusal::None && !recreated_) {
		// ロックのファイルはプラグインのもので内容を持たないため、拒否した場合は削除して再作成する
		// （重複除去・WOF の圧縮・同期が解除されていれば、それで解消する）。拒否できたのは共有
		// なしで開けたためで、他の Vectorworks は保持していない。削除するのは名前のみで、reparse
		// point は辿らない（クラウドのファイルならば、削除する際に同期のサービスを待機することが
		// ある）。再作成するのは 1 度のみ（Files.h）。
		recreated_ = true;
		if (!remove(path)) {
			error = describe("DeleteFile", path, ::GetLastError());
			return Result::Failed;
		}
		opened = openFile(path, GENERIC_READ | GENERIC_WRITE, 0, OPEN_ALWAYS);
	}
	if (opened.handle == INVALID_HANDLE_VALUE) {
		if (opened.refused == Refusal::NotRegular) {
			error = "CreateFile " + path +
			        ": 通常のファイルではありません（スプールを同期・WOF の圧縮・重複除去の対象から"
			        "外し、Vectorworks を起動し直してください）";
			return Result::Failed;
		}
		if (opened.refused == Refusal::NotLocal) {
			error = "CreateFile " + path +
			        ": 中身がローカルにありません（スプールをオフラインファイル・HSM の対象から"
			        "外し、Vectorworks を起動し直してください）";
			return Result::Failed;
		}
		if (opened.error == ERROR_SHARING_VIOLATION) {
			return Result::Busy;
		}
		error = describe("CreateFile", path, opened.error);
		return Result::Failed;
	}
	handle_ = reinterpret_cast<std::intptr_t>(opened.handle);
	return Result::Taken;
}

void Lock::release() {
	if (handle_ != kNone) {
		::CloseHandle(reinterpret_cast<HANDLE>(handle_));
		handle_ = kNone;
	}
}

}
