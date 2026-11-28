#pragma once

// 擬似 Documents（図面の SDK の呼び出しの代替）。図面を操作するツールと quit のテストが共に
// 使用する。

#include "core/DocumentTools.h"

#include <algorithm>
#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::test {

// FakeDocuments は開いている図面の一覧を保持し、SDK の呼び出しを模倣して、呼び出された順を
// 記録する。ツールの検証（DocumentToolsTests）と quit の検証（QuitTests）が共に使用する。
class FakeDocuments : public core::Documents {
public:
	std::vector<core::OpenDocument> open;
	std::vector<std::string> calls;
	std::uint32_t saveError = 0;
	bool switchFails = false;
	// SwitchToOpenFile が真を返すにもかかわらず前面が変わらない
	// （前面を確認せずに保存する・閉じると、別の図面に作用する）。
	bool switchIgnored = false;
	// 閉じても残存する（not_closed を模倣する）。
	bool closeKeeps = false;
	// 閉じると、この ref の図面も一覧から消える（前面へ戻せない場合を模倣する）。
	std::int32_t closeAlsoDrops = -1;
	// 保存の後、前面の図面の項目を一覧から削除する（読み直せない場合を模倣する）。
	bool saveLosesFront = false;
	// 閉じた図面が内部に残存した枚数（一覧には現れず、GetActiveDocument のみが真を返す）。
	int remnants = 0;
	// 一覧が空の場合の CloseDocument で、残存した図面が消えない。
	bool remnantsStick = false;
	// 後処理の最中に開く図面。一覧が空の場合の CloseDocument の間に、一覧へ追加される。
	std::optional<core::OpenDocument> opensWhileClearing;

	// count は name の呼び出しの回数（"list" など）。
	int count(std::string_view name) const {
		return static_cast<int>(std::ranges::count(calls, name));
	}
	// actions は図面を変更しうる呼び出し（前面の切り替え・保存・閉じる）のみを、呼び出された順に
	// 返す（一覧と前面の有無を読み取る呼び出しを除く）。
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
		// 一覧が空であれば、残存した図面を 1 枚消す。
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
	// hasActive は、残存した図面があるか、active のフラグの付いた図面があれば真（GetActiveDocument
	// を模倣する）。開いている図面を格納する場合は、前面の 1 枚に active を付ける。付けないと、
	// 図面が開いていても偽になり、後処理（clearClosedDocuments）が検証したい経路を通らずに
	// 停止する。
	bool hasActive() override {
		calls.emplace_back("active");
		return remnants > 0 ||
		       std::any_of(open.begin(), open.end(),
		                   [](const core::OpenDocument &document) { return document.active; });
	}
};

}
