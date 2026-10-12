#include "core/Bridge.h"

#include "core/Protocol.h"

#include <algorithm>
#include <string_view>

namespace VwCli::core {

namespace {

bool endsWith(std::string_view text, std::string_view suffix) {
	return text.size() >= suffix.size() && text.substr(text.size() - suffix.size()) == suffix;
}

}

std::optional<std::string> defaultSpoolDir() {
	std::optional<std::string> root = files::appRoot();
	if (!root) {
		return std::nullopt;
	}
	return files::join(files::join(*root, protocol::kAppDirName), protocol::kSpoolDirName);
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
	// 作法はファイル名の昇順で処理する（id の昇順ではない。綴りに . より小さい - があるため）。
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
