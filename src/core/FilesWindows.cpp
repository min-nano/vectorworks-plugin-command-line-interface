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
#include <cstring>

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
// SHGetKnownFolderPath の返す絶対パスで、. と .. を含まない。受け付けるのはドライブからの絶対パス
// （C:\…）・UNC（\\server\…）・\\?\ 付きのパスだけで、それ以外（ルート相対・ドライブ相対・
// \\.\ のデバイス名前空間）は空を返して開かせない（予約された名前をデバイスとして開かないため）。
std::wstring extended(std::string_view path) {
	std::wstring wide = widen(path);
	std::replace(wide.begin(), wide.end(), L'/', L'\\');
	if (wide.starts_with(L"\\\\?\\")) {
		return wide;
	}
	if (wide.starts_with(L"\\\\.\\")) {
		return {};
	}
	if (wide.size() >= 3 && wide[1] == L':' && wide[2] == L'\\') {
		return L"\\\\?\\" + wide;
	}
	if (wide.starts_with(L"\\\\")) {
		return L"\\\\?\\UNC\\" + wide.substr(2);
	}
	return {};
}

std::string describe(const char *what, const std::string &path, DWORD error) {
	return std::string(what) + " " + path + ": error " + std::to_string(error);
}

bool isDirectory(const std::wstring &path) {
	DWORD attributes = ::GetFileAttributesW(path.c_str());
	return attributes != INVALID_FILE_ATTRIBUTES && (attributes & FILE_ATTRIBUTE_DIRECTORY);
}

// Refusal は openFile が断った理由。
enum class Refusal {
	None,
	// 通常のファイルでない（リンク・AF_UNIX のソケット・WSL の FIFO とデバイス・ディレクトリ・
	// ディスクにないもの）
	NotRegular,
	// 通常のファイルだが、中身がローカルにない（クラウド・オフラインファイル・HSM などから
	// 取り寄せる）
	NotLocal,
};

// Opened は openFile の結果。
struct Opened {
	// 開けなければ INVALID_HANDLE_VALUE。
	HANDLE handle = INVALID_HANDLE_VALUE;
	// 断ったなら、その理由。
	Refusal refused = Refusal::None;
	// 開けなかった理由（断ったときは 0）。
	DWORD error = 0;
};

// 古い SDK の winnt.h に無い綴り。
#ifndef IO_REPARSE_TAG_AF_UNIX
#define IO_REPARSE_TAG_AF_UNIX 0x80000023L
#endif
#ifndef IO_REPARSE_TAG_LX_FIFO
#define IO_REPARSE_TAG_LX_FIFO 0x80000024L
#endif
#ifndef IO_REPARSE_TAG_LX_CHR
#define IO_REPARSE_TAG_LX_CHR 0x80000025L
#endif
#ifndef IO_REPARSE_TAG_LX_BLK
#define IO_REPARSE_TAG_LX_BLK 0x80000026L
#endif
#ifndef FILE_ATTRIBUTE_RECALL_ON_OPEN
#define FILE_ATTRIBUTE_RECALL_ON_OPEN 0x00040000
#endif
#ifndef FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
#define FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS 0x00400000
#endif

// 中身がローカルにない（クラウド・オフラインファイル・HSM などから取り寄せる）ことを示す
// 属性。読むと取り寄せる処理がメインスレッドで走り、ネットワークを待って止まりうるので断る。
constexpr DWORD kNotLocal =
    FILE_ATTRIBUTE_OFFLINE | FILE_ATTRIBUTE_RECALL_ON_OPEN | FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS;

// rejected はハンドルの指すものを断るか、その理由を返す（断らなければ None）。断るのは、ディスクに
// ないもの・ディレクトリ・リンク（名前の代わりになる reparse point）・AF_UNIX のソケット・WSL の
// FIFO とデバイス（NotRegular）と、中身がローカルにないもの（NotLocal）。tag に属性と reparse tag
// を残す。
Refusal rejected(HANDLE file, FILE_ATTRIBUTE_TAG_INFO &tag) {
	if (::GetFileType(file) != FILE_TYPE_DISK ||
	    !::GetFileInformationByHandleEx(file, FileAttributeTagInfo, &tag, sizeof tag) ||
	    (tag.FileAttributes & FILE_ATTRIBUTE_DIRECTORY) != 0) {
		return Refusal::NotRegular;
	}
	if ((tag.FileAttributes & FILE_ATTRIBUTE_REPARSE_POINT) != 0) {
		switch (tag.ReparseTag) {
		case IO_REPARSE_TAG_AF_UNIX:
		case IO_REPARSE_TAG_LX_FIFO:
		case IO_REPARSE_TAG_LX_CHR:
		case IO_REPARSE_TAG_LX_BLK:
			return Refusal::NotRegular;
		default:
			if (IsReparseTagNameSurrogate(tag.ReparseTag)) {
				return Refusal::NotRegular;
			}
		}
	}
	return (tag.FileAttributes & kNotLocal) != 0 ? Refusal::NotLocal : Refusal::None;
}

// directoryOf は path を含むディレクトリ（最後の \\ まで。ドライブの直下なら \\?\C:\ のような
// ルート）。
std::wstring directoryOf(const std::wstring &path) {
	std::size_t at = path.find_last_of(L'\\');
	return at == std::wstring::npos ? std::wstring() : path.substr(0, at + 1);
}

// sameId は handle が、id の指すファイル（ボリュームとファイル ID が同じ）か。
bool sameId(HANDLE handle, const FILE_ID_INFO &id) {
	FILE_ID_INFO other{};
	return ::GetFileInformationByHandleEx(handle, FileIdInfo, &other, sizeof other) &&
	       other.VolumeSerialNumber == id.VolumeSerialNumber &&
	       std::memcmp(&other.FileId, &id.FileId, sizeof id.FileId) == 0;
}

// openById は id のファイルを、名前を解き直さずに開く（OpenFileById）。同じボリュームの
// ハンドルとして、path のディレクトリをリンクをたどらずに開く。使えなければ（SMB の共有など）
// INVALID_HANDLE_VALUE。
HANDLE openById(const std::wstring &path, const FILE_ID_INFO &id, DWORD access, DWORD share) {
	HANDLE volume = ::CreateFileW(
	    directoryOf(path).c_str(), FILE_READ_ATTRIBUTES,
	    FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE, nullptr, OPEN_EXISTING,
	    FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
	if (volume == INVALID_HANDLE_VALUE) {
		return INVALID_HANDLE_VALUE;
	}
	FILE_ID_DESCRIPTOR descriptor{};
	descriptor.dwSize = sizeof descriptor;
	descriptor.Type = ExtendedFileIdType;
	descriptor.ExtendedFileId = id.FileId;
	HANDLE file = ::OpenFileById(volume, &descriptor, access, share, nullptr, 0);
	::CloseHandle(volume);
	return file;
}

// openFile はディスクの通常のファイルだけを、継承できないハンドルで開く（lpSecurityAttributes に
// null を渡す）。
//
// まず FILE_FLAG_OPEN_REPARSE_POINT で、リンクをたどらずにリンクそのものを開き、断る（rejected）。
// たどると、名前付きパイプへのリンクを置かれたとき、パイプにつないで ReadFile でメインスレッドが
// 止まる（POSIX の O_NOFOLLOW にあたる）。
//
// リンクでない reparse point（OneDrive などのクラウドのファイル・重複除去）は中身を持つふつうの
// ファイルなので、たどって開き直す。開き直すのは、できればファイルの ID で（OpenFileById。名前を
// 解き直さない）、できなければ（SMB の共有など）名前で行い、どちらも 1 回目と同じボリュームと
// ファイル ID のときだけ受け入れる。開き直す間に名前やその reparse point を差し替えられると、
// 差し替えた先を開くこと（その副作用）までは防げないが、受け入れて読み書きはしない。
//
// 1 回目のハンドルは、開き直すまで開いたままにする（その間、呼ぶ側の取り下げで消されない）。
// ただし共有を許さない開き方（share が 0）では開き直しが共有違反になるので、先に閉じる。
Opened openFile(const std::string &path, DWORD access, DWORD share, DWORD disposition) {
	std::wstring wide = extended(path);
	auto failed = [] { return Opened{INVALID_HANDLE_VALUE, Refusal::None, ::GetLastError()}; };
	auto refuse = [](HANDLE handle, Refusal reason) {
		::CloseHandle(handle);
		return Opened{INVALID_HANDLE_VALUE, reason, 0};
	};
	HANDLE first = ::CreateFileW(wide.c_str(), access, share, nullptr, disposition,
	                             FILE_ATTRIBUTE_NORMAL | FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
	if (first == INVALID_HANDLE_VALUE) {
		return failed();
	}
	FILE_ATTRIBUTE_TAG_INFO tag{};
	if (Refusal reason = rejected(first, tag); reason != Refusal::None) {
		return refuse(first, reason);
	}
	if ((tag.FileAttributes & FILE_ATTRIBUTE_REPARSE_POINT) == 0) {
		return {first, Refusal::None, 0};
	}
	FILE_ID_INFO id{};
	if (!::GetFileInformationByHandleEx(first, FileIdInfo, &id, sizeof id)) {
		Opened opened = failed();
		::CloseHandle(first);
		return opened;
	}
	if (share == 0) {
		::CloseHandle(first);
		first = INVALID_HANDLE_VALUE;
	}
	HANDLE file = openById(wide, id, access, share);
	if (file == INVALID_HANDLE_VALUE) {
		file = ::CreateFileW(wide.c_str(), access, share, nullptr, OPEN_EXISTING,
		                     FILE_ATTRIBUTE_NORMAL, nullptr);
	}
	Opened opened = file == INVALID_HANDLE_VALUE ? failed() : Opened{file, Refusal::None, 0};
	if (first != INVALID_HANDLE_VALUE) {
		::CloseHandle(first);
	}
	if (file == INVALID_HANDLE_VALUE) {
		return opened;
	}
	if (Refusal reason = rejected(file, tag); reason != Refusal::None) {
		return refuse(file, reason);
	}
	if (!sameId(file, id)) {
		return refuse(file, Refusal::NotRegular);
	}
	return opened;
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
	// 既にあれば何もしない。無ければ、ルート（\\?\C:\ か \\?\UNC\server\share\）の後から、
	// 区切りごとに作り、ルートには作りに行かない。prepare は受け付けのたびに呼ばれうるので、毎回
	// 失敗する呼び出しをメインスレッドに載せないため（UNC ではネットワークに触れることもある）。
	// 途中で既にあるものは失敗するが、構わない。最後に path がディレクトリかで成否を決める。
	// 区切りは extended が \ に揃えてある。
	std::wstring wide = extended(path);
	if (isDirectory(wide)) {
		return true;
	}
	std::size_t root = 0;
	if (wide.starts_with(L"\\\\?\\UNC\\")) {
		std::size_t server = wide.find(L'\\', 8);
		root = server == std::wstring::npos ? server : wide.find(L'\\', server + 1);
	} else if (wide.size() >= 7 && wide[5] == L':') {
		root = 6;
	}
	for (std::size_t at = root == std::wstring::npos ? root : wide.find(L'\\', root + 1);
	     at != std::wstring::npos; at = wide.find(L'\\', at + 1)) {
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
	// 先に消してから新しく作る（CREATE_NEW）。あるものを開いて切り詰めると（CREATE_ALWAYS）、
	// 置かれたリンク（シンボリックリンクでも hard link でも）の先、つまりスプールの外のファイルを
	// 壊してしまう。消すのは名前だけで、リンクの先は残る。読み取り専用の書きかけが残っていると
	// 消せず、その id には応えられない（読み取り専用の属性を外すのはリンクの先に効きうるので、
	// しない。スプールに書けるのは同じ利用者だけで、作法どおりの呼ぶ側はそうしたファイルを作らない）。
	(void)::DeleteFileW(extended(path).c_str());
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
	// 共有なしで開ければ、呼ぶ側は放している。共有違反なら掴んでいる。通常のファイルでない待つ印は
	// 無いものとする（実行しない側に倒す）。
	Opened opened = openFile(path, GENERIC_READ, 0, OPEN_EXISTING);
	if (opened.handle != INVALID_HANDLE_VALUE) {
		::CloseHandle(opened.handle);
		return WaitState::Released;
	}
	return opened.error == ERROR_SHARING_VIOLATION ? WaitState::Held : WaitState::Missing;
}

Lock::~Lock() { release(); }

Lock::Result Lock::take(const std::string &path, std::string &error) {
	release();
	Opened opened = openFile(path, GENERIC_READ | GENERIC_WRITE, 0, OPEN_ALWAYS);
	if (opened.handle == INVALID_HANDLE_VALUE) {
		if (opened.refused == Refusal::NotRegular) {
			error = "CreateFile " + path + ": 通常のファイルではありません";
			return Result::Failed;
		}
		if (opened.refused == Refusal::NotLocal) {
			error = "CreateFile " + path + ": 中身がローカルにありません（取り寄せる必要がある）";
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
