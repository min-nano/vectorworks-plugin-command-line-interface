#include "SocketServe.h"

#include "core/Protocol.h"

#include <algorithm>

namespace VwCli::proto {

namespace {

namespace files = core::files;

constexpr std::string_view kNoWait =
    R"({"ok":false,"code":"no_wait","error":"呼ぶ側が待つのをやめたので実行しませんでした"})";
constexpr std::string_view kTooLarge =
    R"({"ok":false,"code":"invalid_request","error":"要求が大きすぎるか、改行で終わっていません"})";

}

SocketServer::~SocketServer() {
	for (Connection &connection : connections_) {
		sock::close(connection.socket);
	}
	sock::close(listener_);
}

files::Lock::Result SocketServer::listen(std::string &error) {
	if (!files::makeDirectories(dir_, error)) {
		return files::Lock::Result::Failed;
	}
	files::Lock::Result result = lock_.take(files::join(dir_, core::protocol::kLockFile), error);
	if (result != files::Lock::Result::Taken) {
		return result;
	}
	listener_ = sock::listenAt(files::join(dir_, kSocketFile), error);
	if (listener_ == sock::kNone) {
		lock_.release();
		return files::Lock::Result::Failed;
	}
	return result;
}

void SocketServer::tick(const Handler &handler) {
	if (!listening()) {
		return;
	}
	// 受け取った順（つないだ順）に処理する。
	for (sock::Handle socket = sock::acceptOne(listener_); socket != sock::kNone;
	     socket = sock::acceptOne(listener_)) {
		Connection connection;
		connection.socket = socket;
		connections_.push_back(std::move(connection));
	}
	std::erase_if(connections_,
	              [&](Connection &connection) { return advance(connection, handler); });
}

bool SocketServer::advance(Connection &connection, const Handler &handler) {
	auto finish = [&] {
		sock::close(connection.socket);
		return true;
	};
	auto startReply = [&](std::string_view body) { connection.out = std::string(body) + "\n"; };

	if (connection.out.empty()) {
		// 読めるだけ読む。上限を超えたら、それ以上は読まない（大きすぎることだけ分かればよい）。
		char buffer[65536];
		while (!connection.eof && connection.in.size() <= core::protocol::kMaxRequestBytes) {
			sock::Io io = sock::receive(connection.socket, buffer, sizeof buffer);
			if (io.status == sock::Io::Status::WouldBlock) {
				break;
			}
			if (io.status == sock::Io::Status::Closed) {
				connection.eof = true;
				break;
			}
			connection.in.append(buffer, io.count);
		}
		std::size_t newline = connection.in.find('\n');
		if (newline == std::string::npos) {
			if (connection.in.size() > core::protocol::kMaxRequestBytes) {
				startReply(kTooLarge);
			} else if (connection.eof) {
				// 要求を送り切らずに閉じた。
				return finish();
			} else {
				return false;
			}
		} else if (connection.in.size() > newline + 1) {
			// 要求の後ろに何かある: 受け取りの行を送る前に、呼ぶ側が待つのをやめた。
			startReply(kNoWait);
		} else {
			// 受け取りの行を送る。送れなければ呼ぶ側は居ない（死んだ・閉じた）ので実行しない。
			sock::Io taken = sock::send(connection.socket, kTakenLine.data(), kTakenLine.size());
			if (taken.status != sock::Io::Status::Done || taken.count != kTakenLine.size()) {
				return finish();
			}
			// 送った「あと」に、やめたかを見る。呼ぶ側は待つのをやめるとき 1 バイト送ってから
			// 読むので、ここで何も無ければ、呼ぶ側は受け取りの行を必ず読む。
			char extra = 0;
			if (sock::receive(connection.socket, &extra, 1).status == sock::Io::Status::Done) {
				startReply(kNoWait);
			} else {
				Reply reply = handler(connection.in.substr(0, newline));
				startReply(reply.body);
				connection.onSent = std::move(reply.onSent);
			}
		}
	}

	while (connection.sent < connection.out.size()) {
		sock::Io io = sock::send(connection.socket, connection.out.data() + connection.sent,
		                         connection.out.size() - connection.sent);
		if (io.status == sock::Io::Status::WouldBlock) {
			return false;
		}
		if (io.status == sock::Io::Status::Closed) {
			// 送り切る前に呼ぶ側が居なくなった。応答が届いていないので反映しない。
			return finish();
		}
		connection.sent += io.count;
	}
	if (connection.onSent) {
		connection.onSent();
	}
	return finish();
}

}
