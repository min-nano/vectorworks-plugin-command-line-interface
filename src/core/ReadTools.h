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
inline constexpr ToolSpec kLayersSpec{
    "layers",
    "前面の図面のレイヤ（デザインとシート）を、列の順（背面→前面）に返す。"
    "名前・種類・参照で入ったか・縮尺の分母・直下の図形の数・現在のレイヤか",
    R"({"type":"object","properties":{"include_sheets":{"type":"boolean","default":true,)"
    R"("description":"偽ならシートレイヤを除く"}},"additionalProperties":false})",
    ToolKind::Read};

inline constexpr ToolSpec kClassesSpec{
    "classes",
    "前面の図面のクラスの名前を、ID の順（作った順）に返す（参照している図面のクラスは含めない）",
    R"({"type":"object","properties":{},"additionalProperties":false})", ToolKind::Read};

inline constexpr ToolSpec kLayerObjectsSpec{
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

inline constexpr ToolSpec kObjectCountsSpec{
    "object_counts",
    "前面の図面（layer を渡せばそのレイヤ）の直下の図形の数を、種別ごとに番号の昇順で返す",
    R"({"type":"object","properties":{)"
    R"("layer":{"type":"string","description":"レイヤの名前（layers の name）。省くと図面全体"}},)"
    R"("additionalProperties":false})",
    ToolKind::Read};

// GetObjectTypeN の番号のうち、数えるときに除くもの（Objs.TDType.h）。
inline constexpr std::int16_t kTermNode = 0;
inline constexpr std::int16_t kUndoPlaceholderNode = 90;
inline constexpr std::int16_t kParametricNode = 86;

// 失敗の detail の reason（docs/plugin/tools.md）。layer に一致するレイヤが 2 枚以上。
inline constexpr std::string_view kLayerAmbiguous = "layer_ambiguous";

// ovLayerType の値（MiniCadCallBacks.h の ELayerType。Findings「Layers and Stories」）。3
// はリソースの 参照で入ったデザインレイヤだけで、レイヤの参照で入ったものは 1（参照由来かは
// referenced で見る）。
inline constexpr std::int16_t kLayerDesign = 1;
inline constexpr std::int16_t kLayerSheet = 2;
inline constexpr std::int16_t kGuestLayerDesign = 3;

// LayerFacts は SDK から読むレイヤ 1 枚（GetDrawingHeader の列のうち型が kLayerNode のもの）。
struct LayerFacts {
	// GetObjectName。リソースの参照で入ったレイヤは
	// NNA#<n>_<元の名前>、レイヤの参照で入ったレイヤは 元の名前のまま。
	std::string name;
	// ovLayerType。GetObjectVariable が偽なら空。
	std::optional<std::int16_t> layerType;
	// IsObjectFromReferencedFile(layer)。参照で入ったレイヤなら、参照の方法によらず真（Findings
	// 「Layers and Stories」の「参照で入ってきたレイヤ」。SDK リファレンス #310）。
	bool referenced = false;
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
	// layerNames は前面の図面のレイヤの名前を列の順（背面→前面）に返す。直下の列は辿らない（1 枚の
	// レイヤを名前で探すときに、図面全体を辿らないため）。
	virtual std::vector<std::string> layerNames() = 0;
	// layer は、直前の layerNames() の index 番目のレイヤを、直下の列まで読む。
	virtual LayerFacts layer(std::size_t index) = 0;
	// classes は前面の図面のクラスの名前（ForEachClass(false, …)。ID の順）。
	virtual std::vector<std::string> classes() = 0;
	// object は、layer(index) で読んだレイヤの、列の member 番目の図形を読む。
	virtual ObjectFacts object(std::size_t index, std::size_t member) = 0;
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
