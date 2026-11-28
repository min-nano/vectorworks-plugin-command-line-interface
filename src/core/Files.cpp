#include "core/Files.h"

// Files.h の関数の内 OS に依存しない部分。OS の呼び出しは FilesPosix.cpp・FilesWindows.cpp に
// 配置する。

namespace VwCli::core::files {

bool isSeparator(char c) {
#ifdef _WIN32
	// Windows は \ の他に / も区切りとして解釈する。
	return c == kSeparator || c == '/';
#else
	return c == kSeparator;
#endif
}

std::string join(const std::string &dir, std::string_view name) {
	std::string path = dir;
	if (!path.empty() && !isSeparator(path.back())) {
		path += kSeparator;
	}
	path.append(name);
	return path;
}

Lock::~Lock() { release(); }

}
