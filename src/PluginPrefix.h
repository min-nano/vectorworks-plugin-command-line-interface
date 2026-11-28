#pragma once

// SDK を使用するソースが最初に読み込むヘッダ（プリコンパイルする）。SDK のサンプル
// （SDK リファレンスの plugin/src/PluginPrefix.h）と同じく、Windows では SDK の前に Windows.h を
// 読み込む。VWFC の名前空間は使用しないため開かない（本リポジトリの名前と衝突させない）。

#ifdef _WINDOWS
#include <Windows.h>
#endif

// プラットフォーム（GS_MAC / GS_WIN）は __APPLE__ / _WINDOWS から決定される。mac では _WINDOWS を
// 定義しない。
#include "VectorworksSDK.h"
