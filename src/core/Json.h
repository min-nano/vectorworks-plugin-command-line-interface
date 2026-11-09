#pragma once

// 要求と応答を読み書きするための小さな JSON（docs/plugin/bridge.md「分け方」）。
//
// 読み方は Go の encoding/json に合わせる（CLI と偽のプラグインがそれで読み書きするので、
// 受け付ける要求の範囲を揃えるため。docs/protocol.md「要求」）。
//
//   - 入れ子（オブジェクトと配列）の深さが上限を超えたら、再帰を降りずに失敗する。深い入れ子で
//     スタックを溢れさせ、Vectorworks ごと落とさないための関門。
//   - 文字列の中の不正な UTF-8 と、対にならないサロゲートの \u エスケープは U+FFFD に置き換える。
//     書き出す文字列も同じにするので、応答は常に UTF-8 になる。
//   - オブジェクトの同じキーは、後のものを使う。
//   - 数は綴りのまま持つ（丸めず、書き出しも同じ綴り）。整数の引数は綴りではなく値で読み
//     （integerValue）、道具の結果の実数は Go の encoding/json と同じ綴りで作る（real）。決まりは
//     docs/plugin/tools.md「最初に提供する道具」の冒頭。

#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

namespace VwCli::core {

// 整数の引数が受ける最大の値（int32 の最大）。道具ごとに範囲を決めていなくても、整数の引数は
// 0 以上この値以下に限る（docs/plugin/tools.md「最初に提供する道具」の冒頭）。
inline constexpr std::int64_t kMaxIntegerArg = 2147483647;

// IntegerCheck は数を整数の引数として読んだ結果。
enum class IntegerCheck {
	// 0 以上 kMaxIntegerArg 以下の整数
	Integer,
	// 小数部がある（範囲の外でもあるときもこちら）
	Fraction,
	// 整数だが、負か kMaxIntegerArg を超える
	OutOfRange,
};

struct IntegerValue {
	IntegerCheck check = IntegerCheck::OutOfRange;
	// check が Integer のときだけ意味を持つ。
	std::int64_t value = 0;
};

class Json {
public:
	enum class Type { Null, Bool, Number, String, Array, Object };
	using Array = std::vector<Json>;
	using Member = std::pair<std::string, Json>;
	// キーは重ならない（parse と set が保つ）。並びは読んだ順・足した順。
	using Object = std::vector<Member>;

	// null。
	Json() = default;

	static Json boolean(bool value);
	static Json integer(std::int64_t value);
	// real は実数を、元の値に戻る最も短い桁で、Go の encoding/json と同じ書き方にした数にする
	// （0 と 1e-6 ≤ |x| < 1e21 は固定小数点、それ以外は符号付きで先頭の 0 の無い指数。-0 は -0）。
	// 有限でなければ null を返す（JSON に書けないため。道具の結果の実数はどれもこれで作る）。
	static Json real(double value);
	static Json string(std::string value);
	static Json array(Array items = {});
	static Json object(Object members = {});

	// parse は JSON のテキスト 1 つを読む。読めなければ空で、error に理由（日本語）を書く。
	// maxDepth は入れ子の深さの上限（一番外のオブジェクトか配列を 1 と数える）。
	static std::optional<Json> parse(std::string_view text, int maxDepth,
	                                 std::string *error = nullptr);

	Type type() const;
	bool isNull() const { return type() == Type::Null; }
	bool isBool() const { return type() == Type::Bool; }
	bool isNumber() const { return type() == Type::Number; }
	bool isString() const { return type() == Type::String; }
	bool isArray() const { return type() == Type::Array; }
	bool isObject() const { return type() == Type::Object; }

	// 型の違う値で呼ぶと std::bad_variant_access を投げる（呼ぶ前に型を確かめる）。
	bool asBool() const;
	const std::string &asString() const;
	// 数の綴り（JSON の数の文法を満たす）。
	const std::string &numberText() const;
	// 数が整数の綴り（小数部と指数部が無い）で、std::int64_t に収まるときだけ値を返す。
	std::optional<std::int64_t> asInt64() const;
	// integerValue は数の値を、書かれた 10 進のとおりに正確に読む（浮動小数点数に直さない）。
	// 値が整数なら書き方によらず受ける（2・2.0・2e0・-0・0e99999999）。指数は飽和させて数えるので、
	// 長い指数でも溢れず、長く掛からない。
	IntegerValue integerValue() const;
	const Array &items() const;
	const Object &members() const;

	// find はオブジェクトのキーの値を返す。無い・オブジェクトでなければ nullptr。
	const Json *find(std::string_view key) const;
	// set はオブジェクトのキーに値を置く（あれば置き換える）。オブジェクトでなければ投げる。
	void set(std::string key, Json value);
	// push は配列の末尾に足す。配列でなければ投げる。
	void push(Json value);

	// dump は空白を入れずに書き出す。
	std::string dump() const;

	// オブジェクトはキーの並びに依らずに比べる。数は綴りで比べる。
	friend bool operator==(const Json &a, const Json &b);

private:
	struct Number {
		std::string text;
	};
	using Value = std::variant<std::monostate, bool, Number, std::string, Array, Object>;

	explicit Json(Value value) : value_(std::move(value)) {}
	void dumpTo(std::string &out) const;

	Value value_;

	friend class JsonParser;
};

}
