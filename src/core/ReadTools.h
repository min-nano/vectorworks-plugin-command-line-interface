#pragma once

// 読む道具（layers・classes・layer_objects・object_counts）のうち SDK に依らない部分
// （docs/plugin/tools.md「読む道具」）。引数の検査・no_document の判定・数え方・type_name の表・
// 絞り込みと頁送り・結果の形をここに置き、SDK から読む値は Drawing（src/tools/ReadTools.cpp が
// SDK で作る）から受け取るので、無 SDK でテストできる。

#include "core/Json.h"
#include "core/Serve.h"

#include <cstddef>
#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core {

// 道具の表の行（種類を含む。src/tools/ToolTable.cpp はこれを使う）。どれも図面を変えない Read。
extern const ToolSpec kLayersSpec;
extern const ToolSpec kClassesSpec;
extern const ToolSpec kLayerObjectsSpec;
extern const ToolSpec kObjectCountsSpec;

// GetObjectTypeN の番号のうち、数えるときに除くもの（Objs.TDType.h）。
inline constexpr std::int16_t kTermNode = 0;
inline constexpr std::int16_t kUndoPlaceholderNode = 90;
inline constexpr std::int16_t kParametricNode = 86;

// ovLayerType の値（MiniCadCallBacks.h の ELayerType。Findings「Layers and Stories」）。
inline constexpr std::int16_t kLayerDesign = 1;
inline constexpr std::int16_t kLayerSheet = 2;
inline constexpr std::int16_t kGuestLayerDesign = 3;

// LayerFacts は SDK から読むレイヤ 1 枚（GetDrawingHeader の列のうち型が kLayerNode のもの）。
struct LayerFacts {
	// GetObjectName。参照で入ったレイヤは NNA#1_<元の名前>。
	std::string name;
	// ovLayerType。GetObjectVariable が偽なら空。
	std::optional<std::int16_t> layerType;
	// GetLayerScaleN（分母）。
	double scale = 1;
	// GetCurrentLayer() とハンドルが一致するか。
	bool current = false;
	// FirstMemberObj → NextObject の列の型（GetObjectTypeN）。終端（0）と undo の置き石（90）も
	// そのまま入れる（除くのは core）。
	std::vector<std::int16_t> memberTypes;
};

// ObjectFacts は SDK から読む図形 1 つ。
struct ObjectFacts {
	// VWParametricObj::GetParametricName。PIO（型 86）のときだけ読む。
	std::optional<std::string> pioName;
	// GetObjectName。
	std::string name;
	// GetObjectClass → ClassIDToName。
	std::string className;
	// GetObjectBounds の戻り値と外形（WorldRect の 4 つの値のまま）。
	bool boundsOk = false;
	double left = 0;
	double top = 0;
	double right = 0;
	double bottom = 0;
};

// Drawing は前面の図面から読む口。道具 1 回の中でだけ使う（ハンドルは刻みをまたいで持たない）。
class Drawing {
public:
	virtual ~Drawing() = default;
	// open は前面の図面があるか（GetCurrentLayer() が nullptr でないか）。
	virtual bool open() = 0;
	// layers は前面の図面のレイヤを列の順（背面→前面）に返す。
	virtual std::vector<LayerFacts> layers() = 0;
	// classes は前面の図面のクラスの名前（ForEachClass(false, …)。ID の順）。
	virtual std::vector<std::string> classes() = 0;
	// object は、直前の layers() の layer 番目のレイヤの、列の member 番目の図形を読む。
	virtual ObjectFacts object(std::size_t layer, std::size_t member) = 0;
};

// typeName は type_name の表（docs/plugin/tools.md「type_name の表」が真実）。表に無い番号は空。
std::optional<std::string_view> typeName(std::int16_t type);

// counted は数える図形か（終端と undo の置き石でない）。
constexpr bool counted(std::int16_t type) {
	return type != kTermNode && type != kUndoPlaceholderNode;
}

// boundsJson は bounds の値。ok が偽か、4 つのどれかが有限でなければ null。有限かは最小と最大を
// とる前に確かめる（std::min は NaN を落とすため）。left ≤ right・bottom ≤ top にして入れる。
Json boundsJson(bool ok, double left, double top, double right, double bottom);

Json layers(const Json &args, Drawing &drawing, ToolStatus &status);
Json classes(const Json &args, Drawing &drawing, ToolStatus &status);
Json layerObjects(const Json &args, Drawing &drawing, ToolStatus &status);
Json objectCounts(const Json &args, Drawing &drawing, ToolStatus &status);

}
