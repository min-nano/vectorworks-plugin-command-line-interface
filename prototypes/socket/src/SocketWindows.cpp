#include "Socket.h"

#ifndef NOMINMAX
#define NOMINMAX
#endif
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>

#include <afunix.h>
#include <windows.h>

#include <algorithm>
#include <cstdio>
#include <cstring>
#include <string>

namespace VwCli::proto::sock {

namespace {

// startup は Winsock を 1 度だけ初期化する（終了まで WSACleanup しない）。
bool startup() {
	static const bool ready = [] {
		WSADATA data{};
		return ::WSAStartup(MAKEWORD(2, 2), &data) == 0;
	}();
	return ready;
}

std::wstring widen(const std::string &text) {
	if (text.empty()) {
		return {};
	}
	int size = static_cast<int>(text.size());
	int length = ::MultiByteToWideChar(CP_UTF8, 0, text.data(), size, nullptr, 0);
	std::wstring wide(static_cast<std::size_t>(length), L'\0');
	::MultiByteToWideChar(CP_UTF8, 0, text.data(), size, wide.data(), length);
	return wide;
}

// prepare はノンブロッキングにし、子プロセスに受け継がせないようにする。
bool prepare(SOCKET socket) {
	u_long on = 1;
	return ::ioctlsocket(socket, FIONBIO, &on) == 0 &&
	       ::SetHandleInformation(reinterpret_cast<HANDLE>(socket), HANDLE_FLAG_INHERIT, 0);
}

// address は sun_path に UTF-8 のまま入れる（Windows がどの文字コードで読むかは試作で確かめる。
// prototypes/socket/README.md）。
bool address(const std::string &path, sockaddr_un &out) {
	std::memset(&out, 0, sizeof out);
	out.sun_family = AF_UNIX;
	if (path.empty() || path.size() > maxPathBytes()) {
		return false;
	}
	std::memcpy(out.sun_path, path.data(), path.size());
	return true;
}

SOCKET open() {
	return ::WSASocketW(AF_UNIX, SOCK_STREAM, 0, nullptr, 0, WSA_FLAG_NO_HANDLE_INHERIT);
}

std::string describe(const char *what, const std::string &path, int error) {
	return std::string(what) + " " + path + ": error " + std::to_string(error);
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
	if (!startup()) {
		error = "WSAStartup failed";
		return kNone;
	}
	(void)::DeleteFileW(widen(path).c_str());
	SOCKET socket = open();
	if (socket == INVALID_SOCKET) {
		error = describe("socket", path, ::WSAGetLastError());
		return kNone;
	}
	if (!prepare(socket) ||
	    ::bind(socket, reinterpret_cast<const sockaddr *>(&addr), sizeof addr) != 0 ||
	    ::listen(socket, SOMAXCONN) != 0) {
		error = describe("listen", path, ::WSAGetLastError());
		::closesocket(socket);
		return kNone;
	}
	return static_cast<Handle>(socket);
}

Handle acceptOne(Handle listener) {
	for (;;) {
		SOCKET socket = ::accept(static_cast<SOCKET>(listener), nullptr, nullptr);
		if (socket == INVALID_SOCKET) {
			return kNone;
		}
		if (!prepare(socket)) {
			::closesocket(socket);
			continue;
		}
		return static_cast<Handle>(socket);
	}
}

Io receive(Handle socket, char *buffer, std::size_t size) {
	int want = static_cast<int>(std::min<std::size_t>(size, 1 << 30));
	int got = ::recv(static_cast<SOCKET>(socket), buffer, want, 0);
	if (got > 0) {
		return {Io::Status::Done, static_cast<std::size_t>(got)};
	}
	if (got < 0 && ::WSAGetLastError() == WSAEWOULDBLOCK) {
		return {Io::Status::WouldBlock};
	}
	return {Io::Status::Closed};
}

Io send(Handle socket, const char *data, std::size_t size) {
	int want = static_cast<int>(std::min<std::size_t>(size, 1 << 30));
	int put = ::send(static_cast<SOCKET>(socket), data, want, 0);
	if (put >= 0) {
		return {Io::Status::Done, static_cast<std::size_t>(put)};
	}
	if (::WSAGetLastError() == WSAEWOULDBLOCK) {
		return {Io::Status::WouldBlock};
	}
	return {Io::Status::Closed};
}

void close(Handle socket) {
	if (socket != kNone) {
		::closesocket(static_cast<SOCKET>(socket));
	}
}

Handle connectTo(const std::string &path) {
	sockaddr_un addr{};
	if (!address(path, addr) || !startup()) {
		return kNone;
	}
	SOCKET socket = open();
	if (socket == INVALID_SOCKET) {
		return kNone;
	}
	// 待ち受けていない相手で止まらないよう、ノンブロッキングでつなぎ、2 秒まで待つ。
	if (!prepare(socket)) {
		::closesocket(socket);
		return kNone;
	}
	if (::connect(socket, reinterpret_cast<const sockaddr *>(&addr), sizeof addr) != 0) {
		int error = ::WSAGetLastError();
		fd_set writable;
		fd_set failed;
		FD_ZERO(&writable);
		FD_ZERO(&failed);
		FD_SET(socket, &writable);
		FD_SET(socket, &failed);
		timeval wait{2, 0};
		if (error != WSAEWOULDBLOCK || ::select(0, nullptr, &writable, &failed, &wait) != 1 ||
		    !FD_ISSET(socket, &writable)) {
			std::printf("connect %s: error %d\n", path.c_str(), error);
			std::fflush(stdout);
			::closesocket(socket);
			return kNone;
		}
	}
	return static_cast<Handle>(socket);
}

void shutdownSend(Handle socket) { ::shutdown(static_cast<SOCKET>(socket), SD_SEND); }

}
