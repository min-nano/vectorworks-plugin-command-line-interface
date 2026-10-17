#include "PluginPrefix.h"

#include "Clock.h"

#include "core/Bridge.h"
#include "core/Serve.h"
#include "tools/ToolTable.h"

#include <chrono>
#include <cstdint>
#include <optional>
#include <string>

#if GS_MAC
#include <CoreFoundation/CoreFoundation.h>
#endif

namespace VwCli {

namespace {

// 刻みの間隔。間隔は保証されない（最大で数秒空く）ので、時刻は時計から読む（Findings「Timers
// and Notifications」の 4 の 2）。
constexpr unsigned kIntervalMs = 250;

std::int64_t nowNanos() {
	return std::chrono::duration_cast<std::chrono::nanoseconds>(
	           std::chrono::system_clock::now().time_since_epoch())
	    .count();
}

// tick は受け付け 1 回。例外は Vectorworks へ通さない（docs/plugin/bridge.md「受け付け 1 回」）。
void tick() {
	if (gSDK == nullptr) {
		return;
	}
	try {
		// 受け付けは Vectorworks が終わるまで 1 つ。スプールの場所を求められなければ、次の刻みで
		// やり直す。
		static std::optional<core::Server> server;
		if (!server) {
			std::optional<std::string> dir = core::defaultSpoolDir();
			if (!dir) {
				return;
			}
			server.emplace(std::move(*dir));
		}
		// 結果の quit（終了の依頼）は見ない。種類 app の道具が表に無いので立たない（段 6 で足す）。
		server->serve(tools::table(), nowNanos(), core::Activity::Idle);
	} catch (...) {
		// 次の刻みでやり直す。
		return;
	}
}

#if GS_MAC
void onTick(CFRunLoopTimerRef /*timer*/, void * /*info*/) { tick(); }
#else
void CALLBACK onTick(HWND /*hwnd*/, UINT /*message*/, UINT_PTR /*id*/, DWORD /*time*/) { tick(); }
#endif

}

void startClock() {
	static bool started = false;
	if (started) {
		return;
	}
	started = true;
#if GS_MAC
	// 共通のモードに登録する。既定のモードだけでは、ふつうの操作でも十数秒から数分、レンダリング
	// 中は 6 分近く刻みが止まる（Findings「Timers and Notifications」の 4 の 7）。Vectorworks が
	// 終わるまで持ち続ける（解放しない）。
	CFRunLoopTimerRef timer = CFRunLoopTimerCreate(kCFAllocatorDefault, CFAbsoluteTimeGetCurrent(),
	                                               kIntervalMs / 1000.0, 0, 0, &onTick, nullptr);
	if (timer != nullptr) {
		CFRunLoopAddTimer(CFRunLoopGetMain(), timer, kCFRunLoopCommonModes);
	}
#else
	// ウィンドウを持たないスレッドタイマー。WM_TIMER は Vectorworks のメッセージポンプが配る。
	SetTimer(nullptr, 0, kIntervalMs, &onTick);
#endif
}

}
