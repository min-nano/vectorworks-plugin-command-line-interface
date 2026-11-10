#include "PluginPrefix.h"

#include "core/ReadTools.h"
#include "tools/Tools.h"

#include <cstddef>
#include <exception>
#include <string>
#include <vector>

// 読む道具（layers・classes・layer_objects・object_counts）の SDK の側。SDK から値を読んで
// core::Drawing として渡すだけで、数え方・絞り込み・結果の形は src/core/ReadTools.cpp
// （docs/plugin/tools.md「読む道具」。使う呼び出しは docs/sdk-research.md の 4）。

namespace VwCli::tools {

namespace {

// operator const char* は UTF-8 を返す（Findings「TXString」）。式の中で写しきる。
std::string utf8(const TXString &text) { return std::string(static_cast<const char *>(text)); }

// SdkDrawing は前面の図面を SDK で読む。ハンドルは道具 1 回の中でだけ持つ（刻みをまたいで持たない。
// 閉じた図面と別の図面のハンドルが同じ番地で返る。Findings「Timers and Notifications」の 4 の 8）。
class SdkDrawing final : public core::Drawing {
public:
	// 図面が無ければ GetCurrentLayer() は落ちずに nullptr を返す（ping と同じ基準）。
	bool open() override { return gSDK->GetCurrentLayer() != nullptr; }

	std::vector<core::LayerFacts> layers() override {
		members_.clear();
		std::vector<core::LayerFacts> out;
		MCObjectHandle current = gSDK->GetCurrentLayer();
		// GetDrawingHeader の子の列はレイヤを作った順に並べ、末尾にレイヤでない型 0 が 1
		// つ居るので、 型で絞る（Findings「Layers and Stories」の「レイヤを列挙する」）。
		for (MCObjectHandle layer = gSDK->FirstMemberObj(gSDK->GetDrawingHeader());
		     layer != nullptr; layer = gSDK->NextObject(layer)) {
			if (gSDK->GetObjectTypeN(layer) != ::kLayerNode) {
				continue;
			}
			core::LayerFacts facts;
			TXString name;
			gSDK->GetObjectName(layer, name);
			facts.name = utf8(name);
			// ovLayerType は Sint16 で受け取れる（デザイン 1・シート
			// 2・リソースの参照で入ったデザイン 3）。
			TVariableBlock value;
			Sint16 type = 0;
			if (gSDK->GetObjectVariable(layer, ovLayerType, value) && value.GetSint16(type)) {
				facts.layerType = type;
			}
			// 参照由来かは IsObjectFromReferencedFile で見る。ovLayerType の 3
			// はリソースの参照でしか 立たず、レイヤの参照で入ったレイヤは 1
			// で自分のレイヤと見分けられない（Findings「Layers and
			// Stories」の「参照で入ってきたレイヤ」。SDK リファレンス #310）。
			facts.referenced = gSDK->IsObjectFromReferencedFile(layer) != 0;
			double_gs scale = 1;
			gSDK->GetLayerScaleN(layer, scale);
			facts.scale = scale;
			facts.current = layer == current;
			// 直下の列は終端（型 0）と undo の置き石（型 90）を含んだまま渡す（除くのは core。
			// Findings「Layer Objects」の 2）。
			std::vector<MCObjectHandle> handles;
			for (MCObjectHandle member = gSDK->FirstMemberObj(layer); member != nullptr;
			     member = gSDK->NextObject(member)) {
				facts.memberTypes.push_back(gSDK->GetObjectTypeN(member));
				handles.push_back(member);
			}
			out.push_back(std::move(facts));
			members_.push_back(std::move(handles));
		}
		return out;
	}

	std::vector<std::string> classes() override {
		std::vector<std::string> out;
		// 例外を SDK のフレームを越えて投げないよう、コールバックの中で受けてから投げ直す。
		std::exception_ptr failure;
		// doGuestClasses を偽にし、参照している図面のクラス（NNA#<n>_…）を含めない。渡されるのは
		// クラスの定義（型 94）で、ID の順（Findings「Attributes and Classes」の「クラスの一覧は
		// ForEachClass で列挙する」）。
		gSDK->ForEachClass(false, [&](MCObjectHandle handle) {
			if (failure || gSDK->GetObjectTypeN(handle) != ::kClassDefNode) {
				return;
			}
			try {
				TXString name;
				gSDK->ClassIDToName(gSDK->GetObjectInternalIndex(handle), name);
				out.push_back(utf8(name));
			} catch (...) {
				failure = std::current_exception();
			}
		});
		if (failure) {
			std::rethrow_exception(failure);
		}
		return out;
	}

	core::ObjectFacts object(std::size_t layer, std::size_t member) override {
		MCObjectHandle handle = members_.at(layer).at(member);
		core::ObjectFacts facts;
		TXString name;
		gSDK->GetObjectName(handle, name);
		facts.name = utf8(name);
		TXString className;
		gSDK->ClassIDToName(gSDK->GetObjectClass(handle), className);
		facts.className = utf8(className);
		// 外形は縮尺を掛けない世界座標の mm（Findings「Layer Objects」の 6）。
		WorldRect bounds;
		facts.boundsOk = gSDK->GetObjectBounds(handle, bounds) != 0;
		facts.left = bounds.Left();
		facts.top = bounds.Top();
		facts.right = bounds.Right();
		facts.bottom = bounds.Bottom();
		// PIO の名前（Findings「Layer Objects」の 5）。PIO の型を確かめてから包む。
		if (gSDK->GetObjectTypeN(handle) == ::kParametricNode) {
			VWFC::VWObjects::VWParametricObj parametric(handle);
			facts.pioName = utf8(parametric.GetParametricName());
		}
		return facts;
	}

private:
	// 直前の layers() のレイヤごとの、直下の列のハンドル。
	std::vector<std::vector<MCObjectHandle>> members_;
};

}

core::Json layers(const core::Json &args, const core::ToolContext & /*context*/,
                  core::ToolStatus &status) {
	SdkDrawing drawing;
	return core::layers(args, drawing, status);
}

core::Json classes(const core::Json &args, const core::ToolContext & /*context*/,
                   core::ToolStatus &status) {
	SdkDrawing drawing;
	return core::classes(args, drawing, status);
}

core::Json layerObjects(const core::Json &args, const core::ToolContext & /*context*/,
                        core::ToolStatus &status) {
	SdkDrawing drawing;
	return core::layerObjects(args, drawing, status);
}

core::Json objectCounts(const core::Json &args, const core::ToolContext & /*context*/,
                        core::ToolStatus &status) {
	SdkDrawing drawing;
	return core::objectCounts(args, drawing, status);
}

}
