#include "core/SaveTarget.h"

#include "core/Args.h"

#include <cstddef>

namespace VwCli::core::save_target {

namespace {

// mac の上限（ファイルシステムの名前の長さと PATH_MAX。どちらも UTF-8 のバイト）。
constexpr std::size_t kMacSegmentBytes = 255;
constexpr std::size_t kMacPathBytes = 1024;
// Windows の上限（名前の長さと MAX_PATH。どちらも UTF-16 の単位）。
constexpr std::size_t kWindowsSegmentUnits = 255;
constexpr std::size_t kWindowsPathUnits = 260;

constexpr std::string_view kExtension = ".vwx";

bool hasControl(std::string_view text) {
	for (char c : text) {
		if (static_cast<unsigned char>(c) < 0x20) {
			return true;
		}
	}
	return false;
}

// endsWithVwx は名前が .vwx で終わるか（大文字と小文字を区別しない。ASCII だけを畳む）。
bool endsWithVwx(std::string_view name) {
	if (name.size() < kExtension.size()) {
		return false;
	}
	std::string_view tail = name.substr(name.size() - kExtension.size());
	for (std::size_t i = 0; i < tail.size(); ++i) {
		char c = tail[i];
		if (c >= 'A' && c <= 'Z') {
			c = static_cast<char>(c - 'A' + 'a');
		}
		if (c != kExtension[i]) {
			return false;
		}
	}
	return true;
}

// utf16Units は UTF-8 の文字列を UTF-16 にしたときの単位の数（サロゲートペアは 2 つ）。文字列は
// 要求の解析で正しい UTF-8 になっている。
std::size_t utf16Units(std::string_view text) {
	std::size_t units = 0;
	for (char c : text) {
		auto byte = static_cast<unsigned char>(c);
		if ((byte & 0xC0) == 0x80) {
			continue;
		}
		units += byte >= 0xF0 ? 2 : 1;
	}
	return units;
}

std::vector<std::string_view> split(std::string_view text, char separator) {
	std::vector<std::string_view> segments;
	std::size_t start = 0;
	for (;;) {
		std::size_t at = text.find(separator, start);
		if (at == std::string_view::npos) {
			segments.push_back(text.substr(start));
			return segments;
		}
		segments.push_back(text.substr(start, at - start));
		start = at + 1;
	}
}

std::optional<std::string> refuse(std::string &problem, std::string message) {
	problem = std::move(message);
	return std::nullopt;
}

// commonSegment は mac と Windows に共通の段の確かめ（空の段・. と ..）。
std::optional<std::string> commonSegment(std::string_view segment) {
	if (segment.empty()) {
		return "空の段があります（区切りが続いているか、区切りで終わっています）";
	}
	if (segment == "." || segment == "..") {
		return ". か .. の段があります（正規化してから渡してください）";
	}
	return std::nullopt;
}

}

std::optional<std::string> checkMacTo(std::string_view to, std::string &problem) {
	if (!to.starts_with('/')) {
		return refuse(problem, "to が絶対パス（/ から始まる）ではありません");
	}
	if (hasControl(to)) {
		return refuse(problem, "to に制御文字があります");
	}
	if (to.size() >= kMacPathBytes) {
		return refuse(problem, "to が長すぎます（1024 バイト以上）");
	}
	std::vector<std::string_view> segments = split(to.substr(1), '/');
	for (std::string_view segment : segments) {
		if (std::optional<std::string> message = commonSegment(segment)) {
			return refuse(problem, *message);
		}
		if (segment.size() > kMacSegmentBytes) {
			return refuse(problem, "to に長すぎる段があります（255 バイトを超える）");
		}
	}
	if (!endsWithVwx(segments.back())) {
		return refuse(problem, "to の拡張子が .vwx ではありません");
	}
	std::size_t last = to.rfind('/');
	return std::string(last == 0 ? to.substr(0, 1) : to.substr(0, last));
}

std::optional<std::string> checkWindowsTo(std::string_view to, std::string &problem) {
	if (hasControl(to)) {
		return refuse(problem, "to に制御文字があります");
	}
	if (to.find('/') != std::string_view::npos) {
		return refuse(problem, "to に / があります（区切りは \\ で渡してください）");
	}
	if (utf16Units(to) >= kWindowsPathUnits) {
		return refuse(problem, "to が長すぎます（260 文字以上）");
	}
	auto letter = [](char c) { return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z'); };
	bool drive = to.size() >= 3 && letter(to[0]) && to[1] == ':' && to[2] == '\\';
	bool unc = !drive && to.starts_with("\\\\");
	if (!drive && !unc) {
		return refuse(problem,
		              "to が絶対パス（X:\\ か \\\\<サーバー>\\<共有>\\ で始まる）ではありません");
	}
	// ルートの後のどの段も確かめる（UNC はサーバーと共有の段も）。
	std::vector<std::string_view> segments = split(to.substr(drive ? 3 : 2), '\\');
	for (std::size_t i = 0; i < segments.size(); ++i) {
		std::string_view segment = segments[i];
		if (std::optional<std::string> message = commonSegment(segment)) {
			return refuse(problem, *message);
		}
		if (utf16Units(segment) > kWindowsSegmentUnits) {
			return refuse(problem, "to に長すぎる段があります（255 文字を超える）");
		}
		if (segment.find_first_of("<>:\"|?*") != std::string_view::npos) {
			return refuse(problem, "to に Windows で使えない文字（< > : \" | ? *）があります");
		}
		bool last = i + 1 == segments.size();
		if (!last && (segment.back() == '.' || segment.back() == ' ')) {
			return refuse(problem, "to に末尾が . か空白の段があります");
		}
	}
	if (unc && segments.size() < 3) {
		return refuse(problem, "UNC の to にはサーバー・共有・ファイル名が要ります");
	}
	if (!endsWithVwx(segments.back())) {
		return refuse(problem, "to の拡張子が .vwx ではありません");
	}
	std::size_t last = to.rfind('\\');
	// ルートの直下は、区切りを含めたルートの形（C:\ と \\server\share\）。
	bool underRoot = segments.size() == (drive ? 1 : 3);
	return std::string(to.substr(0, underRoot ? last + 1 : last));
}

#ifdef _WIN32
const FormCheck kNativeForm = &checkWindowsTo;
#else
const FormCheck kNativeForm = &checkMacTo;
#endif

bool check(const std::string &to, const std::string &parent,
           const std::vector<std::string> &openPaths, const Checks &checks, ToolStatus &status) {
	for (const std::string &open : openPaths) {
		if (checks.same(open, to)) {
			args::failWithReason(status, kToIsOpen,
			                     "保存先は開いている図面です（" + open +
			                         "）。新しいパスを渡してください");
			return false;
		}
	}
	auto checkFailed = [&](std::string_view step, const std::string &path, std::int64_t osError) {
		args::fail(status, ErrorCode::Internal,
		           "保存先を確かめられませんでした（" + path + "。OS のエラー " +
		               std::to_string(osError) + "）",
		           Json::object({{"reason", Json::string(std::string(kToCheckFailed))},
		                         {"check", Json::string(std::string(step))},
		                         {"os_error", Json::integer(osError)}}));
		return false;
	};
	Probe target = checks.to(to);
	switch (target.entry) {
	case Entry::File:
	case Entry::Directory:
		args::failWithReason(status, kFileExists,
		                     "保存先が既にあります（" + to + "）。新しいパスを渡してください");
		return false;
	case Entry::Failed:
		return checkFailed("to", to, target.osError);
	case Entry::Missing:
		break;
	}
	Probe folder = checks.parent(parent);
	switch (folder.entry) {
	case Entry::Directory:
		return true;
	case Entry::Failed:
		return checkFailed("parent", parent, folder.osError);
	case Entry::File:
	case Entry::Missing:
		break;
	}
	args::failWithReason(status, kParentMissing, "保存先のフォルダがありません（" + parent + "）");
	return false;
}

}
