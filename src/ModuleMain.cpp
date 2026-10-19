// プラグインの入口（docs/plugin/architecture.md「入口」）。拡張機能は登録せず、VCOM を初期化して
// OS タイマーを始めるだけ（docs/plugin/architecture.md「拡張機能を登録しない」）。
//
// SDK の静的ライブラリをリンクするモジュールが定義するもの（plugin_module_ver と
// DefaultPluginVWRIdentifier）は SDK リファレンスの Findings「Plug-in Modules」による。

#include "PluginPrefix.h"

#include "BuildConfig.h"
#include "Clock.h"

// リソース（.vwr）の識別子。リソースを引かなくても、SDK の静的ライブラリが参照するので要る。
// C++ のリンケージ（extern "C" を付けない）。
const char *DefaultPluginVWRIdentifier() { return PLUGIN_VWR_ID; }

// コンパイルした SDK の版（Vectorworks が読み込んでよいかを決める）。
extern "C" Sint32 GS_EXTERNAL_ENTRY plugin_module_ver() { return SDK_VERSION; }

extern "C" Sint32 GS_EXTERNAL_ENTRY plugin_module_main(Sint32 action, void * /*moduleInfo*/,
                                                       const VWIID & /*iid*/,
                                                       IVWUnknown *& /*inOutInterface*/,
                                                       CallBackPtr cbp) {
	::GS_InitializeVCOM(cbp);
	// 起動では kPluginModuleInit のあとにも何度か呼ばれる。タイマーは 1 度だけ始める（始められたら
	// 2 度目からは何もしない）。終了（kPluginModuleDeinit）は刻みが止まった後に届くので、
	// タイマーは止めない（Findings「Timers and Notifications」の 4 の 9）。
	if (action != kPluginModuleDeinit) {
		VwCli::startClock();
	}
	return 0;
}
