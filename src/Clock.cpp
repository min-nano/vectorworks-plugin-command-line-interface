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

// 周期処理の間隔。間隔は保証されない（最大で数秒空く）ため、時刻は時計から読み取る（Findings
// 「Timers and Notifications」の 4 の 2）。
constexpr unsigned kIntervalMs = 250;

std::int64_t nowNanos() {
	return std::chrono::duration_cast<std::chrono::nanoseconds>(
	           std::chrono::system_clock::now().time_since_epoch())
	    .count();
}

// 起動の完了を示すフラグと、終了の依頼の最中を示すフラグ。設定と解除の手順は core
// （core/Lifecycle.h）にあり、ここは SDK を呼び出すのみ。
core::Lifecycle gLifecycle;

// onNotify は通知 kNotifyBeforePendingUpdate（Vectorworks のアイドルから届く）。最初の 1 回で
// 「起動を終えた」フラグを設定するのみとする（以後も 20 ms ほどの間隔で届くため、何もしない。
// ここから終了を依頼しない。Findings「Timers and Notifications」の 4 の 9「起動を終えた目印」）。
void onNotify(StatusID id, StatusData /*data*/) {
	if (id == kNotifyBeforePendingUpdate) {
		gLifecycle.markStarted();
	}
}

// registerNotify は onNotify を登録する。plugin_module_main ではなく周期処理から呼び出す
// （Windows の Vectorworks は起動時にモジュールを一度アンロードして読み込み直すため、固定する前に
// 渡したアドレスは残らない。周期処理はモジュールを固定してから作成したタイマーから届く。
// docs/plugin/architecture.md「エントリポイント」）。登録できなければ次の周期処理で再試行する
// （その間 quit は starting で拒否し、detail に登録できていないことを示す）。受付処理の入れ子の
// 周期処理と終了の依頼の最中は呼び出さない（core::Lifecycle）。
bool registerNotify() {
	return gSDK->RegisterNotificationProcedure(&onNotify,
	                                           static_cast<OSType>(kNotifyBeforePendingUpdate));
}

// requestQuit は終了を依頼する。保存の確認を表示するか否かは図面に依存し（quit は図面が 0 枚の
// 場合のみ終了の依頼を返すため、通常は表示されない）、再起動はしない（docs/plugin/tools.md
// 「quit」）。保存の確認が開くと、取り消されるか終了するまで戻らない（Windows はその間も周期処理が
// 実行され、tick に再入する）。
void requestQuit() { gSDK->CloseAllFilesAndQuitVectorworks(true, false); }

// tick は 1 回の受付処理と、依頼されていれば終了の依頼。例外は Vectorworks へ伝播させない
// （docs/plugin/bridge.md「1 回の受付処理」）。
void tick() {
	if (gSDK == nullptr) {
		return;
	}
	try {
		// 受付処理は Vectorworks が終了するまで 1 つ。スプールの場所を導出できなければ、次の
		// 周期処理で再試行する（通知も登録しない。受付処理を行わなければ quit も呼び出されない）。
		static std::optional<core::Server> server;
		if (!server) {
			std::optional<std::string> dir = core::defaultSpoolDir();
			if (!dir) {
				return;
			}
			server.emplace(std::move(*dir));
		}
		gLifecycle.registerNotification(*server, &registerNotify);
		core::tick(*server, tools::table(), nowNanos(), gLifecycle, &requestQuit);
	} catch (...) {
		// 次の周期処理で再試行する。
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
	// 開始できた場合のみフラグを設定する。開始できなければ、次の plugin_module_main（起動時には
	// 何度も呼び出される）で再試行する。失敗を記録する先はプラグインに無い（受付処理を
	// 行っていないことは vw2026 status で判明する）。
	static bool started = false;
	if (started) {
		return;
	}
#if GS_MAC
	// 共通のモードに登録する。既定のモードのみでは、通常の操作でも十数秒から数分、レンダリング中は
	// 6 分近く周期処理が停止する（Findings「Timers and Notifications」の 4 の 7）。起動の途中の
	// 「開発元不明のプラグイン」のダイアログ（モーダル）の間も、既定のモードでは 1 度も周期処理が
	// 実行されない（Findings「Plug-in Modules」の「mac も、共通モードの刻みからなら扱える」）。
	// Vectorworks が終了するまで保持し続ける（解放しない）。
	CFRunLoopTimerRef timer = CFRunLoopTimerCreate(kCFAllocatorDefault, CFAbsoluteTimeGetCurrent(),
	                                               kIntervalMs / 1000.0, 0, 0, &onTick, nullptr);
	if (timer == nullptr) {
		return;
	}
	CFRunLoopAddTimer(CFRunLoopGetMain(), timer, kCFRunLoopCommonModes);
#else
	// タイマーより先に自身を固定する。Windows の Vectorworks は起動時にこのモジュールを読み込んで
	// kPluginModuleInit を送った後、kPluginModuleDeinit を送らずに一度アンロードし、別のアドレスへ
	// 読み込み直す。固定せずにタイマーを作成すると、既に存在しないモジュールの onTick のアドレスへ
	// WM_TIMER が配送され続け、そこへ後から読み込まれた別のモジュールのコードを実行して起動の途中で
	// 異常終了する。固定したモジュールはアンロードされず、読み込み直しも同じモジュールを返す
	// （Findings「Plug-in Modules」の
	// 「【罠】Windows の VW は起動時にモジュールを一度降ろして読み込み直す」）。固定できなければ
	// タイマーを作成しない（受付処理を行わないのみで、異常終了するより良い）。mac は
	// 読み込み直さないため固定しない（同）。固定するのは何度でも差し支えない
	// （アンロードされなくなるのみ）ため、再試行で重ねて固定しても構わない。
	constexpr DWORD kPinSelf =
	    GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS | GET_MODULE_HANDLE_EX_FLAG_PIN;
	HMODULE self = nullptr;
	if (GetModuleHandleExW(kPinSelf, reinterpret_cast<LPCWSTR>(&onTick), &self) == 0) {
		return;
	}
	// ウィンドウを持たないスレッドタイマー。WM_TIMER は Vectorworks のメッセージポンプが配送する。
	if (SetTimer(nullptr, 0, kIntervalMs, &onTick) == 0) {
		return;
	}
#endif
	started = true;
}

}
