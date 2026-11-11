#include "core/SaveTarget.h"

#include <cerrno>
#include <sys/stat.h>

// save の保存先をファイルシステムに聞く関数の POSIX の実装（docs/plugin/tools.md「パスの一致の
// 判定」）。出荷するのは mac で、Linux のテストも同じ実装を通る。

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
	// リンクをたどらない（壊れたシンボリックリンクがある to も既にあるとする）。
	struct stat info{};
	int result = ::lstat(path.c_str(), &info);
	return classify(result, info);
}

Probe probeParent(const std::string &path) {
	// リンクをたどる（先の無いリンクの親は「無い」）。
	struct stat info{};
	int result = ::stat(path.c_str(), &info);
	return classify(result, info);
}

}
