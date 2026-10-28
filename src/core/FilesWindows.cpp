#include "core/Files.h"

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
// GetModuleFileNameW の返すモジュールの絶対パスから求め、. と .. を含まない。受け付けるのは
// ドライブからの絶対パス（C:\…）・UNC（\\server\…）・\\?\ 付きのパスだけで、それ以外（ルート
// 相対・ドライブ相対・\\.\ のデバイス名前空間）は空を返して開かせない（予約された名前を
// デバイスとして開かないため）。
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
	// 通常のファイルでない（reparse point・ディレクトリ・ディスクにないもの）
	NotRegular,
	// reparse point ではないが、中身がローカルにない（オフラインファイル・HSM などから取り寄せる。
	// クラウドのファイルは reparse point なので NotRegular になる）
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
#ifndef FILE_ATTRIBUTE_RECALL_ON_OPEN
#define FILE_ATTRIBUTE_RECALL_ON_OPEN 0x00040000
#endif
#ifndef FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
#define FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS 0x00400000
#endif

// 中身がローカルにない（オフラインファイル・HSM などから取り寄せる）ことを示す属性。読むと
// 取り寄せる処理がメインスレッドで走り、ネットワークを待って止まりうるので断る。
constexpr DWORD kNotLocal =
    FILE_ATTRIBUTE_OFFLINE | FILE_ATTRIBUTE_RECALL_ON_OPEN | FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS;

// rejected はハンドルの指すものを断るか、その理由を返す（断らなければ None）。
Refusal rejected(HANDLE file) {
	FILE_ATTRIBUTE_TAG_INFO tag{};
	if (::GetFileType(file) != FILE_TYPE_DISK ||
	    !::GetFileInformationByHandleEx(file, FileAttributeTagInfo, &tag, sizeof tag) ||
	    (tag.FileAttributes & (FILE_ATTRIBUTE_DIRECTORY | FILE_ATTRIBUTE_REPARSE_POINT)) != 0) {
		return Refusal::NotRegular;
	}
	return (tag.FileAttributes & kNotLocal) != 0 ? Refusal::NotLocal : Refusal::None;
}

// openFile はディスクの通常のファイルだけを、継承できないハンドルで開く（lpSecurityAttributes に
// null を渡す）。
//
// FILE_FLAG_OPEN_REPARSE_POINT で、reparse point をたどらずにそれ自体を開き、断る（rejected）。
// たどると、名前付きパイプへのリンクを置かれたとき、パイプにつないで ReadFile でメインスレッドが
// 止まる（POSIX の O_NOFOLLOW にあたる）。リンクでない reparse point（OneDrive などのクラウドの
// ファイル・重複除去）も断る。中身を読むにはたどって開き直すしかなく、その間の差し替えへの
// 備えが、スプールに置かれる見込みの低さに見合わないため（スプールは Vectorworks の利用者フォルダの
// 中で、そこを同期の対象にした環境には対応しない。通信規則「プラグイン側の義務」）。
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

std::string join(const std::string &dir, std::string_view name) {
	std::string path = dir;
	if (!path.empty() && path.back() != '\\' && path.back() != '/') {
		path += '\\';
	}
	path.append(name);
	return path;
}

std::optional<std::string> modulePath() {
	// モジュールの中の番地なら何でもよいので、関数ではなく静的なデータの番地を渡す。参照の数は
	// 変えない（留めるのは Clock.cpp の startClock の受け持ち）。
	static const char anchor = 0;
	HMODULE module = nullptr;
	constexpr DWORD kFromAddress =
	    GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT;
	if (::GetModuleHandleExW(kFromAddress, reinterpret_cast<LPCWSTR>(&anchor), &module) == 0) {
		return std::nullopt;
	}
	// パスの長さの上限（\\?\ のパスで 32767 文字）が収まる領域を 1 度で渡す。MAX_PATH を超える
	// パスもあり、足りなければ切り詰めて領域の大きさを返すため。
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
	// 読む間は消させない（呼ぶ側の取り下げは、消せないことでプラグインが受け取ったと分かる。通信規則
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
	// しない。スプールに書けるのは同じ利用者だけで、通信規則どおりの呼ぶ側はそうしたファイルを作らない）。
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
	if (opened.refused != Refusal::None && !recreated_) {
		// ロックのファイルはプラグインのもので中身を持たないので、断ったら消して作り直す
		// （重複除去・WOF の圧縮・同期が外れていれば、それで直る）。断れたのは共有なしで
		// 開けたからで、ほかの Vectorworks は掴んでいない。消すのは名前だけで、reparse point は
		// たどらない（クラウドのファイルなら、消すときに同期のサービスを待つことがある）。
		// 作り直すのは 1 度だけ（Files.h）。
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
