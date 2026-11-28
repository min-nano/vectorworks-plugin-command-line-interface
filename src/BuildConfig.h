#pragma once

// プラグインの名前と、ビルドの刻印（docs/plugin/identifiers.md「src/BuildConfig.h」）。

// リソース（.vwr）の識別子。パッケージする .vwr の基底名（resources/cli.vwr）と一致させる。
#define PLUGIN_VWR_ID "cli"

// 刻印（VW_BUILD_VERSION / VW_BUILD_BRANCH）。CMake が構成のたびに書き込む（-D で
// 渡さなければ "local"）。ping の結果にのみ含まれる（docs/plugin/build-and-release.md
// 「リリースの構成」）。
#include "BuildStamp.h"
