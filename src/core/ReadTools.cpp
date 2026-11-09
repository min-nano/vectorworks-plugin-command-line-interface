#include "core/ReadTools.h"

#include "core/Args.h"

#include <algorithm>
#include <cmath>
#include <map>

namespace VwCli::core {

const ToolSpec kLayersSpec{
    "layers",
    "前面の図面のレイヤ（デザインとシート）を、列の順（背面→前面）に返す。名前・種類・参照で入った"
    "か・"
    "縮尺の分母・直下の図形の数・現在のレイヤか",
    R"({"type":"object","properties":{"include_sheets":{"type":"boolean","default":true,)"
    R"("description":"偽ならシートレイヤを除く"}},"additionalProperties":false})",
    ToolKind::Read};

const ToolSpec kClassesSpec{
    "classes",
    "前面の図面のクラスの名前を、ID の順（作った順）に返す（参照している図面のクラスは含めない）",
    R"({"type":"object","properties":{},"additionalProperties":false})", ToolKind::Read};

const ToolSpec kLayerObjectsSpec{
    "layer_objects",
    "前面の図面のレイヤの直下の図形を、背面→前面の順に返す（種別・名前・クラス・外形（mm））。"
    "type で種別を絞り、offset と limit で頁を送る",
    R"({"type":"object","properties":{)"
    R"("layer":{"type":"string","description":"レイヤの名前（layers の name）"},)"
    R"("limit":{"type":"integer","minimum":1,"maximum":1000,"default":50},)"
    R"("offset":{"type":"integer","minimum":0,"maximum":2147483647,"default":0},)"
    R"("type":{"type":"integer","minimum":0,"maximum":2147483647,)"
    R"("description":"GetObjectTypeN の種別の番号。省くとすべての種別"}},)"
    R"("required":["layer"],"additionalProperties":false})",
    ToolKind::Read};

const ToolSpec kObjectCountsSpec{
    "object_counts",
    "前面の図面（layer を渡せばそのレイヤ）の直下の図形の数を、種別ごとに番号の昇順で返す",
    R"({"type":"object","properties":{)"
    R"("layer":{"type":"string","description":"レイヤの名前（layers の name）。省くと図面全体"}},)"
    R"("additionalProperties":false})",
    ToolKind::Read};

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

// findLayer は名前が完全に一致するレイヤ。無ければ invalid_args で失敗し、nullptr を返す。
const LayerFacts *findLayer(const std::vector<LayerFacts> &layers, const std::string &name,
                            std::string_view tool, ToolStatus &status) {
	for (const LayerFacts &layer : layers) {
		if (layer.name == name) {
			return &layer;
		}
	}
	args::fail(status, ErrorCode::InvalidArgs,
	           std::string(tool) + " の引数が誤っています: レイヤ " + name + " がありません");
	return nullptr;
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
	for (const LayerFacts &layer : drawing.layers()) {
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
			              {"referenced", Json::boolean(layer.layerType == kGuestLayerDesign)},
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
	std::vector<LayerFacts> all = drawing.layers();
	const LayerFacts *found = findLayer(all, layerName, kLayerObjectsSpec.name, status);
	if (!found) {
		return Json();
	}
	auto at = static_cast<std::size_t>(found - all.data());
	// 絞ったあとの並び（列の中の位置）。
	const std::vector<std::int16_t> &types = found->memberTypes;
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
	std::vector<LayerFacts> all = drawing.layers();
	std::vector<const LayerFacts *> chosen;
	if (layerName) {
		const LayerFacts *found = findLayer(all, *layerName, kObjectCountsSpec.name, status);
		if (!found) {
			return Json();
		}
		chosen.push_back(found);
	} else {
		for (const LayerFacts &layer : all) {
			chosen.push_back(&layer);
		}
	}
	// 種別番号の昇順に並べる。
	std::map<std::int16_t, std::int64_t> counts;
	std::int64_t total = 0;
	for (const LayerFacts *layer : chosen) {
		for (std::int16_t type : layer->memberTypes) {
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
