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

} // namespace

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
	int fd = ::open(path.c_str(), O_RDONLY | O_CLOEXEC);
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
	int fd = ::open(path.c_str(), O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0600);
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
	int fd = ::open(path.c_str(), O_RDONLY | O_CLOEXEC);
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
	int fd = ::open(path.c_str(), O_RDWR | O_CREAT | O_CLOEXEC, 0600);
	if (fd < 0) {
		error = describe("open", path, errno);
		return Result::Failed;
	}
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

} // namespace VwCli::core::files
