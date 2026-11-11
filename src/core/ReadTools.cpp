#include "core/ReadTools.h"

#include "core/Args.h"

#include <algorithm>
#include <cmath>
#include <map>

namespace VwCli::core {

namespace {

// layer_objects の limit の範囲と既定値（docs/plugin/tools.md「layer_objects・object_counts」）。
constexpr std::int64_t kMinLimit = 1;
constexpr std::int64_t kMaxLimit = 1000;
constexpr std::int64_t kDefaultLimit = 50;

std::size_t countOf(const LayerFacts &layer) {
	return static_cast<std::size_t>(
	    std::count_if(layer.memberTypes.begin(), layer.memberTypes.end(), counted));
}

Json typeNameJson(std::int16_t type) {
	std::optional<std::string_view> name = typeName(type);
	return name ? Json::string(std::string(*name)) : Json();
}

// kNoLayer は findLayer が 1 枚に決められなかったときの値。
constexpr std::size_t kNoLayer = static_cast<std::size_t>(-1);

// findLayer は名前が完全に一致するレイヤの、列の中の位置を返す。無ければ invalid_args、2 枚以上なら
// internal（layer_ambiguous）で失敗し、kNoLayer を返す。Vectorworks は同じ名前のレイヤを参照で
// 入れることも断る（Findings「Layers and Stories」の「参照で入ってきたレイヤ」）が、確かめていない
// 経路で同じ名前が 2 枚になっても、黙ってどちらかを読まない（save・close の document_ambiguous と
// 同じ扱い）。
std::size_t findLayer(const std::vector<std::string> &names, const std::string &name,
                      std::string_view tool, ToolStatus &status) {
	std::size_t found = kNoLayer;
	int matches = 0;
	for (std::size_t i = 0; i < names.size(); ++i) {
		if (names[i] == name) {
			found = i;
			++matches;
		}
	}
	if (matches == 0) {
		args::fail(status, ErrorCode::InvalidArgs,
		           std::string(tool) + " の引数が誤っています: レイヤ " + name + " がありません");
		return kNoLayer;
	}
	if (matches > 1) {
		args::failWithReason(status, kLayerAmbiguous,
		                     "名前が " + name + " のレイヤが " + std::to_string(matches) +
		                         " 枚あるので、どれも読みませんでした");
		return kNoLayer;
	}
	return found;
}

// allLayers は前面の図面のすべてのレイヤを、直下の列まで読む。
std::vector<LayerFacts> allLayers(Drawing &drawing) {
	std::size_t count = drawing.layerNames().size();
	std::vector<LayerFacts> layers;
	layers.reserve(count);
	for (std::size_t i = 0; i < count; ++i) {
		layers.push_back(drawing.layer(i));
	}
	return layers;
}

}

std::optional<std::string_view> typeName(std::int16_t type) {
	// 綴りは docs/plugin/tools.md の表が真実。載せた綴りは変えない（行を足すのは互換の変更）。
	switch (type) {
	case 2:
		return "line";
	case 3:
		return "box";
	case 4:
		return "oval";
	case 5:
		return "polygon";
	case 6:
		return "arc";
	case 10:
		return "text";
	case 11:
		return "group";
	case 15:
		return "symbol";
	case 17:
		return "locus";
	case 21:
		return "polyline";
	case 24:
		return "extrude";
	case 68:
		return "wall";
	case 84:
		return "csg_tree";
	case 86:
		return "parametric";
	case 122:
		return "viewport";
	default:
		return std::nullopt;
	}
}

Json boundsJson(bool ok, double left, double top, double right, double bottom) {
	if (!ok || !std::isfinite(left) || !std::isfinite(top) || !std::isfinite(right) ||
	    !std::isfinite(bottom)) {
		return Json();
	}
	return Json::object({{"left", Json::real(std::min(left, right))},
	                     {"right", Json::real(std::max(left, right))},
	                     {"top", Json::real(std::max(top, bottom))},
	                     {"bottom", Json::real(std::min(top, bottom))}});
}

Json layers(const Json &args, Drawing &drawing, ToolStatus &status) {
	args::Reader reader(args, kLayersSpec.name, status);
	reader.onlyKnown({"include_sheets"});
	bool includeSheets = reader.boolean("include_sheets", true);
	if (!reader.ok()) {
		return Json();
	}
	if (!drawing.open()) {
		args::noDocument(status);
		return Json();
	}
	Json list = Json::array();
	for (const LayerFacts &layer : allLayers(drawing)) {
		Json kind;
		if (layer.layerType == kLayerDesign || layer.layerType == kGuestLayerDesign) {
			kind = Json::string("design");
		} else if (layer.layerType == kLayerSheet) {
			if (!includeSheets) {
				continue;
			}
			kind = Json::string("sheet");
		}
		list.push(
		    Json::object({{"name", Json::string(layer.name)},
			              {"kind", std::move(kind)},
			              {"referenced", Json::boolean(layer.referenced)},
			              {"scale", Json::real(layer.scale)},
			              {"objects", Json::integer(static_cast<std::int64_t>(countOf(layer)))},
			              {"current", Json::boolean(layer.current)}}));
	}
	auto count = static_cast<std::int64_t>(list.items().size());
	return Json::object({{"layers", std::move(list)}, {"count", Json::integer(count)}});
}

Json classes(const Json &args, Drawing &drawing, ToolStatus &status) {
	args::Reader reader(args, kClassesSpec.name, status);
	reader.onlyKnown({});
	if (!reader.ok()) {
		return Json();
	}
	if (!drawing.open()) {
		args::noDocument(status);
		return Json();
	}
	Json list = Json::array();
	for (std::string &name : drawing.classes()) {
		list.push(Json::string(std::move(name)));
	}
	auto count = static_cast<std::int64_t>(list.items().size());
	return Json::object({{"classes", std::move(list)}, {"count", Json::integer(count)}});
}

Json layerObjects(const Json &args, Drawing &drawing, ToolStatus &status) {
	args::Reader reader(args, kLayerObjectsSpec.name, status);
	reader.onlyKnown({"layer", "limit", "offset", "type"});
	std::string layerName = reader.requiredString("layer");
	std::int64_t limit = reader.integer("limit", kMinLimit, kMaxLimit).value_or(kDefaultLimit);
	std::int64_t offset = reader.integer("offset", 0, kMaxIntegerArg).value_or(0);
	std::optional<std::int64_t> type = reader.integer("type", 0, kMaxIntegerArg);
	if (!reader.ok()) {
		return Json();
	}
	if (!drawing.open()) {
		args::noDocument(status);
		return Json();
	}
	// 名前で 1 枚を探し、そのレイヤの直下だけを読む（頁ごとに図面全体を辿らない）。
	std::size_t at = findLayer(drawing.layerNames(), layerName, kLayerObjectsSpec.name, status);
	if (at == kNoLayer) {
		return Json();
	}
	LayerFacts found = drawing.layer(at);
	// 絞ったあとの並び（列の中の位置）。
	const std::vector<std::int16_t> &types = found.memberTypes;
	std::vector<std::size_t> members;
	for (std::size_t i = 0; i < types.size(); ++i) {
		if (counted(types[i]) && (!type || types[i] == *type)) {
			members.push_back(i);
		}
	}
	auto first = static_cast<std::size_t>(offset);
	Json objects = Json::array();
	for (std::size_t i = first; i < members.size() && i - first < static_cast<std::size_t>(limit);
	     ++i) {
		std::int16_t memberType = types[members[i]];
		ObjectFacts facts = drawing.object(at, members[i]);
		Json pio;
		if (memberType == kParametricNode) {
			pio = facts.pioName ? Json::string(*facts.pioName) : Json();
		}
		objects.push(Json::object({{"index", Json::integer(static_cast<std::int64_t>(i))},
		                           {"type", Json::integer(memberType)},
		                           {"type_name", typeNameJson(memberType)},
		                           {"pio_name", std::move(pio)},
		                           {"name", Json::string(std::move(facts.name))},
		                           {"class", Json::string(std::move(facts.className))},
		                           {"bounds", boundsJson(facts.boundsOk, facts.left, facts.top,
		                                                 facts.right, facts.bottom)}}));
	}
	auto returned = static_cast<std::int64_t>(objects.items().size());
	return Json::object({{"layer", Json::string(layerName)},
	                     {"unit", Json::string("mm")},
	                     {"objects", std::move(objects)},
	                     {"returned", Json::integer(returned)},
	                     {"total", Json::integer(static_cast<std::int64_t>(members.size()))}});
}

Json objectCounts(const Json &args, Drawing &drawing, ToolStatus &status) {
	args::Reader reader(args, kObjectCountsSpec.name, status);
	reader.onlyKnown({"layer"});
	std::optional<std::string> layerName = reader.string("layer");
	if (!reader.ok()) {
		return Json();
	}
	if (!drawing.open()) {
		args::noDocument(status);
		return Json();
	}
	std::vector<LayerFacts> chosen;
	if (layerName) {
		std::size_t at =
		    findLayer(drawing.layerNames(), *layerName, kObjectCountsSpec.name, status);
		if (at == kNoLayer) {
			return Json();
		}
		chosen.push_back(drawing.layer(at));
	} else {
		chosen = allLayers(drawing);
	}
	// 種別番号の昇順に並べる。
	std::map<std::int16_t, std::int64_t> counts;
	std::int64_t total = 0;
	for (const LayerFacts &layer : chosen) {
		for (std::int16_t type : layer.memberTypes) {
			if (counted(type)) {
				++counts[type];
				++total;
			}
		}
	}
	Json types = Json::array();
	for (const auto &[type, count] : counts) {
		types.push(Json::object({{"type", Json::integer(type)},
		                         {"type_name", typeNameJson(type)},
		                         {"count", Json::integer(count)}}));
	}
	Json result = Json::object();
	if (layerName) {
		result.set("layer", Json::string(*layerName));
	}
	result.set("types", std::move(types));
	result.set("total", Json::integer(total));
	return result;
}

}
