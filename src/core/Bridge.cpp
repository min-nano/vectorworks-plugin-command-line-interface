#include "core/Bridge.h"

#include "core/Protocol.h"

#include <algorithm>
#include <string_view>

namespace VwCli::core {

namespace {

// mac のプラグインのバンドル（<name>.vwlibrary）の名前の末尾。
constexpr std::string_view kBundleSuffix = ".vwlibrary";

}

std::optional<std::string> spoolDirOf(const std::string &modulePath) {
	// 区切りの位置を列挙する（先頭から）。
	std::vector<std::size_t> separators;
	for (std::size_t at = 0; at < modulePath.size(); ++at) {
		if (files::isSeparator(modulePath[at])) {
			separators.push_back(at);
		}
	}
	if (separators.empty()) {
		return std::nullopt;
	}
	// i 番目の区切りの後方のフォルダの名前（最後の区切りの後方はモジュールそのもの）。
	auto folderName = [&](std::size_t i) {
		return std::string_view(modulePath.data() + separators[i] + 1,
		                        separators[i + 1] - separators[i] - 1);
	};
	// 既定はモジュールのあるフォルダ（最後の区切りの前）。mac のバンドルの中の実行ファイル
	// （<X>.vwlibrary/Contents/MacOS/<実行ファイル>）の形式の場合のみ、バンドルのあるフォルダまで
	// 遡る。パスの途中に .vwlibrary で終わるフォルダが存在しても遡らない（呼び出し元は bin の親を
	// 取得するため、遡ると両側の場所が相違する）。
	std::size_t folderEnd = separators.back();
	std::size_t n = separators.size();
	if (n >= 4 && folderName(n - 2) == "MacOS" && folderName(n - 3) == "Contents" &&
	    folderName(n - 4).ends_with(kBundleSuffix)) {
		folderEnd = separators[n - 4];
	}
	// ルートの直下（/cli.vlb など）ならば、区切りを残してルートを指す。
	std::string folder = modulePath.substr(0, folderEnd == 0 ? 1 : folderEnd);
	return files::join(folder, protocol::kSpoolDirName);
}

std::optional<std::string> defaultSpoolDir() {
	// モジュールの中のアドレスであれば任意でよいため、関数ではなく静的なデータのアドレスを渡す
	// （関数のポインタを void * に変換することは処理系に依存する）。
	// 取得できなければ空のパスを渡し、spoolDirOf が空を返す（区切りが無い）。
	static const char anchor = 0;
	return spoolDirOf(files::modulePathOf(&anchor).value_or(""));
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
		bool ours = name.ends_with(protocol::kRequestSuffix) ||
		            name.ends_with(protocol::kResponseSuffix) ||
		            name.ends_with(protocol::kWaitSuffix) || name.ends_with(protocol::kTempSuffix);
		if (!ours) {
			continue;
		}
		std::string id = name.substr(0, name.find('.'));
		// 確かめられない待つ印の id のファイルも残す（待っている呼ぶ側のものかもしれない）。
		files::WaitState state = waitState(id);
		if (state == files::WaitState::Released || state == files::WaitState::Missing) {
			files::remove(path(name));
		}
	}
}

std::vector<std::string> Bridge::poll() const {
	std::vector<std::string> requests;
	if (std::optional<std::vector<std::string>> names = files::list(dir_)) {
		for (const std::string &name : *names) {
			if (!name.ends_with(protocol::kRequestSuffix)) {
				continue;
			}
			std::string_view id(name.data(), name.size() - protocol::kRequestSuffix.size());
			if (protocol::validId(id)) {
				requests.push_back(name);
			}
		}
	}
	// 通信規則はファイル名の昇順で処理する（id の昇順ではない。綴りに . より小さい - が
	// 含まれるため）。
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
