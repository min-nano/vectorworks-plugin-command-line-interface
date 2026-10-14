// 試作: ソケットでの受け付けの前提を確かめる（prototypes/socket/README.md「確かめること」）。
// 呼ぶ側もこのテストの中で作り、受け付け（tick）と交互に進める（スレッドを使わない）。

#include "TestSupport.h"

#include "Socket.h"
#include "SocketServe.h"
#include "core/Bridge.h"

#include <chrono>
#include <cstdio>
#include <filesystem>
#include <string>
#include <thread>
#include <vector>

using VwCli::proto::Handler;
using VwCli::proto::kSocketFile;
using VwCli::proto::kTakenLine;
using VwCli::proto::Reply;
using VwCli::proto::SocketServer;
using VwCli::test::TempDir;
namespace sock = VwCli::proto::sock;
namespace files = VwCli::core::files;

namespace {

// Client はテストの中の呼ぶ側。
class Client {
public:
	explicit Client(const std::string &path) : socket_(sock::connectTo(path)) {}
	~Client() { close(); }
	Client(const Client &) = delete;
	Client &operator=(const Client &) = delete;

	bool connected() const { return socket_ != sock::kNone; }

	// put は送れるだけ送り、送った分を返す（ノンブロッキング）。
	std::size_t put(std::string_view data) {
		std::size_t total = 0;
		while (total < data.size()) {
			sock::Io io = sock::send(socket_, data.data() + total, data.size() - total);
			if (io.status != sock::Io::Status::Done) {
				break;
			}
			total += io.count;
		}
		return total;
	}

	// drain は今読めるだけ読み、got に足す。相手が閉じたら closed を立てる。
	void drain() {
		char buffer[65536];
		for (;;) {
			sock::Io io = sock::receive(socket_, buffer, sizeof buffer);
			if (io.status == sock::Io::Status::Done) {
				got.append(buffer, io.count);
				continue;
			}
			closed = io.status == sock::Io::Status::Closed;
			return;
		}
	}

	void shutdownSend() { sock::shutdownSend(socket_); }

	void close() {
		sock::close(socket_);
		socket_ = sock::kNone;
	}

	std::string got;
	bool closed = false;

private:
	sock::Handle socket_;
};

// Calls は handler が呼ばれた要求を記録する。
struct Calls {
	std::vector<std::string> requests;
	int sent = 0;

	Handler handler(std::string body = R"({"ok":true})") {
		return [this, body](const std::string &request) {
			requests.push_back(request);
			return Reply{body, [this] { ++sent; }};
		};
	}
};

// settle は tick を何度か回し、呼ぶ側が読めるものを読む（OS がつなぐのを待つ余裕を持たせる）。
void settle(SocketServer &server, const Handler &handler, std::vector<Client *> clients,
            int ticks = 20) {
	for (int i = 0; i < ticks; ++i) {
		server.tick(handler);
		for (Client *client : clients) {
			client->drain();
		}
		std::this_thread::sleep_for(std::chrono::milliseconds(5));
	}
}

std::string socketPath(const TempDir &dir) { return dir.sub(std::string(kSocketFile)); }

void listen(SocketServer &server) {
	std::string error;
	CHECK(server.listen(error) == files::Lock::Result::Taken);
	if (!error.empty()) {
		std::printf("listen: %s\n", error.c_str());
	}
}

}

TEST(SocketRoundTrip) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("{\"tool\":\"ping\"}\n");
	Calls calls;
	settle(server, calls.handler(), {&client});
	CHECK_EQ(calls.requests, std::vector<std::string>{"{\"tool\":\"ping\"}"});
	CHECK_EQ(calls.sent, 1);
	CHECK_EQ(client.got, std::string(kTakenLine) + "{\"ok\":true}\n");
	CHECK(client.closed);
	CHECK_EQ(server.connections(), std::size_t{0});
}

// 要求が数回に分かれて届いても、改行までそろってから実行する。
TEST(SocketPartialRequestWaitsForNewline) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	Calls calls;
	client.put("{\"tool\":");
	settle(server, calls.handler(), {&client}, 5);
	CHECK(calls.requests.empty());
	CHECK_EQ(server.connections(), std::size_t{1});
	client.put("\"ping\"}\n");
	settle(server, calls.handler(), {&client});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
}

// 呼ぶ側が要求を置いたあとに死んだ（閉じた）なら、実行しない（作法 5 の待つ印の代わり）。
TEST(SocketClosedClientIsNotRun) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Calls calls;
	{
		Client client(socketPath(dir));
		REQUIRE(client.connected());
		CHECK_EQ(client.put("{\"tool\":\"ping\"}\n"), std::size_t{16});
	}
	settle(server, calls.handler(), {});
	CHECK(calls.requests.empty());
	CHECK_EQ(server.connections(), std::size_t{0});
}

// 受け取りの行より前に待つのをやめた（1 バイト送った）なら、実行せず no_wait で応える。
TEST(SocketCancelBeforeTakenIsNotRun) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("{\"tool\":\"ping\"}\nc");
	Calls calls;
	settle(server, calls.handler(), {&client});
	CHECK(calls.requests.empty());
	CHECK(client.got.find("taken") == std::string::npos);
	CHECK(client.got.find("\"no_wait\"") != std::string::npos);
}

// 送る側だけを閉じた呼ぶ側（nc -U など）は待っているので、実行する。
TEST(SocketHalfClosedClientIsRun) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("{\"tool\":\"ping\"}\n");
	client.shutdownSend();
	Calls calls;
	settle(server, calls.handler(), {&client});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
	CHECK_EQ(client.got, std::string(kTakenLine) + "{\"ok\":true}\n");
}

// 1 回の受け付けで、つないだ順に実行する。
TEST(SocketRunsInConnectOrder) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client a(socketPath(dir));
	Client b(socketPath(dir));
	Client c(socketPath(dir));
	REQUIRE(a.connected() && b.connected() && c.connected());
	a.put("a\n");
	b.put("b\n");
	c.put("c\n");
	Calls calls;
	server.tick(calls.handler());
	CHECK_EQ(calls.requests, (std::vector<std::string>{"a", "b", "c"}));
}

// 大きな応答は 1 回の受け付けで送り切れない。止まらずに戻り、次の回に続ける。送り切ってから
// onSent を呼ぶ。
TEST(SocketLargeResponseSpansTicks) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("{\"tool\":\"big\"}\n");
	std::string body = "{\"ok\":true,\"result\":\"" + std::string(8 << 20, 'x') + "\"}";
	Calls calls;
	Handler handler = calls.handler(body);
	auto started = std::chrono::steady_clock::now();
	server.tick(handler);
	auto first = std::chrono::steady_clock::now() - started;
	CHECK_EQ(calls.requests.size(), std::size_t{1});
	CHECK_EQ(calls.sent, 0);
	CHECK_EQ(server.connections(), std::size_t{1});
	CHECK(first < std::chrono::seconds(1));
	int ticks = 1;
	while (!client.closed && ticks < 100000) {
		client.drain();
		server.tick(handler);
		++ticks;
	}
	CHECK_EQ(client.got.size(), kTakenLine.size() + body.size() + 1);
	CHECK_EQ(calls.sent, 1);
	std::printf("8 MiB response took %d ticks\n", ticks);
}

// 応答を送り切る前に呼ぶ側が居なくなったら、onSent を呼ばない（占有などを反映しない）。
TEST(SocketClientLeavingMidResponseIsNotCommitted) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Calls calls;
	Handler handler =
	    calls.handler("{\"ok\":true,\"result\":\"" + std::string(8 << 20, 'x') + "\"}");
	{
		Client client(socketPath(dir));
		REQUIRE(client.connected());
		client.put("{\"tool\":\"big\"}\n");
		server.tick(handler);
		client.drain();
	}
	settle(server, handler, {});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
	CHECK_EQ(calls.sent, 0);
	CHECK_EQ(server.connections(), std::size_t{0});
}

// 上限を超えても改行が来ない要求は、実行せずに invalid_request で応える。
TEST(SocketTooLargeRequest) {
	TempDir dir;
	SocketServer server(dir.utf8Path());
	listen(server);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	std::string data((1 << 20) + 2, 'x');
	Calls calls;
	std::size_t sent = 0;
	for (int i = 0; i < 100000 && sent < data.size(); ++i) {
		sent += client.put(std::string_view(data).substr(sent));
		server.tick(calls.handler());
	}
	settle(server, calls.handler(), {&client});
	CHECK(calls.requests.empty());
	CHECK(client.got.find("\"invalid_request\"") != std::string::npos);
}

// 前の Vectorworks が残したソケット（異常終了など）は、次にロックを取った側が消して作り直す。
TEST(SocketStaleSocketIsReplaced) {
	TempDir dir;
	{
		SocketServer first(dir.utf8Path());
		listen(first);
	}
	CHECK(std::filesystem::exists(dir.path() / std::string(kSocketFile)));
	{
		// 残ったソケットへつないでも、待ち受けている者は居ない。
		Client client(socketPath(dir));
		CHECK(!client.connected());
	}
	SocketServer second(dir.utf8Path());
	listen(second);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("x\n");
	Calls calls;
	settle(second, calls.handler(), {&client});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
}

// ロックを取れなかった側は、受け付けている側のソケットに触らない。
TEST(SocketSecondServerLeavesSocketAlone) {
	TempDir dir;
	SocketServer first(dir.utf8Path());
	listen(first);
	SocketServer second(dir.utf8Path());
	std::string error;
	CHECK(second.listen(error) == files::Lock::Result::Busy);
	Client client(socketPath(dir));
	REQUIRE(client.connected());
	client.put("x\n");
	Calls calls;
	settle(first, calls.handler(), {&client});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
}

// パスに ASCII でない文字があっても、その名前のソケットができ、つなげる（Windows が sun_path を
// UTF-8 で読むか）。
TEST(SocketNonAsciiPath) {
	TempDir dir;
	std::filesystem::path sub = dir.path() / std::filesystem::path(u8"日");
	std::string subUtf8 = VwCli::test::utf8(sub);
	SocketServer server(subUtf8);
	listen(server);
	CHECK(std::filesystem::exists(sub / std::string(kSocketFile)));
	Client client(files::join(subUtf8, kSocketFile));
	REQUIRE(client.connected());
	client.put("x\n");
	Calls calls;
	settle(server, calls.handler(), {&client});
	CHECK_EQ(calls.requests.size(), std::size_t{1});
}

// パスの長さの上限。CI のログに、この計算機のスプールでの余裕を出す。
TEST(SocketPathLimit) {
	TempDir dir;
	std::string longDir = dir.sub(std::string(sock::maxPathBytes(), 'a'));
	SocketServer server(longDir);
	std::string error;
	CHECK(server.listen(error) == files::Lock::Result::Failed);
	CHECK(error.find("too long") != std::string::npos);
	if (std::optional<std::string> spool = VwCli::core::defaultSpoolDir()) {
		std::string path = files::join(*spool, kSocketFile);
		std::printf("socket path limit %zu bytes; this machine's spool socket is %zu bytes: %s\n",
		            sock::maxPathBytes(), path.size(), path.c_str());
	}
}
