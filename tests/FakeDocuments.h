#pragma once

// 偽の Documents（図面の SDK の呼び出しの代わり）。図面を扱う道具と quit のテストが共に使う。

#include "core/DocumentTools.h"

#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <stdexcept>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::test {

// FakeDocuments は開いている図面の一覧を持ち、SDK の呼び出しを真似て、呼んだ順を覚える。
// 道具の確かめ（DocumentToolsTests）と quit の確かめ（QuitTests）が共に使う。
class FakeDocuments : public core::Documents {
public:
	std::vector<core::OpenDocument> open;
	std::vector<std::string> calls;
	std::uint32_t saveError = 0;
	bool switchFails = false;
	// SwitchToOpenFile が真を返すのに前面が変わらない
	// （前面を確かめずに保存・閉じると、別の図面に効く）。
	bool switchIgnored = false;
	// 閉じても残る（not_closed を真似る）。
	bool closeKeeps = false;
	// 閉じると、この ref の図面も一覧から消える（前面へ戻せない場合を真似る）。
	std::int32_t closeAlsoDrops = -1;
	// 保存の後、前面の図面の項目を一覧から消す（読み直せない場合を真似る）。
	bool saveLosesFront = false;
	// 閉じた図面が中に残った枚数（一覧には出ず、GetActiveDocument だけが真を返す）。
	int remnants = 0;
	// 一覧が空のときの CloseDocument で、残った図面が消えない。
	bool remnantsStick = false;
	// 片付けの最中に開く図面。一覧が空のときの CloseDocument の間に、一覧へ加わる。
	std::optional<core::OpenDocument> opensWhileClearing;
	// この回目（1 から数える）の一覧の読み出しで投げる。0 なら投げない。
	// パスを読めない・確保できないことを真似る。投げた回も calls に残す。
	int listThrowsAt = 0;
	// listThrowsAt の回より後の読み出しでも投げ続ける（前面を戻すための読み直しも失敗する場合）。
	bool listKeepsThrowing = false;

	// count は name の呼び出しの回数（"list" など）。
	int count(std::string_view name) const {
		return static_cast<int>(std::ranges::count(calls, name));
	}
	// actions は図面を変えうる呼び出し（前面の切り替え・保存・閉じる）だけを、呼んだ順に返す
	// （一覧と前面の有無を読む呼び出しを除く）。
	std::vector<std::string> actions() const {
		std::vector<std::string> out;
		for (const std::string &call : calls) {
			if (call != "list" && call != "active") {
				out.push_back(call);
			}
		}
		return out;
	}

	std::vector<core::OpenDocument> list() override {
		calls.emplace_back("list");
		int nth = count("list");
		if (listThrowsAt > 0 &&
		    (nth == listThrowsAt || (listKeepsThrowing && nth > listThrowsAt))) {
			throw std::runtime_error("cannot read the list");
		}
		return open;
	}
	bool switchTo(std::int32_t ref) override {
		calls.push_back("switch " + std::to_string(ref));
		if (switchFails) {
			return false;
		}
		if (switchIgnored) {
			return true;
		}
		for (core::OpenDocument &document : open) {
			document.active = document.ref == ref;
		}
		return true;
	}
	std::uint32_t saveActive(const std::string &path) override {
		calls.push_back("save " + path);
		if (saveError == 0) {
			for (std::size_t i = 0; i < open.size(); ++i) {
				if (open[i].active) {
					if (saveLosesFront) {
						open.erase(open.begin() + static_cast<std::ptrdiff_t>(i));
						break;
					}
					open[i].path = path;
					open[i].untitled = false;
				}
			}
		}
		return saveError;
	}
	void closeActive() override {
		calls.emplace_back("close");
		if (closeKeeps) {
			return;
		}
		// 一覧が空なら、残った図面を 1 枚消す。
		if (open.empty()) {
			if (remnants > 0 && !remnantsStick) {
				--remnants;
			}
			if (opensWhileClearing) {
				open.push_back(*opensWhileClearing);
				opensWhileClearing.reset();
			}
			return;
		}
		for (std::size_t i = 0; i < open.size(); ++i) {
			if (open[i].active) {
				open.erase(open.begin() + static_cast<std::ptrdiff_t>(i));
				break;
			}
		}
		std::erase_if(open, [this](const core::OpenDocument &document) {
			return document.ref == closeAlsoDrops;
		});
		// 閉じた後に前面になるのは最後に開いた図面（ここでは一覧の末尾）。
		if (!open.empty()) {
			open.back().active = true;
		}
	}
	// hasActive は、残った図面があるか、active の印の付いた図面があれば真（GetActiveDocument を
	// 真似る）。開いている図面を入れるときは、前面の 1 枚に active を付ける。付けないと、図面が
	// 開いていても偽になり、片付け（clearClosedDocuments）が確かめたい道筋を通らずに止まる。
	bool hasActive() override {
		calls.emplace_back("active");
		return remnants > 0 ||
		       std::any_of(open.begin(), open.end(),
		                   [](const core::OpenDocument &document) { return document.active; });
	}
};

}
