#pragma once

// 試作: ソケットでの受け付け（prototypes/socket/README.md「手順」）。SDK に依らない。
// core::Bridge（スプールのファイル）の代わりになるもので、要求の中身の扱い（core::Server::answer）
// は変えずに handler として受ける。

#include "Socket.h"
#include "core/Files.h"

#include <cstddef>
#include <functional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::proto {

// 受け取りの行。要求を読み終え、実行する前に送る。呼ぶ側はこれを受け取っていなければ、要求が
// 実行されないと分かる。
inline constexpr std::string_view kTakenLine = "{\"taken\":true}\n";
inline constexpr std::string_view kSocketFile = "bridge.sock";

struct Reply {
	// 応答の JSON（改行を含まない）。
	std::string body;
	// 応答を送り切れたら呼ぶ（占有の印・終了の依頼を、応答が届いたときだけ反映する）。
	std::function<void()> onSent;
};

using Handler = std::function<Reply(const std::string &request)>;

class SocketServer {
public:
	explicit SocketServer(std::string dir) : dir_(std::move(dir)) {}
	~SocketServer();
	SocketServer(const SocketServer &) = delete;
	SocketServer &operator=(const SocketServer &) = delete;

	// listen はスプールを用意し、bridge.lock を掴めたら bridge.sock で待ち受ける（前の
	// Vectorworks が残したソケットは消す）。Taken のときだけ待ち受けている。
	core::files::Lock::Result listen(std::string &error);
	bool listening() const { return listener_ != sock::kNone; }

	// tick は受け付け 1 回。待っている接続をすべて受け取り、接続ごとに読めるだけ読み、要求を読み
	// 終えたものは実行し、応答を送れるだけ送る。止まって待つことはない（送り切れない応答・読み
	// 切れない要求は、次の回に続ける）。
	void tick(const Handler &handler);

	std::size_t connections() const { return connections_.size(); }

private:
	struct Connection {
		sock::Handle socket = sock::kNone;
		// 受け取った分。最初の改行までが要求。
		std::string in;
		// 相手が送る側を閉じた（EOF）。
		bool eof = false;
		// 空でなければ応答を送っている。
		std::string out;
		std::size_t sent = 0;
		std::function<void()> onSent;
	};

	// advance は接続 1 つを進める。終えた（閉じた）なら真。
	bool advance(Connection &connection, const Handler &handler);

	std::string dir_;
	core::files::Lock lock_;
	sock::Handle listener_ = sock::kNone;
	std::vector<Connection> connections_;
};

}
