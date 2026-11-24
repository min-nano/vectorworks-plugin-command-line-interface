#include "PluginPrefix.h"

#include "Clock.h"

#include "core/Bridge.h"
#include "core/Lifecycle.h"
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

// 起動を終えた印と終了の依頼の最中の印。立て下ろしの手順は core（core/Lifecycle.h）で、ここは
// SDK を呼ぶだけ。
core::Lifecycle gLifecycle;

// onNotify は通知 kNotifyBeforePendingUpdate（Vectorworks のアイドルから届く）。最初の 1 回で
// 「起動を終えた」印を立てるだけにする（以後も 20 ms ほどの間隔で届くので、何もしない。ここから
// 終了を頼まない。Findings「Timers and Notifications」の 4 の 9「起動を終えた目印」）。
void onNotify(StatusID id, StatusData /*data*/) {
	if (id == kNotifyBeforePendingUpdate) {
		gLifecycle.markStarted();
	}
}

// registerNotify は onNotify を登録する。plugin_module_main ではなく刻みから呼ぶ（Windows の
// Vectorworks は起動時にモジュールを一度降ろして読み込み直すので、留める前に渡した番地は残らない。
// 刻みはモジュールを留めてから作ったタイマーから来る。docs/plugin/architecture.md「入口」）。
// 登録できなければ次の刻みでやり直す（その間 quit は starting で断り、detail に登録できて
// いないことを示す）。
void registerNotify() {
	if (gLifecycle.notificationRegistered()) {
		return;
	}
	if (gSDK->RegisterNotificationProcedure(&onNotify,
	                                        static_cast<OSType>(kNotifyBeforePendingUpdate))) {
		gLifecycle.markNotificationRegistered();
	}
}

// requestQuit は終了を頼む。保存の確認を出すかは図面しだい（quit は図面が 0 枚のときだけ終了の
// 依頼を返すので、ふつうは出ない）で、再起動はしない（docs/plugin/tools.md「quit」）。保存の確認が
// 開くと、取り消されるか終えるまで戻らない（Windows はその間も刻み、tick に入り直す）。
void requestQuit() { gSDK->CloseAllFilesAndQuitVectorworks(true, false); }

// tick は受け付け 1 回と、頼まれていれば終了の依頼。例外は Vectorworks へ通さない
// （docs/plugin/bridge.md「受け付け 1 回」）。
void tick() {
	if (gSDK == nullptr) {
		return;
	}
	try {
		registerNotify();
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
		core::tick(*server, tools::table(), nowNanos(), gLifecycle, &requestQuit);
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
	// 始められたときだけ立てる。始められなければ、次の plugin_module_main（起動では何度も呼ばれる）
	// でやり直す。失敗を書き残す先はプラグインに無い（受け付けないことは vw2026 status で分かる）。
	static bool started = false;
	if (started) {
		return;
	}
#if GS_MAC
	// 共通のモードに登録する。既定のモードだけでは、ふつうの操作でも十数秒から数分、レンダリング
	// 中は 6 分近く刻みが止まる（Findings「Timers and Notifications」の 4 の 7）。起動の途中の
	// 「開発元不明のプラグイン」のダイアログ（モーダル）の間も、既定のモードでは 1 度も刻まない
	// （Findings「Plug-in Modules」の「mac も、共通モードの刻みからなら扱える」）。Vectorworks が
	// 終わるまで持ち続ける（解放しない）。
	CFRunLoopTimerRef timer = CFRunLoopTimerCreate(kCFAllocatorDefault, CFAbsoluteTimeGetCurrent(),
	                                               kIntervalMs / 1000.0, 0, 0, &onTick, nullptr);
	if (timer == nullptr) {
		return;
	}
	CFRunLoopAddTimer(CFRunLoopGetMain(), timer, kCFRunLoopCommonModes);
#else
	// タイマーより先に自分を留める。Windows の Vectorworks は起動時にこのモジュールを読み込んで
	// kPluginModuleInit を送ったあと、kPluginModuleDeinit を送らずに一度降ろし、別の番地へ読み込み
	// 直す。留めずにタイマーを作ると、もう無いモジュールの onTick の番地へ WM_TIMER が配られ続け、
	// そこへ後から読み込まれた別のモジュールのコードを踏んで起動の途中で落ちる。留めたモジュールは
	// 降ろされず、読み込み直しも同じモジュールを返す（Findings「Plug-in Modules」の「【罠】Windows
	// の VW は起動時にモジュールを一度降ろして読み込み直す」）。留められなければタイマーを作らない
	// （受け付けないだけで、落ちるよりよい）。mac は読み込み直さないので留めない（同）。留めるのは
	// 何度でもよい（降ろされなくなるだけ）ので、やり直しで重ねて留めても構わない。
	constexpr DWORD kPinSelf =
	    GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_PIN;
	HMODULE self = nullptr;
	if (GetModuleHandleExW(kPinSelf, reinterpret_cast<LPCWSTR>(&onTick), &self) == 0) {
		return;
	}
	// ウィンドウを持たないスレッドタイマー。WM_TIMER は Vectorworks のメッセージポンプが配る。
	if (SetTimer(nullptr, 0, kIntervalMs, &onTick) == 0) {
		return;
	}
#endif
	started = true;
}

}
