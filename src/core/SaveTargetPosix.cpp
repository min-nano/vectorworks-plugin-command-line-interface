#include "core/SaveTarget.h"

#include <cerrno>
#include <sys/stat.h>

// save の保存先をファイルシステムに問い合わせる関数の POSIX の実装
// （docs/plugin/save-target.md「状態の検証」）。出荷するのは mac で、Linux のテストも同じ実装を
// 通過する。

namespace VwCli::core::save_target {

namespace {

Probe classify(int result, const struct stat &info) {
	if (result == 0) {
		return {S_ISDIR(info.st_mode) ? Entry::Directory : Entry::File, 0};
	}
	int error = errno;
	return {isMissing(error) ? Entry::Missing : Entry::Failed, error};
}

}

bool isMissing(std::int64_t osError) { return osError == ENOENT || osError == ENOTDIR; }

Probe probeTo(const std::string &path) {
	// リンクをたどらない（壊れたシンボリックリンクが存在する to も既に存在するものとする）。
	struct stat info{};
	int result = ::lstat(path.c_str(), &info);
	return classify(result, info);
}

Probe probeParent(const std::string &path) {
	// リンクをたどる（参照先の無いリンクの親は「無い」）。
	struct stat info{};
	int result = ::stat(path.c_str(), &info);
	return classify(result, info);
}

}
