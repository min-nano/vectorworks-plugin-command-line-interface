#pragma once

// SDK の文字列を UTF-8 の std::string にする（src/tools のツールが共に使用する。変換を 1 か所に
// 集約する）。
// PluginPrefix.h の後に読み込む。

#include <string>

namespace VwCli::tools {

// utf8 は TXString を UTF-8 にする。operator const char* は UTF-8 を返す（Findings「TXString」）。
// 返る const char* の寿命は元の TXString のものであるため、式の中で複製を完了する。
inline std::string utf8(const TXString &text) {
	return std::string(static_cast<const char *>(text));
}

}
