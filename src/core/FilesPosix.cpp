#include "core/Files.h"

#include <algorithm>
#include <cerrno>
#include <cstring>
#include <dirent.h>
#include <fcntl.h>
#include <pwd.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

namespace VwCli::core::files {

namespace {

// closeQuietly は記述子を閉じる。閉じた結果は使わない（読み終えた・ロックを放すだけなので）。
void closeQuietly(int fd) { (void)::close(fd); }

std::string describe(const char *what, const std::string &path, int error) {
	return std::string(what) + " " + path + ": " + std::strerror(error);
}

// スプールのファイルを開くときに必ず付ける。O_NONBLOCK は、FIFO を置かれても読み手・書き手を
// 待って止まらないため（止まるのは Vectorworks のメインスレッドで、利用者の未保存の作業が
// 失われる）。通常のファイルの読み書きには効かない。O_NOFOLLOW は、最後の名前がシンボリック
// リンクなら開かないため。開いてから断るのでは、リンクの先がデバイスならそのドライバの open が
// 先に走ってしまう（シリアルポートの信号線が上がるなど）。スプールのファイルをリンク越しに開く
// 理由は無い。O_NOCTTY は端末を開いても制御端末にしないため。
constexpr int kOpenFlags = O_CLOEXEC | O_NONBLOCK | O_NOFOLLOW | O_NOCTTY;

// Opened は openRegular の結果。
struct Opened {
	// 開けなければ -1。
	int fd = -1;
	// 通常のファイルでない（FIFO・デバイス・ディレクトリ・シンボリックリンク）。
	bool notRegular = false;
	// 開けなかった理由（notRegular のときは 0）。
	int error = 0;
};

// openRegular は通常のファイルだけを開く。
Opened openRegular(const std::string &path, int flags, mode_t mode = 0) {
	int fd = ::open(path.c_str(), flags | kOpenFlags, mode);
	if (fd < 0) {
		int failure = errno;
		// O_NOFOLLOW でシンボリックリンクを断ると ELOOP（FreeBSD 系は EMLINK）。ELOOP は親の
		// ディレクトリでリンクが循環しているときにも返るので、最後の名前がリンクのときだけ
		// 「通常のファイルでない」とし、それ以外は本当の原因を返す。
		struct stat link{};
		if ((failure == ELOOP || failure == EMLINK) && ::lstat(path.c_str(), &link) == 0 &&
		    S_ISLNK(link.st_mode)) {
			return {-1, true, 0};
		}
		return {-1, false, failure};
	}
	struct stat info{};
	if (::fstat(fd, &info) != 0) {
		int failure = errno;
		closeQuietly(fd);
		return {-1, false, failure};
	}
	if (!S_ISREG(info.st_mode)) {
		closeQuietly(fd);
		return {-1, true, 0};
	}
	return {fd, false, 0};
}

}

std::string join(const std::string &dir, std::string_view name) {
	std::string path = dir;
	if (!path.empty() && path.back() != '/') {
		path += '/';
	}
	path.append(name);
	return path;
}

std::optional<std::string> appRoot() {
	// ホームは環境変数 HOME ではなく passwd の項目から求める（launchd のジョブで無い・ツールの
	// 実行環境で上書きされることがあり、CLI と食い違うと要求が届かないため）。
	long size = ::sysconf(_SC_GETPW_R_SIZE_MAX);
	std::string buffer(size > 0 ? static_cast<std::size_t>(size) : 16384, '\0');
	passwd entry{};
	passwd *found = nullptr;
	while (::getpwuid_r(::getuid(), &entry, buffer.data(), buffer.size(), &found) == ERANGE) {
		buffer.resize(buffer.size() * 2);
	}
	if (!found || !found->pw_dir || found->pw_dir[0] == '\0') {
		return std::nullopt;
	}
	return join(join(found->pw_dir, "Library"), "Application Support");
}

bool makeDirectories(const std::string &path, std::string &error) {
	// 既にあれば何もしない（prepare は受け付けのたびに呼ばれうるので、毎回失敗する mkdir を
	// メインスレッドに載せない）。
	struct stat existing{};
	if (::stat(path.c_str(), &existing) == 0 && S_ISDIR(existing.st_mode)) {
		return true;
	}
	// 区切りごとに作る。途中が既にあれば EEXIST で、それは構わない。最後に path が
	// ディレクトリかで成否を決める。
	for (std::size_t at = path.find('/', 1); at != std::string::npos; at = path.find('/', at + 1)) {
		(void)::mkdir(path.substr(0, at).c_str(), 0700);
	}
	if (::mkdir(path.c_str(), 0700) == 0) {
		return true;
	}
	int failure = errno;
	struct stat info{};
	if (::stat(path.c_str(), &info) == 0 && S_ISDIR(info.st_mode)) {
		return true;
	}
	error = describe("mkdir", path, failure);
	return false;
}

std::optional<std::vector<std::string>> list(const std::string &dir) {
	DIR *stream = ::opendir(dir.c_str());
	if (!stream) {
		return std::nullopt;
	}
	std::vector<std::string> names;
	while (const dirent *entry = ::readdir(stream)) {
		std::string_view name = entry->d_name;
		if (name != "." && name != "..") {
			names.emplace_back(name);
		}
	}
	::closedir(stream);
	return names;
}

std::optional<std::string> read(const std::string &path, std::size_t limit) {
	int fd = openRegular(path, O_RDONLY).fd;
	if (fd < 0) {
		return std::nullopt;
	}
	std::string data;
	char buffer[65536];
	while (data.size() < limit) {
		std::size_t want = std::min(sizeof buffer, limit - data.size());
		ssize_t got = ::read(fd, buffer, want);
		if (got < 0) {
			if (errno == EINTR) {
				continue;
			}
			closeQuietly(fd);
			return std::nullopt;
		}
		if (got == 0) {
			break;
		}
		data.append(buffer, static_cast<std::size_t>(got));
	}
	closeQuietly(fd);
	return data;
}

bool write(const std::string &path, std::string_view data) {
	// 先に消してから新しく作る（O_EXCL。シンボリックリンクもたどらない）。あるものを開いて
	// 切り詰めると、置かれたリンク（シンボリックリンクでも hard link でも）の先、つまりスプールの
	// 外のファイルを壊してしまう。消すのは名前だけで、リンクの先は残る。
	(void)::unlink(path.c_str());
	int fd = openRegular(path, O_WRONLY | O_CREAT | O_EXCL, 0600).fd;
	if (fd < 0) {
		return false;
	}
	while (!data.empty()) {
		ssize_t put = ::write(fd, data.data(), data.size());
		if (put < 0) {
			if (errno == EINTR) {
				continue;
			}
			closeQuietly(fd);
			return false;
		}
		data.remove_prefix(static_cast<std::size_t>(put));
	}
	return ::close(fd) == 0;
}

bool rename(const std::string &from, const std::string &to) {
	return ::rename(from.c_str(), to.c_str()) == 0;
}

bool remove(const std::string &path) { return ::unlink(path.c_str()) == 0; }

WaitState probe(const std::string &path) {
	// 通常のファイルでない待つ印は無いものとする（実行しない側に倒す）。
	int fd = openRegular(path, O_RDONLY).fd;
	if (fd < 0) {
		return WaitState::Missing;
	}
	WaitState state = WaitState::Missing;
	if (::flock(fd, LOCK_EX | LOCK_NB) == 0) {
		state = WaitState::Released;
	} else if (errno == EWOULDBLOCK) {
		state = WaitState::Held;
	}
	// 閉じればロックも放れる。
	closeQuietly(fd);
	return state;
}

Lock::~Lock() { release(); }

Lock::Result Lock::take(const std::string &path, std::string &error) {
	release();
	Opened opened = openRegular(path, O_RDWR | O_CREAT, 0600);
	if (opened.notRegular) {
		// ロックのファイルはプラグインのもので中身を持たないので、通常のファイルでなければ消して
		// 作り直す（FIFO・リンクは、ほかの Vectorworks も掴めない）。消すのは名前だけで、リンクは
		// たどらない。
		(void)::unlink(path.c_str());
		opened = openRegular(path, O_RDWR | O_CREAT, 0600);
	}
	if (opened.fd < 0) {
		error = opened.notRegular ? "open " + path + ": 作り直しても通常のファイルになりません"
		                          : describe("open", path, opened.error);
		return Result::Failed;
	}
	int fd = opened.fd;
	if (::flock(fd, LOCK_EX | LOCK_NB) != 0) {
		int failure = errno;
		closeQuietly(fd);
		if (failure == EWOULDBLOCK) {
			return Result::Busy;
		}
		error = describe("flock", path, failure);
		return Result::Failed;
	}
	handle_ = fd;
	return Result::Taken;
}

void Lock::release() {
	if (handle_ != kNone) {
		closeQuietly(static_cast<int>(handle_));
		handle_ = kNone;
	}
}

}
