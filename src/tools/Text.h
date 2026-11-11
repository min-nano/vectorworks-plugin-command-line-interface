#pragma once

// SDK の文字列を UTF-8 の std::string にする（src/tools の道具が共に使う。変換を 1 か所にする）。
// PluginPrefix.h のあとに読む。

#include <string>

namespace VwCli::tools {

// utf8 は TXString を UTF-8 にする。operator const char* は UTF-8 を返す（Findings「TXString」）。
// 返る const char* の寿命は元の TXString のものなので、式の中で写しきる。
inline std::string utf8(const TXString &text) {
	return std::string(static_cast<const char *>(text));
}

}
