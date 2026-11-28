#pragma once

// 起動を終えた印と、終了の依頼の最中の印（docs/plugin/architecture.md「エントリポイント」「Vectorworks が
// 処理中」）。SDK に依らない。タイマー（src/Clock.cpp）は刻みごとに tick を呼び、SDK で行うのは
// 通知の登録と終了の呼び出し（CloseAllFilesAndQuitVectorworks）だけにする。印の立て下ろしをここに
// 置くのは、保存の確認が開く競合（quit が図面を確かめてから終了を頼むまでに図面が開く）を実機の
// 確認で起こせず、無 SDK のテストでしか確かめられないため（docs/plugin/bridge.md「分割の方針」）。

#include "core/Serve.h"

#include <cstdint>
#include <span>
#include <utility>

namespace VwCli::core {

class Lifecycle {
public:
	// markStarted は「起動を終えた」印を立てる。最初の kNotifyBeforePendingUpdate から呼ぶ（以後も
	// 届くが、立ったままで下ろさない。docs/plugin/tools.md「quit」）。
	void markStarted() { started_ = true; }

	// markNotificationRegistered は、その通知を登録できたときに呼ぶ。
	void markNotificationRegistered() { notificationRegistered_ = true; }

	bool notificationRegistered() const { return notificationRegistered_; }

	// startup は起動の状態（道具 quit が ToolContext で見る）。
	Startup startup() const { return Startup{started_, notificationRegistered_}; }

	// activity は受け付けに渡す状態。終了の依頼の最中なら Quitting。
	Activity activity() const { return quitting_ ? Activity::Quitting : Activity::Idle; }

	// quit は request を呼んで終了を頼む。印が立っていれば（終了の呼び出しの途中に入り直した刻み。
	// Windows は保存の確認の間も刻む）何もせずに偽を返す（終了の依頼を重ねない）。立っていなければ
	// 立てて request を呼び、戻ったら（投げても）下ろして真を返す。戻ったら下ろすのは、取り消された
	// （Vectorworks が動き続ける）かを見分けられず、下ろさないと取り消した後も断り続けるため
	// （戻った後の刻みから 2 度目を頼んでも何も起きない。SDK リファレンス #313）。
	template <typename Request> bool quit(Request &&request) {
		if (quitting_) {
			return false;
		}
		ScopedFlag raised(quitting_);
		std::forward<Request>(request)();
		return true;
	}

private:
	bool started_ = false;
	bool notificationRegistered_ = false;
	bool quitting_ = false;
};

// tick は刻み 1 回。受け付けを 1 回走らせ（lifecycle の状態を渡す）、道具が終了の依頼を返して
// 応答を書けたら、受け付けの外で request を呼んで終了を頼む（docs/plugin/architecture.md
// 「受付処理の流れ」）。終了の依頼の最中に入り直した刻みでは、受け付けが Quitting で表の道具を
// 断るので、終了の依頼は返らない（返っても lifecycle が 2 度目を頼まない）。
template <typename Request>
ServeResult tick(Server &server, std::span<const Tool> tools, std::int64_t nowNanos,
                 Lifecycle &lifecycle, Request &&request) {
	ServeResult result = server.serve(tools, nowNanos, lifecycle.activity(), lifecycle.startup());
	if (result.quit) {
		(void)lifecycle.quit(std::forward<Request>(request));
	}
	return result;
}

}
