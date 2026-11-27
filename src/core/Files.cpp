#include "core/Files.h"

// Files.h の関数のうち OS に依らない部分。OS の呼び出しは FilesPosix.cpp・FilesWindows.cpp に置く。

namespace VwCli::core::files {

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
