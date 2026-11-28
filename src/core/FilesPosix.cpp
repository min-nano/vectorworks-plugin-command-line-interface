#include "core/Files.h"

#include <algorithm>
#include <cerrno>
#include <cstring>
#include <dirent.h>
#include <dlfcn.h>
#include <fcntl.h>
#include <filesystem>
#include <sys/file.h>
#include <sys/stat.h>
#include <unistd.h>

namespace VwCli::core::files {

namespace {

// closeQuietly は記述子を閉じる。閉じた結果は使用しない（読み取りの終了・ロックの解放のみで
// あるため）。
void closeQuietly(int fd) { (void)::close(fd); }

std::string describe(const char *what, const std::string &path, int error) {
	return std::string(what) + " " + path + ": " + std::strerror(error);
}

// スプールのファイルを開く際に必ず付与する。O_NONBLOCK は、FIFO を配置されても読み手・書き手を
// 待機して停止しないため（停止するのは Vectorworks のメインスレッドで、利用者の未保存の作業が
// 失われる）。通常のファイルの読み書きには作用しない。O_NOFOLLOW は、最後の名前がシンボリック
// リンクならば開かないため。開いてから拒否するのでは、リンクの先がデバイスならばそのドライバの
// open が先に実行される（シリアルポートの信号線が上がるなど）。スプールのファイルをリンク越しに
// 開く理由は無い。O_NOCTTY は端末を開いても制御端末にしないため。
constexpr int kOpenFlags = O_CLOEXEC | O_NONBLOCK | O_NOFOLLOW | O_NOCTTY;

// Opened は openRegular の結果。
struct Opened {
	// 開けなければ -1。
	int fd = -1;
	// 通常のファイルでない（FIFO・デバイス・シンボリックリンク。ディレクトリは読み取る場合のみで、
	// 書き込むために開くと open が EISDIR で先に失敗し、error に格納される）。
	bool notRegular = false;
	// 開けなかった理由（notRegular の場合は 0）。
	int error = 0;
};

// openRegular は通常のファイルのみを開く。
Opened openRegular(const std::string &path, int flags, mode_t mode = 0) {
	// open のフラグ（O_*）は POSIX が int と定めているため、符号付きのまま組み合わせる。
	// NOLINTNEXTLINE(bugprone-signed-bitwise)
	int fd = ::open(path.c_str(), flags | kOpenFlags, mode);
	if (fd < 0) {
		int failure = errno;
		// O_NOFOLLOW でシンボリックリンクを拒否すると ELOOP（FreeBSD 系は EMLINK）。ELOOP は親の
		// ディレクトリでリンクが循環している場合にも返るため、最後の名前がリンクの場合のみ
		// 「通常のファイルでない」とし、それ以外は実際の原因を返す。
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

std::optional<std::string> modulePathOf(const void *address) {
	// リンクは解決しない。バンドル（cli.vwlibrary）のみを他の場所へのリンクにしても、その
	// バンドルのあるフォルダ（<Plug-Ins>/cli）を指すため。呼び出し元は実行ファイルのリンクを解決
	// するが、その親の bin と <Plug-Ins>/cli が実体ならば、両側は同一のフォルダに到達する
	// （通信規則「スプールの場所」）。
	Dl_info info{};
	if (::dladdr(address, &info) == 0 || !info.dli_fname) {
		return std::nullopt;
	}
	return absolute(info.dli_fname);
}

std::optional<std::string> absolute(const std::string &path) {
	if (path.empty()) {
		return std::nullopt;
	}
	return std::filesystem::absolute(path).string();
}

bool makeDirectories(const std::string &path, std::string &error) {
	// 既に存在すれば何もしない（prepare は受付処理の度に呼ばれうるため、毎回失敗する mkdir を
	// メインスレッドで実行しない）。
	struct stat existing{};
	if (::stat(path.c_str(), &existing) == 0 && S_ISDIR(existing.st_mode)) {
		return true;
	}
	// 区切りごとに作成する。途中が既に存在すれば EEXIST で、それは差し支えない。最後に path が
	// ディレクトリか否かで成否を判定する。
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
	// 先に削除してから新たに作成する（O_EXCL。シンボリックリンクも辿らない）。既存のものを開いて
	// 切り詰めると、配置されたリンク（シンボリックリンクでも hard link でも）の先、すなわち
	// スプールの外のファイルを破壊する。削除するのは名前のみで、リンクの先は残る。
	(void)files::remove(path);
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
	// 通常のファイルでない待機標識は存在しないものとする（実行しない側に倒す）。存在しない（名前が長すぎて
	// 配置できないものも）以外の理由で開けなければ、確認できない（掃除が、待機している呼び出し元のものを削除しない
	// ように）。
	Opened opened = openRegular(path, O_RDONLY);
	if (opened.fd < 0) {
		bool missing = opened.notRegular || opened.error == ENOENT || opened.error == ENAMETOOLONG;
		return missing ? WaitState::Missing : WaitState::Unknown;
	}
	int fd = opened.fd;
	WaitState state = WaitState::Unknown;
	if (::flock(fd, LOCK_EX | LOCK_NB) == 0) {
		state = WaitState::Released;
	} else if (errno == EWOULDBLOCK) {
		state = WaitState::Held;
	}
	// 閉じればロックも解放される。
	closeQuietly(fd);
	return state;
}

Lock::Result Lock::take(const std::string &path, std::string &error) {
	release();
	Opened opened = openRegular(path, O_RDWR | O_CREAT, 0600);
	if (opened.notRegular && !recreated_) {
		// ロックのファイルはプラグインのもので内容を持たないため、通常のファイルでなければ削除して
		// 再作成する（FIFO・リンクは、他の Vectorworks も取得できない）。削除するのは名前のみで、
		// リンクは辿らない。再作成するのは 1 度のみ（Files.h）。
		recreated_ = true;
		if (::unlink(path.c_str()) != 0) {
			error = describe("unlink", path, errno);
			return Result::Failed;
		}
		opened = openRegular(path, O_RDWR | O_CREAT, 0600);
	}
	if (opened.fd < 0) {
		error = opened.notRegular ? "open " + path +
		                                ": 通常のファイルではありません（消して、Vectorworks を"
		                                "起動し直してください）"
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
