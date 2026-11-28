// プラグインのエントリポイント（docs/plugin/architecture.md「エントリポイント」）。拡張機能は
// 登録せず、VCOM を初期化して OS タイマーを開始するのみ（docs/plugin/architecture.md
// 「拡張機能の非登録」）。
//
// SDK の静的ライブラリをリンクするモジュールが定義するもの（plugin_module_ver と
// DefaultPluginVWRIdentifier）は SDK リファレンスの Findings「Plug-in Modules」に基づく。

#include "PluginPrefix.h"

#include "BuildConfig.h"
#include "Clock.h"

// リソース（.vwr）の識別子。リソースを参照しなくても、SDK の静的ライブラリが参照するため
// 必要である。
// C++ のリンケージ（extern "C" を付けない）。
const char *DefaultPluginVWRIdentifier() { return PLUGIN_VWR_ID; }

// コンパイルした SDK の版（Vectorworks が読み込んでよいかを決定する）。
extern "C" Sint32 GS_EXTERNAL_ENTRY plugin_module_ver() { return SDK_VERSION; }

extern "C" Sint32 GS_EXTERNAL_ENTRY plugin_module_main(Sint32 action, void * /*moduleInfo*/,
                                                       const VWIID & /*iid*/,
                                                       IVWUnknown *& /*inOutInterface*/,
                                                       CallBackPtr cbp) {
	::GS_InitializeVCOM(cbp);
	// 起動時には kPluginModuleInit の後にも何度か呼び出される。タイマーは 1 度のみ開始する
	// （開始できたら 2 度目からは何もしない）。終了（kPluginModuleDeinit）は周期処理が停止した後に
	// 届くため、タイマーは停止しない（Findings「Timers and Notifications」の 4 の 9）。
	if (action != kPluginModuleDeinit) {
		VwCli::startClock();
	}
	return 0;
}
