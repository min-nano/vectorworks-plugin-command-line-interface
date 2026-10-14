#pragma once

// 試作: AF_UNIX のストリームソケットの OS の呼び出し（prototypes/socket/README.md）。
// SDK に依らない。実装は OS ごとのファイル（SocketPosix.cpp・SocketWindows.cpp）。
//
// ソケットはすべてノンブロッキングで、子プロセスに受け継がせない（POSIX は FD_CLOEXEC、Windows は
// 継承できないハンドル）。相手が閉じたソケットへ送っても SIGPIPE を出さない（Vectorworks ごと
// 落ちないように。mac は SO_NOSIGPIPE、Linux は MSG_NOSIGNAL）。

#include <cstddef>
#include <cstdint>
#include <string>

namespace VwCli::proto::sock {

// POSIX はファイル記述子、Windows は SOCKET（INVALID_SOCKET は -1）。
using Handle = std::intptr_t;
inline constexpr Handle kNone = -1;

// maxPathBytes はソケットのパスに使える最大のバイト数（sun_path の大きさから終端の分を除く）。
std::size_t maxPathBytes();

// listenAt は path にソケットを作って待ち受ける。path にあるもの（前の Vectorworks が残した
// ソケット）は先に消す。消してよいかは呼ぶ側が bridge.lock で決める。失敗なら kNone で、error に
// 理由を書く。
Handle listenAt(const std::string &path, std::string &error);

// acceptOne は待ち受けているソケットから 1 つ受け取る。無ければ kNone。
Handle acceptOne(Handle listener);

// Io は送受信 1 回の結果。
struct Io {
	enum class Status {
		// count バイトを送った・受け取った
		Done,
		// 今はできない（次の回にやり直す）
		WouldBlock,
		// 相手が閉じた（受け取りでは EOF。送りでは相手が居ない）・そのほかの失敗
		Closed,
	};
	Status status;
	std::size_t count = 0;
};

Io receive(Handle socket, char *buffer, std::size_t size);
Io send(Handle socket, const char *data, std::size_t size);

void close(Handle socket);

// 以下はテストが呼ぶ側を作るためだけに使う（プラグインは呼ばない）。

// connectTo は path のソケットへつなぐ（つなぐまでは待つ）。つないだあとはノンブロッキング。
Handle connectTo(const std::string &path);

// shutdownSend は送る側だけを閉じる（相手は EOF を受け取る）。
void shutdownSend(Handle socket);

}
