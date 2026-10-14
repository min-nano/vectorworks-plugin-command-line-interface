#include "Socket.h"

#include <cerrno>
#include <cstring>

#include <fcntl.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

namespace VwCli::proto::sock {

namespace {

// Linux には SO_NOSIGPIPE が無いので、送るたびに MSG_NOSIGNAL を付ける。
#ifdef MSG_NOSIGNAL
constexpr int kSendFlags = MSG_NOSIGNAL;
#else
constexpr int kSendFlags = 0;
#endif

// prepare はノンブロッキングにし、子プロセスに受け継がせないようにする。mac には socket() と
// accept() で FD_CLOEXEC を付ける指定が無いので、作ったあとに付ける（その間にほかのスレッドが
// fork すると子に渡る）。
bool prepare(int fd) {
	int flags = ::fcntl(fd, F_GETFL);
	if (flags < 0 || ::fcntl(fd, F_SETFL, flags | O_NONBLOCK) != 0 ||
	    ::fcntl(fd, F_SETFD, FD_CLOEXEC) != 0) {
		return false;
	}
#ifdef SO_NOSIGPIPE
	int on = 1;
	if (::setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &on, sizeof on) != 0) {
		return false;
	}
#endif
	return true;
}

bool address(const std::string &path, sockaddr_un &out) {
	std::memset(&out, 0, sizeof out);
	out.sun_family = AF_UNIX;
	if (path.empty() || path.size() > maxPathBytes()) {
		return false;
	}
	std::memcpy(out.sun_path, path.data(), path.size());
	return true;
}

std::string describe(const char *what, const std::string &path, int error) {
	return std::string(what) + " " + path + ": " + std::strerror(error);
}

}

std::size_t maxPathBytes() { return sizeof(sockaddr_un::sun_path) - 1; }

Handle listenAt(const std::string &path, std::string &error) {
	sockaddr_un addr{};
	if (!address(path, addr)) {
		error = "socket path is too long (" + std::to_string(path.size()) + " bytes, limit " +
		        std::to_string(maxPathBytes()) + "): " + path;
		return kNone;
	}
	(void)::unlink(path.c_str());
	int fd = ::socket(AF_UNIX, SOCK_STREAM, 0);
	if (fd < 0) {
		error = describe("socket", path, errno);
		return kNone;
	}
	if (!prepare(fd) || ::bind(fd, reinterpret_cast<const sockaddr *>(&addr), sizeof addr) != 0 ||
	    ::listen(fd, SOMAXCONN) != 0) {
		error = describe("listen", path, errno);
		::close(fd);
		return kNone;
	}
	return fd;
}

Handle acceptOne(Handle listener) {
	for (;;) {
		int fd = ::accept(static_cast<int>(listener), nullptr, nullptr);
		if (fd < 0) {
			if (errno == EINTR) {
				continue;
			}
			return kNone;
		}
		if (!prepare(fd)) {
			::close(fd);
			continue;
		}
		return fd;
	}
}

Io receive(Handle socket, char *buffer, std::size_t size) {
	for (;;) {
		ssize_t got = ::recv(static_cast<int>(socket), buffer, size, 0);
		if (got > 0) {
			return {Io::Status::Done, static_cast<std::size_t>(got)};
		}
		if (got < 0 && errno == EINTR) {
			continue;
		}
		if (got < 0 && (errno == EAGAIN || errno == EWOULDBLOCK)) {
			return {Io::Status::WouldBlock};
		}
		return {Io::Status::Closed};
	}
}

Io send(Handle socket, const char *data, std::size_t size) {
	for (;;) {
		ssize_t put = ::send(static_cast<int>(socket), data, size, kSendFlags);
		if (put >= 0) {
			return {Io::Status::Done, static_cast<std::size_t>(put)};
		}
		if (errno == EINTR) {
			continue;
		}
		if (errno == EAGAIN || errno == EWOULDBLOCK) {
			return {Io::Status::WouldBlock};
		}
		return {Io::Status::Closed};
	}
}

void close(Handle socket) {
	if (socket != kNone) {
		::close(static_cast<int>(socket));
	}
}

Handle connectTo(const std::string &path) {
	sockaddr_un addr{};
	if (!address(path, addr)) {
		return kNone;
	}
	int fd = ::socket(AF_UNIX, SOCK_STREAM, 0);
	if (fd < 0) {
		return kNone;
	}
	if (::connect(fd, reinterpret_cast<const sockaddr *>(&addr), sizeof addr) != 0 ||
	    !prepare(fd)) {
		::close(fd);
		return kNone;
	}
	return fd;
}

void shutdownSend(Handle socket) { ::shutdown(static_cast<int>(socket), SHUT_WR); }

}
