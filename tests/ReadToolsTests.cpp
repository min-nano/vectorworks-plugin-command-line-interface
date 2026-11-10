#include "TestSupport.h"

#include "core/ReadTools.h"

#include <cmath>
#include <cstddef>
#include <cstdint>
#include <initializer_list>
#include <limits>
#include <string>
#include <utility>
#include <vector>

// 読む道具のうち SDK に依らない部分（docs/plugin/tools.md「読む道具」）。

using VwCli::core::Drawing;
using VwCli::core::ErrorCode;
using VwCli::core::Json;
using VwCli::core::LayerFacts;
using VwCli::core::ObjectFacts;
using VwCli::core::ToolKind;
using VwCli::core::ToolStatus;
using VwCli::test::must;

namespace {

constexpr double kNaN = std::numeric_limits<double>::quiet_NaN();
constexpr double kInfinity = std::numeric_limits<double>::infinity();

// FakeDrawing は SDK の代わりに決まった図面を返す。object を呼んだ位置を覚える。
class FakeDrawing : public Drawing {
public:
	bool isOpen = true;
	std::vector<LayerFacts> layerList;
	std::vector<std::string> classList;
	std::vector<std::pair<std::size_t, std::size_t>> read;
	bool layersCalled = false;

	bool open() override { return isOpen; }
	std::vector<LayerFacts> layers() override {
		layersCalled = true;
		return layerList;
	}
	std::vector<std::string> classes() override { return classList; }
	ObjectFacts object(std::size_t layer, std::size_t member) override {
		read.emplace_back(layer, member);
		ObjectFacts facts;
		facts.name = "o" + std::to_string(layer) + "-" + std::to_string(member);
		facts.className = "一般";
		facts.boundsOk = true;
		facts.left = 10;
		facts.top = 0;
		facts.right = 0;
		facts.bottom = 20;
		if (layerList[layer].memberTypes[member] == VwCli::core::kParametricNode) {
			facts.pioName = "Door";
		}
		return facts;
	}
};

// layer はレイヤ 1 枚。type が負なら ovLayerType を読めなかったとする。
LayerFacts layer(std::string name, int type, double scale, std::vector<std::int16_t> members,
                 bool current = false, bool referenced = false) {
	std::optional<std::int16_t> layerType;
	if (type >= 0) {
		layerType = static_cast<std::int16_t>(type);
	}
	return LayerFacts{std::move(name), layerType, referenced, scale, current, std::move(members)};
}

// sample は見本の図面（Findings「Layers and Stories」の列の並び）: レイヤの参照で入ったレイヤ
// （列の先頭。元の名前・ovLayerType 1・中身が直下に見える）・デザイン（1:1。置き石と終端を含む）・
// シート・リソースの参照で入ったレイヤ（NNA#1_・ovLayerType 3・直下は終端だけ）・種類の分からない
// レイヤ。
FakeDrawing sample() {
	FakeDrawing drawing;
	drawing.layerList = {
	    layer("RF", 1, 100, {3, 2, 0}, false, true),
	    layer("1F", 1, 1, {2, 90, 3, 86, 2, 11, 0}, true),
	    layer("S-1", 2, 1, {122, 0}),
	    layer("NNA#1_2F", 3, 50, {0}, false, true),
	    layer("X", 7, 100, {10, 0}),
	};
	drawing.classList = {"一般", "寸法", "壁-外"};
	return drawing;
}

Json call(Json (*tool)(const Json &, Drawing &, ToolStatus &), std::string_view args,
          Drawing &drawing, ToolStatus &status) {
	return tool(must(args), drawing, status);
}

}

// 種類は src/tools/ToolTable.cpp がこの定数を使う。読む道具はどれも占有に照らさない Read。
TEST(ReadToolsAreReadKind) {
	CHECK(VwCli::core::kLayersSpec.kind == ToolKind::Read);
	CHECK(VwCli::core::kClassesSpec.kind == ToolKind::Read);
	CHECK(VwCli::core::kLayerObjectsSpec.kind == ToolKind::Read);
	CHECK(VwCli::core::kObjectCountsSpec.kind == ToolKind::Read);
	for (const auto *spec : {&VwCli::core::kLayersSpec, &VwCli::core::kClassesSpec,
	                         &VwCli::core::kLayerObjectsSpec, &VwCli::core::kObjectCountsSpec}) {
		CHECK(must(spec->inputSchema).isObject());
	}
	CHECK_EQ(std::string(VwCli::core::kLayerObjectsSpec.name), std::string("layer_objects"));
}

TEST(LayersListsEveryLayerInListOrder) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layers, "{}", drawing, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(
	    result.dump(),
	    R"({"layers":[)"
	    R"({"name":"RF","kind":"design","referenced":true,"scale":100,"objects":2,"current":false},)"
	    R"({"name":"1F","kind":"design","referenced":false,"scale":1,"objects":5,"current":true},)"
	    R"({"name":"S-1","kind":"sheet","referenced":false,"scale":1,"objects":1,"current":false},)"
	    R"({"name":"NNA#1_2F","kind":"design","referenced":true,"scale":50,"objects":0,"current":false},)"
	    R"({"name":"X","kind":null,"referenced":false,"scale":100,"objects":1,"current":false}],"count":5})");
}

// include_sheets が偽ならシートだけを除く（kind が null のものは除かない）。
TEST(LayersCanLeaveOutSheets) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layers, R"({"include_sheets":false})", drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("count"), Json::integer(4));
	for (const Json &item : result.find("layers")->items()) {
		CHECK(!(*item.find("kind") == Json::string("sheet")));
	}
}

TEST(LayersWritesAScaleThatIsNotFiniteAsNull) {
	FakeDrawing drawing;
	drawing.layerList = {layer("a", 1, kNaN, {0}), layer("b", 1, 2.5, {0}),
	                     layer("c", -1, kInfinity, {0})};
	ToolStatus status;
	Json result = call(VwCli::core::layers, "{}", drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	const auto &items = result.find("layers")->items();
	REQUIRE(items.size() == 3);
	CHECK(items[0].find("scale")->isNull());
	CHECK_EQ(items[1].find("scale")->numberText(), std::string("2.5"));
	CHECK(items[2].find("scale")->isNull());
	CHECK(items[2].find("kind")->isNull());
}

TEST(LayersChecksArgumentsBeforeTheDocument) {
	FakeDrawing drawing = sample();
	drawing.isOpen = false;
	for (const char *args :
	     {R"({"include_sheets":1})", R"({"include_sheets":null})", R"({"sheets":true})"}) {
		ToolStatus status;
		Json result = call(VwCli::core::layers, args, drawing, status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(status.detail.isNull());
		CHECK(result.isNull());
	}
}

TEST(ReadToolsFailWithoutADocument) {
	using Tool = Json (*)(const Json &, Drawing &, ToolStatus &);
	const std::pair<Tool, const char *> calls[] = {
	    {VwCli::core::layers, "{}"},
	    {VwCli::core::classes, "{}"},
	    {VwCli::core::layerObjects, R"({"layer":"無いレイヤ"})"},
	    {VwCli::core::objectCounts, "{}"},
	    {VwCli::core::objectCounts, R"({"layer":"無いレイヤ"})"},
	};
	for (const auto &[tool, args] : calls) {
		FakeDrawing drawing = sample();
		drawing.isOpen = false;
		ToolStatus status;
		Json result = call(tool, args, drawing, status);
		CHECK(status.code == ErrorCode::Internal);
		CHECK_EQ(status.detail, must(R"({"reason":"no_document"})"));
		CHECK(result.isNull());
		// レイヤを確かめる前に返す（列を読まない）。
		CHECK(!drawing.layersCalled);
	}
}

TEST(ClassesListsNamesInIdOrder) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::classes, "{}", drawing, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result, must(R"({"classes":["一般","寸法","壁-外"],"count":3})"));
	ToolStatus refused;
	call(VwCli::core::classes, R"({"guest":true})", drawing, refused);
	CHECK(refused.code == ErrorCode::InvalidArgs);
}

// 終端と置き石を除き、index は絞ったあとの並びの位置。PIO だけが pio_name を持つ。
TEST(LayerObjectsListsTheObjectsUnderTheLayer) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layerObjects, R"({"layer":"1F"})", drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("layer"), Json::string("1F"));
	CHECK_EQ(*result.find("unit"), Json::string("mm"));
	CHECK_EQ(*result.find("returned"), Json::integer(5));
	CHECK_EQ(*result.find("total"), Json::integer(5));
	const auto &objects = result.find("objects")->items();
	REQUIRE(objects.size() == 5);
	CHECK_EQ(objects[0],
	         must(R"({"index":0,"type":2,"type_name":"line","pio_name":null,"name":"o1-0",)"
	              R"("class":"一般","bounds":{"left":0,"right":10,"top":20,"bottom":0}})"));
	CHECK_EQ(*objects[2].find("type_name"), Json::string("parametric"));
	CHECK_EQ(*objects[2].find("pio_name"), Json::string("Door"));
	CHECK_EQ(*objects[2].find("index"), Json::integer(2));
	CHECK_EQ(*objects[4].find("type_name"), Json::string("group"));
	// 読んだのは置き石（列の 1 番目）と終端を除いた位置だけ。
	std::vector<std::pair<std::size_t, std::size_t>> expected = {
	    {1, 0}, {1, 2}, {1, 3}, {1, 4}, {1, 5}};
	CHECK(drawing.read == expected);
}

// レイヤの参照で入ったレイヤは、参照元の図形が直下に見えるので、そのまま返す（情報パレットも
// 「N 図形」に数える。Findings「Layers and Stories」の「参照で入ってきたレイヤ」）。
TEST(LayerObjectsListsTheContentsOfAReferencedLayer) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layerObjects, R"({"layer":"RF"})", drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("total"), Json::integer(2));
	const auto &objects = result.find("objects")->items();
	REQUIRE(objects.size() == 2);
	CHECK_EQ(*objects[0].find("type_name"), Json::string("box"));
	CHECK_EQ(*objects[1].find("type_name"), Json::string("line"));
}

TEST(LayerObjectsFiltersByTypeBeforePaging) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layerObjects, R"({"layer":"1F","type":2,"offset":1,"limit":5})",
	                   drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("returned"), Json::integer(1));
	CHECK_EQ(*result.find("total"), Json::integer(2));
	const auto &objects = result.find("objects")->items();
	REQUIRE(objects.size() == 1);
	CHECK_EQ(*objects[0].find("index"), Json::integer(1));
	CHECK_EQ(*objects[0].find("name"), Json::string("o1-4"));
}

TEST(LayerObjectsPagesWithLimitAndOffset) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::layerObjects, R"({"layer":"1F","offset":2.0,"limit":2e0})",
	                   drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	CHECK_EQ(*result.find("returned"), Json::integer(2));
	CHECK_EQ(*result.find("total"), Json::integer(5));
	const auto &objects = result.find("objects")->items();
	REQUIRE(objects.size() == 2);
	CHECK_EQ(*objects[0].find("index"), Json::integer(2));
	CHECK_EQ(*objects[1].find("index"), Json::integer(3));
}

// offset が total 以上なら空で成功し、表に無い番号の type も断らない。
TEST(LayerObjectsReturnsNothingPastTheEnd) {
	FakeDrawing drawing = sample();
	for (const char *args :
	     {R"({"layer":"1F","offset":5})", R"({"layer":"1F","offset":2147483647})",
	      R"({"layer":"1F","type":9999})", R"({"layer":"NNA#1_2F"})"}) {
		ToolStatus status;
		Json result = call(VwCli::core::layerObjects, args, drawing, status);
		REQUIRE(status.code == ErrorCode::None);
		CHECK(result.find("objects")->items().empty());
		CHECK_EQ(*result.find("returned"), Json::integer(0));
	}
	ToolStatus status;
	Json result = call(VwCli::core::layerObjects, R"({"layer":"1F","type":9999})", drawing, status);
	CHECK_EQ(*result.find("total"), Json::integer(0));
}

TEST(LayerObjectsRejectsBadArguments) {
	for (const char *args : {
	         R"({})",
	         R"({"layer":1})",
	         R"({"layer":"1F","limit":0})",
	         R"({"layer":"1F","limit":1001})",
	         R"({"layer":"1F","limit":2.5})",
	         R"({"layer":"1F","limit":"2"})",
	         R"({"layer":"1F","offset":-1})",
	         R"({"layer":"1F","offset":1e30})",
	         R"({"layer":"1F","offset":2147483648})",
	         R"({"layer":"1F","offset":2.0000000000000001})",
	         R"({"layer":"1F","type":-2})",
	         R"({"layer":"1F","type":null})",
	         R"({"layer":"1F","depth":1})",
	     }) {
		// 図面が無くても、引数の誤りを先に返す。
		FakeDrawing drawing = sample();
		drawing.isOpen = false;
		ToolStatus status;
		Json result = call(VwCli::core::layerObjects, args, drawing, status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(result.isNull());
	}
}

// 名前は完全に一致するものだけ（参照で入ったレイヤは接頭辞ごと渡す）。
// 範囲と型の誤りの文言（「以下」のあとに空白を入れない）。
TEST(LayerObjectsDescribesTheRange) {
	FakeDrawing drawing = sample();
	ToolStatus range;
	call(VwCli::core::layerObjects, R"({"layer":"1F","offset":1e400})", drawing, range);
	CHECK(range.message.find("offset は 0 以上 2147483647 以下です（1e400）") != std::string::npos);
	ToolStatus type;
	call(VwCli::core::layerObjects, R"({"layer":"1F","limit":"2"})", drawing, type);
	CHECK(type.message.find("limit は 1 以上 1000 以下の整数です") != std::string::npos);
}

TEST(LayerObjectsRejectsALayerThatDoesNotExist) {
	for (const char *name : {"2F", "1f", "1F ", ""}) {
		FakeDrawing drawing = sample();
		ToolStatus status;
		Json args = Json::object({{"layer", Json::string(name)}});
		Json result = VwCli::core::layerObjects(args, drawing, status);
		CHECK(status.code == ErrorCode::InvalidArgs);
		CHECK(status.detail.isNull());
		CHECK(result.isNull());
	}
}

TEST(ObjectCountsCountsTheWholeDrawingByType) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::objectCounts, "{}", drawing, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(result.dump(), R"({"types":[{"type":2,"type_name":"line","count":3},)"
	                        R"({"type":3,"type_name":"box","count":2},)"
	                        R"({"type":10,"type_name":"text","count":1},)"
	                        R"({"type":11,"type_name":"group","count":1},)"
	                        R"({"type":86,"type_name":"parametric","count":1},)"
	                        R"({"type":122,"type_name":"viewport","count":1}],"total":9})");
}

// 図面全体は layers のすべてのレイヤの objects の和。
TEST(ObjectCountsTotalIsTheSumOfLayerObjects) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json counts = call(VwCli::core::objectCounts, "{}", drawing, status);
	Json listed = call(VwCli::core::layers, "{}", drawing, status);
	REQUIRE(status.code == ErrorCode::None);
	std::int64_t sum = 0;
	for (const Json &item : listed.find("layers")->items()) {
		sum += item.find("objects")->asInt64().value_or(-1000);
	}
	CHECK_EQ(counts.find("total")->asInt64().value_or(-1), sum);
}

TEST(ObjectCountsCountsOneLayer) {
	FakeDrawing drawing = sample();
	ToolStatus status;
	Json result = call(VwCli::core::objectCounts, R"({"layer":"S-1"})", drawing, status);
	CHECK(status.code == ErrorCode::None);
	CHECK_EQ(
	    result.dump(),
	    R"({"layer":"S-1","types":[{"type":122,"type_name":"viewport","count":1}],"total":1})");
	ToolStatus missing;
	call(VwCli::core::objectCounts, R"({"layer":"無い"})", drawing, missing);
	CHECK(missing.code == ErrorCode::InvalidArgs);
	ToolStatus bad;
	call(VwCli::core::objectCounts, R"({"layer":["S-1"]})", drawing, bad);
	CHECK(bad.code == ErrorCode::InvalidArgs);
}

TEST(TypeNameFollowsTheTable) {
	struct Row {
		std::int16_t type;
		const char *name;
	};
	const Row table[] = {
	    {2, "line"},     {3, "box"},    {4, "oval"},      {5, "polygon"},     {6, "arc"},
	    {10, "text"},    {11, "group"}, {15, "symbol"},   {17, "locus"},      {21, "polyline"},
	    {24, "extrude"}, {68, "wall"},  {84, "csg_tree"}, {86, "parametric"}, {122, "viewport"},
	};
	for (const Row &row : table) {
		CHECK_EQ(std::string(VwCli::core::typeName(row.type).value_or("<none>")),
		         std::string(row.name));
	}
	for (std::int16_t type : std::initializer_list<std::int16_t>{0, 1, 16, 31, 90, 127, -128}) {
		CHECK(!VwCli::core::typeName(type));
	}
}

TEST(BoundsOrdersTheSides) {
	CHECK_EQ(VwCli::core::boundsJson(true, 1250, 100, 2366.025, 1033.013).dump(),
	         std::string(R"({"left":1250,"right":2366.025,"top":1033.013,"bottom":100})"));
	CHECK_EQ(VwCli::core::boundsJson(true, 5, 7, -5, -7).dump(),
	         std::string(R"({"left":-5,"right":5,"top":7,"bottom":-7})"));
}

// 偽を返したか、4 つのどれかが有限でなければ null（NaN が最小・最大の後ろの引数でも）。
TEST(BoundsIsNullUnlessAllSidesAreFinite) {
	CHECK(VwCli::core::boundsJson(false, 0, 1, 1, 0).isNull());
	CHECK(VwCli::core::boundsJson(true, kNaN, 1, 1, 0).isNull());
	CHECK(VwCli::core::boundsJson(true, 0, kNaN, 1, 0).isNull());
	CHECK(VwCli::core::boundsJson(true, 0, 1, kNaN, 0).isNull());
	CHECK(VwCli::core::boundsJson(true, 0, 1, 1, kNaN).isNull());
	CHECK(VwCli::core::boundsJson(true, 0, 1, kInfinity, 0).isNull());
	CHECK(VwCli::core::boundsJson(true, -kInfinity, 1, 1, 0).isNull());
}
