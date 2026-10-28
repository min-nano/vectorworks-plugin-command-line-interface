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
	// 既定はモジュールのあるフォルダ（最後の区切りの前）。バンドルの中なら、最も近いバンドルの
	// 名前の前の区切りまで上る（名前は区切りどうしの間。最後の名前はモジュールそのもの）。
	std::size_t folderEnd = separators.back();
	for (std::size_t i = separators.size() - 1; i > 0; --i) {
		std::string_view name(modulePath.data() + separators[i - 1] + 1,
		                      separators[i] - separators[i - 1] - 1);
		if (endsWith(name, kBundleSuffix)) {
			folderEnd = separators[i - 1];
			break;
		}
	}
	// 根の直下（/cli.vlb など）なら、区切りを残して根を指す。
	std::string folder = modulePath.substr(0, folderEnd == 0 ? 1 : folderEnd);
	return files::join(folder, protocol::kSpoolDirName);
}

std::optional<std::string> defaultSpoolDir() {
	std::optional<std::string> module = files::modulePath();
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
