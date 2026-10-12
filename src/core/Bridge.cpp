//
//	core/Bridge.cpp
//
//	スプールの実装（意図と制約は core/Bridge.h）。ディレクトリの列挙・消す・名前を変えるは
//	std::filesystem で、ファイルを開くのは OS の呼び出しで行う（子プロセスに受け継がせない
//	ため。Bridge.h「子プロセスに受け継がせない」）。
//

#include "core/Bridge.h"
#include "core/Json.h"

#include <algorithm>
#include <array>
#include <cstddef>
#include <filesystem>
#include <string>
#include <system_error>
#include <utility>
#include <vector>

#if defined(_WIN32)
#	ifndef NOMINMAX
#		define NOMINMAX
#	endif
#	ifndef WIN32_LEAN_AND_MEAN
#		define WIN32_LEAN_AND_MEAN
#	endif
#	include <windows.h>
#	include <knownfolders.h>
#	include <shlobj.h>
#else
#	include <cerrno>
#	include <fcntl.h>
#	include <pwd.h>
#	include <sys/file.h>
#	include <sys/stat.h>
#	include <sys/types.h>
#	include <unistd.h>
#endif

namespace VwCli::core {
namespace {
bool EndsWith(const std::string& text, const std::string& suffix) {
	return text.size() >= suffix.size() &&
	       text.compare(text.size() - suffix.size(), suffix.size(), suffix) == 0;
}

// 掃除の対象になるファイルか（要求・応答・待つ印・書きかけ）。
bool IsSpoolFile(const std::string& name) {
	return EndsWith(name, kBridgeRequestSuffix) || EndsWith(name, kBridgeResponseSuffix) ||
	       EndsWith(name, kBridgeWaitSuffix) || EndsWith(name, kBridgeTempSuffix);
}

enum class ReadResult {
	Ok,
	TooLarge,
	Unreadable,
};

#if defined(_WIN32)
// ハンドルを閉じ忘れないための持ち手。
class Handle {
public:
	explicit Handle(HANDLE handle)
	    : fHandle(handle) {}
	~Handle() {
		if (valid())
			CloseHandle(fHandle);
	}
	Handle(const Handle&) = delete;
	Handle& operator=(const Handle&) = delete;
	Handle(Handle&&) = delete;
	Handle& operator=(Handle&&) = delete;

	bool valid() const {
		return fHandle != INVALID_HANDLE_VALUE;
	}
	HANDLE get() const {
		return fHandle;
	}

private:
	HANDLE fHandle;
};

// 上限つきで丸ごと読む。読んでいる間も呼ぶ側が消せるように、共有を許して開く。
// lpSecurityAttributes に null を渡すので、ハンドルは子プロセスに受け継がれない。
ReadResult ReadCapped(const std::filesystem::path& path, std::size_t limit,
  std::string& out) {
	const Handle file(CreateFileW(path.c_str(), GENERIC_READ,
	  FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE,
	  nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr));
	if (!file.valid())
		return ReadResult::Unreadable;
	out.clear();
	std::array<char, 4096> buffer{};
	for (;;) {
		DWORD got = 0;
		if (ReadFile(file.get(), buffer.data(), static_cast<DWORD>(buffer.size()), &got,
		      nullptr) == FALSE)
			return ReadResult::Unreadable;
		if (got == 0)
			return ReadResult::Ok;
		if (out.size() + got > limit)
			return ReadResult::TooLarge;
		out.append(buffer.data(), got);
	}
}

bool WriteWhole(const std::filesystem::path& path, const std::string& text) {
	const Handle file(CreateFileW(path.c_str(), GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS,
	  FILE_ATTRIBUTE_NORMAL, nullptr));
	if (!file.valid())
		return false;
	std::size_t done = 0;
	while (done < text.size()) {
		const auto chunk =
		  static_cast<DWORD>(std::min<std::size_t>(text.size() - done, 1U << 20U));
		DWORD wrote = 0;
		if (WriteFile(file.get(), text.data() + done, chunk, &wrote, nullptr) == FALSE)
			return false;
		done += wrote;
	}
	return true;
}
#else
// 記述子を閉じ忘れないための持ち手。
class Descriptor {
public:
	explicit Descriptor(int fd)
	    : fFd(fd) {}
	~Descriptor() {
		if (fFd >= 0)
			::close(fFd);
	}
	Descriptor(const Descriptor&) = delete;
	Descriptor& operator=(const Descriptor&) = delete;
	Descriptor(Descriptor&&) = delete;
	Descriptor& operator=(Descriptor&&) = delete;

	bool valid() const {
		return fFd >= 0;
	}
	int get() const {
		return fFd;
	}

private:
	int fFd;
};

ReadResult ReadCapped(const std::filesystem::path& path, std::size_t limit,
  std::string& out) {
	const Descriptor file(::open(path.c_str(), O_RDONLY | O_CLOEXEC));
	if (!file.valid())
		return ReadResult::Unreadable;
	out.clear();
	std::array<char, 4096> buffer{};
	for (;;) {
		const ::ssize_t got = ::read(file.get(), buffer.data(), buffer.size());
		if (got < 0) {
			if (errno == EINTR)
				continue;
			return ReadResult::Unreadable;
		}
		if (got == 0)
			return ReadResult::Ok;
		if (out.size() + static_cast<std::size_t>(got) > limit)
			return ReadResult::TooLarge;
		out.append(buffer.data(), static_cast<std::size_t>(got));
	}
}

bool WriteWhole(const std::filesystem::path& path, const std::string& text) {
	const Descriptor file(
	  ::open(path.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, S_IRUSR | S_IWUSR));
	if (!file.valid())
		return false;
	std::size_t done = 0;
	while (done < text.size()) {
		const ::ssize_t wrote = ::write(file.get(), text.data() + done, text.size() - done);
		if (wrote < 0) {
			if (errno == EINTR)
				continue;
			return false;
		}
		done += static_cast<std::size_t>(wrote);
	}
	return true;
}
#endif
} // namespace

// -----------------------------------------------------------------------
std::filesystem::path pathFromUtf8(const std::string& text) {
	std::u8string converted;
	converted.reserve(text.size());
	for (const char ch : text)
		converted.push_back(static_cast<char8_t>(ch));
	return {converted};
}

std::string utf8FromPath(const std::filesystem::path& path) {
	const std::u8string text = path.u8string();
	std::string converted;
	converted.reserve(text.size());
	for (const char8_t ch : text)
		converted.push_back(static_cast<char>(ch));
	return converted;
}

std::filesystem::path bridgeSpoolDirUnder(const std::filesystem::path& base) {
	return base / kBridgeAppDirName / kBridgeSpoolDirName;
}

bool bridgeSpoolDir(std::filesystem::path& out, std::string& error) {
	error.clear();
#if defined(_WIN32)
	PWSTR raw = nullptr;
	const HRESULT hr = SHGetKnownFolderPath(FOLDERID_LocalAppData, 0, nullptr, &raw);
	if (FAILED(hr) || raw == nullptr) {
		CoTaskMemFree(raw);
		error = "LocalAppData の場所を求められません";
		return false;
	}
	const std::filesystem::path base(raw);
	CoTaskMemFree(raw);
	out = bridgeSpoolDirUnder(base);
	return true;
#else
	// 環境変数 HOME は読まない（launchd のジョブで無い・ツールの実行環境で上書きされる
	// ことがある。docs/protocol.md「スプールの場所」）。
	long size = ::sysconf(_SC_GETPW_R_SIZE_MAX);
	if (size <= 0)
		size = 16384;
	std::vector<char> buffer(static_cast<std::size_t>(size));
	struct passwd entry {
	};
	struct passwd* found = nullptr;
	if (::getpwuid_r(::getuid(), &entry, buffer.data(), buffer.size(), &found) != 0 ||
	    found == nullptr || found->pw_dir == nullptr || found->pw_dir[0] == '\0') {
		error = "利用者のホームを求められません";
		return false;
	}
	out = bridgeSpoolDirUnder(std::filesystem::path(found->pw_dir) / "Library" /
	                          "Application Support");
	return true;
#endif
}

bool isValidBridgeId(const std::string& id) {
	if (id.empty() || id.size() > kBridgeMaxIdLength)
		return false;
	return std::all_of(id.begin(), id.end(),
	  [](const char ch) {
		  return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') ||
		         (ch >= 'A' && ch <= 'Z') || ch == '-' || ch == '_';
	  });
}

const char* bridgeCodeName(BridgeCode code) {
	switch (code) {
	case BridgeCode::InvalidRequest:
		return "invalid_request";
	case BridgeCode::UnknownTool:
		return "unknown_tool";
	case BridgeCode::InvalidArgs:
		return "invalid_args";
	case BridgeCode::NoWait:
		return "no_wait";
	case BridgeCode::Busy:
		return "busy";
	case BridgeCode::NoSession:
		return "no_session";
	case BridgeCode::Internal:
		return "internal";
	}
	return "internal";
}

bool parseBridgeRequest(const std::string& text, BridgeRequest& out, std::string& error) {
	Json value;
	if (!Json::parse(text, value, error))
		return false;
	if (!value.isObject()) {
		error = "要求はオブジェクトでなければなりません";
		return false;
	}
	BridgeRequest made;

	// キーは大文字と小文字を区別して照合する（Json::at が区別する）。
	const Json& tool = value.at("tool");
	if (!tool.isString() || tool.asString().empty()) {
		error = "tool は空でない文字列でなければなりません";
		return false;
	}
	made.tool = tool.asString();

	const Json& args = value.at("args");
	if (args.isObject()) {
		made.args = args;
	} else if (!args.isNull()) {
		// オブジェクト以外を {} へ読み替えない（docs/protocol.md「互換性」）。
		error = "args はオブジェクトでなければなりません";
		return false;
	}

	const Json& session = value.at("session");
	if (session.isString()) {
		made.session = session.asString();
	} else if (!session.isNull()) {
		error = "session は文字列でなければなりません";
		return false;
	}

	out = std::move(made);
	return true;
}

BridgeResponse BridgeResponse::success(Json result) {
	BridgeResponse made;
	made.ok = true;
	made.result = std::move(result);
	return made;
}

BridgeResponse BridgeResponse::failure(BridgeCode code, std::string error) {
	BridgeResponse made;
	made.ok = false;
	made.code = code;
	made.error = std::move(error);
	return made;
}

std::string dumpBridgeResponse(const BridgeResponse& response) {
	Json value = Json::object();
	value.set("ok", Json::boolean(response.ok));
	if (response.ok) {
		value.set("result", response.result.isNull() ? Json::object() : response.result);
	} else {
		value.set("code", Json::string(bridgeCodeName(response.code)));
		value.set("error", Json::string(response.error));
	}
	return value.dump();
}

// -----------------------------------------------------------------------
BridgeSpool::BridgeSpool(std::filesystem::path dir)
    : fDir(std::move(dir)) {}

BridgeSpool::~BridgeSpool() {
	unlock();
}

std::filesystem::path BridgeSpool::pathFor(const std::string& name) const {
	return fDir / pathFromUtf8(name);
}

bool BridgeSpool::prepare(std::string& error) {
	error.clear();
	if (fDir.empty()) {
		error = "スプールの場所が決まっていません";
		return false;
	}
	std::error_code ec;
	if (std::filesystem::is_directory(fDir, ec))
		return true;
#if defined(_WIN32)
	// %LOCALAPPDATA% の下は利用者ごと。継承される権限のまま作る。
	std::filesystem::create_directories(fDir, ec);
#else
	// 親から順に 0700 で作る（umask はビットを落とすだけなので、これより緩くならない）。
	std::filesystem::path partial;
	for (const auto& part : fDir) {
		partial /= part;
		if (partial == partial.root_path() || std::filesystem::is_directory(partial, ec))
			continue;
		if (::mkdir(partial.c_str(), S_IRWXU) != 0 && errno != EEXIST)
			break;
	}
#endif
	if (!std::filesystem::is_directory(fDir, ec)) {
		error = "スプールを作れません: " + utf8FromPath(fDir);
		return false;
	}
	return true;
}

LockResult BridgeSpool::lock(std::string& error) {
	error.clear();
	if (locked())
		return LockResult::Locked;
	const std::filesystem::path path = pathFor(kBridgeLockFile);
#if defined(_WIN32)
	// 共有なし（share mode 0）で開いたハンドルを持ち続ける（docs/protocol.md「ロック」）。
	const HANDLE handle = CreateFileW(path.c_str(), GENERIC_READ | GENERIC_WRITE, 0, nullptr,
	  OPEN_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
	if (handle == INVALID_HANDLE_VALUE) {
		if (GetLastError() == ERROR_SHARING_VIOLATION)
			return LockResult::HeldElsewhere;
		error = "ロックを開けません: " + utf8FromPath(path);
		return LockResult::Failed;
	}
	fLock = handle;
	return LockResult::Locked;
#else
	const int fd = ::open(path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC, S_IRUSR | S_IWUSR);
	if (fd < 0) {
		error = "ロックを開けません: " + utf8FromPath(path);
		return LockResult::Failed;
	}
	if (::flock(fd, LOCK_EX | LOCK_NB) != 0) {
		const int why = errno;
		::close(fd);
		if (why == EWOULDBLOCK)
			return LockResult::HeldElsewhere;
		error = "ロックを掴めません: " + utf8FromPath(path);
		return LockResult::Failed;
	}
	fLock = fd;
	return LockResult::Locked;
#endif
}

bool BridgeSpool::locked() const {
#if defined(_WIN32)
	return fLock != nullptr;
#else
	return fLock >= 0;
#endif
}

void BridgeSpool::unlock() {
#if defined(_WIN32)
	if (fLock != nullptr) {
		CloseHandle(fLock);
		fLock = nullptr;
	}
#else
	if (fLock >= 0) {
		::close(fLock);
		fLock = -1;
	}
#endif
}

std::size_t BridgeSpool::sweep() {
	// 先に名前を集める（列挙しながら消さない）。
	std::vector<std::string> names;
	std::error_code ec;
	for (std::filesystem::directory_iterator it(fDir, ec), end; !ec && it != end;
	     it.increment(ec)) {
		const std::string name = utf8FromPath(it->path().filename());
		if (IsSpoolFile(name))
			names.push_back(name);
	}

	std::size_t removed = 0;
	for (const std::string& name : names) {
		const std::string id = name.substr(0, name.find('.'));
		if (waitState(id) == WaitState::Held)
			continue;
		std::error_code removeEc;
		if (std::filesystem::remove(pathFor(name), removeEc))
			++removed;
	}
	return removed;
}

std::vector<std::string> BridgeSpool::poll() const {
	std::vector<std::string> ids;
	const std::string suffix = kBridgeRequestSuffix;
	std::error_code ec;
	for (std::filesystem::directory_iterator it(fDir, ec), end; !ec && it != end;
	     it.increment(ec)) {
		const std::string name = utf8FromPath(it->path().filename());
		if (!EndsWith(name, suffix))
			continue;
		std::string id = name.substr(0, name.size() - suffix.size());
		// 綴りを満たさない id の要求は取り出さない（読まず・消さず・応えない）。
		if (isValidBridgeId(id))
			ids.push_back(std::move(id));
	}
	// 列挙の順は OS 任せなので並べ替える（名前の昇順＝呼ぶ側が送った順。docs/protocol.md「id」）。
	std::sort(ids.begin(), ids.end());
	return ids;
}

TakeResult BridgeSpool::take(const std::string& id, std::string& text) {
	const std::filesystem::path path = pathFor(id + kBridgeRequestSuffix);
	const ReadResult read = ReadCapped(path, kBridgeMaxRequestBytes, text);
	if (read == ReadResult::Unreadable)
		return TakeResult::Skipped;
	// **消せたものだけを実行する**（消さずに実行すると次の回にまた実行してしまう）。
	std::error_code ec;
	if (!std::filesystem::remove(path, ec))
		return TakeResult::Skipped;
	if (read == ReadResult::TooLarge) {
		text.clear();
		return TakeResult::TooLarge;
	}
	return TakeResult::Taken;
}

WaitState BridgeSpool::waitState(const std::string& id) const {
	const std::filesystem::path path = pathFor(id + kBridgeWaitSuffix);
#if defined(_WIN32)
	// 共有なしで開けたら呼ぶ側は放している。共有違反なら掴んでいる。
	const Handle file(CreateFileW(path.c_str(), GENERIC_READ, 0, nullptr, OPEN_EXISTING,
	  FILE_ATTRIBUTE_NORMAL, nullptr));
	if (file.valid())
		return WaitState::Released;
	return GetLastError() == ERROR_SHARING_VIOLATION ? WaitState::Held : WaitState::Missing;
#else
	const Descriptor file(::open(path.c_str(), O_RDONLY | O_CLOEXEC));
	if (!file.valid())
		return WaitState::Missing;
	if (::flock(file.get(), LOCK_EX | LOCK_NB) == 0)
		return WaitState::Released;
	// 掴まれているとき以外の失敗は「無い」に倒す（実行せずに no_wait で応える）。
	return errno == EWOULDBLOCK ? WaitState::Held : WaitState::Missing;
#endif
}

void BridgeSpool::removeWait(const std::string& id) {
	std::error_code ec;
	std::filesystem::remove(pathFor(id + kBridgeWaitSuffix), ec);
}

bool BridgeSpool::reply(const std::string& id, const BridgeResponse& response,
  std::string& error) {
	error.clear();
	if (!isValidBridgeId(id)) {
		error = "id の綴りが正しくありません";
		return false;
	}
	const std::filesystem::path target = pathFor(id + kBridgeResponseSuffix);
	std::filesystem::path temp = target;
	temp += kBridgeTempSuffix;
	if (!WriteWhole(temp, dumpBridgeResponse(response))) {
		std::error_code ec;
		std::filesystem::remove(temp, ec);
		error = "応答を書けません: " + utf8FromPath(temp);
		return false;
	}
	// 書いてから rename で公開する（読み手に書きかけを読ませない）。
	std::error_code ec;
	std::filesystem::rename(temp, target, ec);
	if (ec) {
		std::filesystem::remove(temp, ec);
		error = "応答を公開できません: " + utf8FromPath(target);
		return false;
	}
	return true;
}
} // namespace VwCli::core
