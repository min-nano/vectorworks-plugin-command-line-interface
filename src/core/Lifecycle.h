#pragma once

// 起動の完了を示すフラグと、終了の依頼の処理中を示すフラグ（docs/plugin/architecture.md
// 「エントリポイント」「Vectorworks が処理中」）。SDK に依存しない。タイマー（src/Clock.cpp）は
// 周期処理ごとに tick を呼び出し、SDK で行うのは通知の登録と終了の呼び出し
// （CloseAllFilesAndQuitVectorworks）のみとする。フラグの設定と解除をここに配置するのは、保存の
// 確認が表示される競合（quit が図面を確認してから終了を依頼するまでに図面が開かれる）を実機の
// 確認で再現できず、無 SDK のテストでのみ検証できるため（docs/plugin/bridge.md「分割の方針」）。

#include "core/Serve.h"

#include <cstdint>
#include <span>
#include <utility>

namespace VwCli::core {

class Lifecycle {
public:
	// markStarted は「起動を終えた」フラグを設定する。最初の kNotifyBeforePendingUpdate から
	// 呼び出す（以後も通知されるが、設定したまま解除しない。docs/plugin/tools.md「quit」）。
	void markStarted() { started_ = true; }

	// markNotificationRegistered は、その通知を登録できた場合に呼び出す。
	void markNotificationRegistered() { notificationRegistered_ = true; }

	bool notificationRegistered() const { return notificationRegistered_; }

	// startup は起動の状態（ツール quit が ToolContext で参照する）。
	Startup startup() const { return Startup{started_, notificationRegistered_}; }

	// activity は受付処理に渡す状態。終了の依頼の処理中であれば Quitting。
	Activity activity() const { return quitting_ ? Activity::Quitting : Activity::Idle; }

	// quit は request を呼び出して終了を依頼する。フラグが設定されていれば（終了の呼び出しの途中に
	// 再入した周期処理。Windows は保存の確認の間も周期処理を実行する）何もせずに偽を返す（終了の
	// 依頼を重ねない）。設定されていなければ設定して request を呼び出し、戻ったら（例外が送出され
	// ても）解除して真を返す。戻ったら解除するのは、取り消された（Vectorworks が動作を継続する）か
	// 否かを判別できず、解除しないと取り消された後も拒否し続けるため（戻った後の周期処理から
	// 2 度目を依頼しても何も起きない。SDK リファレンス #313）。
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

// tick は 1 回の周期処理。受付処理を 1 回実行し（lifecycle の状態を渡す）、ツールが終了の依頼を
// 返して応答を書き込めたら、受付処理の外で request を呼び出して終了を依頼する
// （docs/plugin/architecture.md「受付処理の流れ」）。終了の依頼の処理中に再入した周期処理では、
// 受付処理が Quitting で一覧のツールを拒否するため、終了の依頼は返らない（返っても lifecycle が
// 2 度目を依頼しない）。
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
