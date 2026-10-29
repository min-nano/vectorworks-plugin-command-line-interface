#include "core/Bridge.h"

#include "core/Protocol.h"

#include <algorithm>
#include <string_view>

namespace VwCli::core {

namespace {

bool endsWith(std::string_view text, std::string_view suffix) {
	return text.size() >= suffix.size() && text.substr(text.size() - suffix.size()) == suffix;
}

// mac のプラグインのバンドル（<name>.vwlibrary）の名前の終わり。
constexpr std::string_view kBundleSuffix = ".vwlibrary";

bool isSeparator(char c) {
#ifdef _WIN32
	return c == '/' || c == '\\';
#else
	return c == '/';
#endif
}

}

std::optional<std::string> spoolDirOf(const std::string &modulePath) {
	// 区切りの位置を並べる（先頭から）。
	std::vector<std::size_t> separators;
	for (std::size_t at = 0; at < modulePath.size(); ++at) {
		if (isSeparator(modulePath[at])) {
			separators.push_back(at);
		}
	}
	if (separators.empty()) {
		return std::nullopt;
	}
	// i 番目の区切りの後ろのフォルダの名前（最後の区切りの後ろはモジュールそのもの）。
	auto folderName = [&](std::size_t i) {
		return std::string_view(modulePath.data() + separators[i] + 1,
		                        separators[i + 1] - separators[i] - 1);
	};
	// 既定はモジュールのあるフォルダ（最後の区切りの前）。mac のバンドルの中の実行ファイル
	// （<X>.vwlibrary/Contents/MacOS/<実行ファイル>）の形のときだけ、バンドルのあるフォルダまで
	// 上る。パスの途中に .vwlibrary で終わるフォルダがあっても上らない（呼ぶ側は bin の親を
	// 取るので、上ると両側の場所が食い違う）。
	std::size_t folderEnd = separators.back();
	std::size_t n = separators.size();
	if (n >= 4 && folderName(n - 2) == "MacOS" && folderName(n - 3) == "Contents" &&
	    endsWith(folderName(n - 4), kBundleSuffix)) {
		folderEnd = separators[n - 4];
	}
	// 根の直下（/cli.vlb など）なら、区切りを残して根を指す。
	std::string folder = modulePath.substr(0, folderEnd == 0 ? 1 : folderEnd);
	return files::join(folder, protocol::kSpoolDirName);
}

std::optional<std::string> defaultSpoolDir() {
	// モジュールの中の番地なら何でもよいので、関数ではなく静的なデータの番地を渡す（関数の
	// ポインタを void * にするのは処理系に依る）。
	static const char anchor = 0;
	std::optional<std::string> module = files::modulePathOf(&anchor);
	if (!module) {
		return std::nullopt;
	}
	return spoolDirOf(*module);
}

bool Bridge::prepare(std::string &error) { return files::makeDirectories(dir_, error); }

files::Lock::Result Bridge::lock(std::string &error) {
	return lock_.take(path(std::string(protocol::kLockFile)), error);
}

void Bridge::sweep() {
	std::optional<std::vector<std::string>> names = files::list(dir_);
	if (!names) {
		return;
	}
	for (const std::string &name : *names) {
		bool ours = endsWith(name, protocol::kRequestSuffix) ||
		            endsWith(name, protocol::kResponseSuffix) ||
		            endsWith(name, protocol::kWaitSuffix) || endsWith(name, protocol::kTempSuffix);
		if (!ours) {
			continue;
		}
		std::string id = name.substr(0, name.find('.'));
		if (waitState(id) != files::WaitState::Held) {
			files::remove(path(name));
		}
	}
}

std::vector<std::string> Bridge::poll() const {
	std::vector<std::string> requests;
	if (std::optional<std::vector<std::string>> names = files::list(dir_)) {
		for (const std::string &name : *names) {
			if (!endsWith(name, protocol::kRequestSuffix)) {
				continue;
			}
			std::string_view id(name.data(), name.size() - protocol::kRequestSuffix.size());
			if (protocol::validId(id)) {
				requests.push_back(name);
			}
		}
	}
	// 通信規則はファイル名の昇順で処理する（id の昇順ではない。綴りに . より小さい - があるため）。
	std::sort(requests.begin(), requests.end());
	for (std::string &name : requests) {
		name.resize(name.size() - protocol::kRequestSuffix.size());
	}
	return requests;
}

std::optional<std::string> Bridge::take(const std::string &id) {
	std::string request = path(id + std::string(protocol::kRequestSuffix));
	std::optional<std::string> text = files::read(request, protocol::kMaxRequestBytes + 1);
	if (!text || !files::remove(request)) {
		return std::nullopt;
	}
	return text;
}

files::WaitState Bridge::waitState(const std::string &id) const {
	return files::probe(path(id + std::string(protocol::kWaitSuffix)));
}

void Bridge::removeWait(const std::string &id) {
	files::remove(path(id + std::string(protocol::kWaitSuffix)));
}

bool Bridge::reply(const std::string &id, const std::string &body) {
	std::string response = path(id + std::string(protocol::kResponseSuffix));
	std::string temp = response + std::string(protocol::kTempSuffix);
	if (!files::write(temp, body)) {
		files::remove(temp);
		return false;
	}
	if (!files::rename(temp, response)) {
		files::remove(temp);
		return false;
	}
	return true;
}

}
