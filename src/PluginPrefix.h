#pragma once

// SDK を使うソースが最初に読むヘッダ（プリコンパイルする）。SDK の見本（SDK リファレンスの
// plugin/src/PluginPrefix.h）と同じく、Windows では SDK の前に Windows.h を読む。VWFC の名前空間は
// 使わないので開かない（このリポジトリの名前とぶつけない）。

#ifdef _WINDOWS
#include <Windows.h>
#endif

// プラットフォーム（GS_MAC / GS_WIN）は __APPLE__ / _WINDOWS から決まる。mac では _WINDOWS を
// 定義しない。
#include "VectorworksSDK.h"
