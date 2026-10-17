#pragma once

// プラグインの名前と、ビルドの刻印（docs/plugin/identifiers.md「src/BuildConfig.h」）。

// リソース（.vwr）の識別子。パッケージする .vwr の基底名（resources/cli.vwr）と一致させる。
#define PLUGIN_VWR_ID "cli"

// 刻印は CMake が渡す（CI は短い sha とブランチ）。ping の結果にだけ載る
// （docs/plugin/build-and-release.md「リリースの形」）。
#ifndef VW_BUILD_VERSION
#define VW_BUILD_VERSION "local"
#endif
#ifndef VW_BUILD_BRANCH
#define VW_BUILD_BRANCH "local"
#endif
