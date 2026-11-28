#pragma once

// 要求と応答を読み書きするための小規模な JSON（docs/plugin/bridge.md「分割の方針」）。
//
// 読み取りの方法は Go の encoding/json に合わせる（CLI と擬似プラグインがそれで読み書きするため、
// 受け付ける要求の範囲を統一するため。docs/protocol.md「要求」）。
//
//   - 入れ子（オブジェクトと配列）の深さが上限を超えたら、再帰を降りずに失敗する。深い入れ子で
//     スタックを溢れさせ、Vectorworks ごと異常終了させないための関門。
//   - 文字列の中の不正な UTF-8 と、対にならないサロゲートの \u エスケープは U+FFFD に置換する。
//     書き出す文字列も同様にするため、応答は常に UTF-8 となる。
//   - オブジェクトの同一のキーは、後のものを使用する。
//   - 数は綴りのまま保持する（丸めず、書き出しも同じ綴り）。整数の引数は綴りではなく値で読み取り
//     （integerValue）、ツールの結果の実数は Go の encoding/json と同じ綴りで生成する（real）。
//     規則は docs/plugin/tools.md「共通の規則」。

#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <utility>
#include <variant>
#include <vector>

namespace VwCli::core {

// 整数の引数が受け付ける最大の値（int32 の最大）。ツールごとに範囲を定めていなくても、整数の
// 引数は 0 以上この値以下に限定する（docs/plugin/tools.md「共通の規則」）。
inline constexpr std::int64_t kMaxIntegerArg = 2147483647;

// IntegerCheck は数を整数の引数として読み取った結果。
enum class IntegerCheck {
	// 0 以上 kMaxIntegerArg 以下の整数
	Integer,
	// 小数部がある（範囲外でもある場合もこちら）
	Fraction,
	// 整数だが、負か kMaxIntegerArg を超える
	OutOfRange,
};

struct IntegerValue {
	IntegerCheck check = IntegerCheck::OutOfRange;
	// check が Integer の場合のみ意味を持つ。
	std::int64_t value = 0;
};

class Json {
public:
	enum class Type { Null, Bool, Number, String, Array, Object };
	using Array = std::vector<Json>;
	using Member = std::pair<std::string, Json>;
	// キーは重複しない（parse と set が保証する）。順序は読み取った順・追加した順。
	using Object = std::vector<Member>;

	// null。
	Json() = default;

	static Json boolean(bool value);
	static Json integer(std::int64_t value);
	// real は実数を、元の値に戻る最短の桁で、Go の encoding/json と同じ表記にした数にする
	// （0 と 1e-6 ≤ |x| < 1e21 は固定小数点、それ以外は符号付きで先頭の 0 の無い指数。-0 は -0）。
	// 有限でなければ null を返す（JSON に記述できないため。ツールの結果の実数は全てこれで
	// 生成する）。
	static Json real(double value);
	static Json string(std::string value);
	static Json array(Array items = {});
	static Json object(Object members = {});

	// parse は 1 つの JSON のテキストを読み取る。読み取れなければ空で、error に理由（日本語）を
	// 書き込む。maxDepth は入れ子の深さの上限（最も外側のオブジェクトか配列を 1 と数える）。
	static std::optional<Json> parse(std::string_view text, int maxDepth,
	                                 std::string *error = nullptr);

	Type type() const;
	bool isNull() const { return type() == Type::Null; }
	bool isBool() const { return type() == Type::Bool; }
	bool isNumber() const { return type() == Type::Number; }
	bool isString() const { return type() == Type::String; }
	bool isArray() const { return type() == Type::Array; }
	bool isObject() const { return type() == Type::Object; }

	// 型の異なる値で呼ぶと std::bad_variant_access を送出する（呼ぶ前に型を確認する）。
	bool asBool() const;
	const std::string &asString() const;
	// 数の綴り（JSON の数の文法を満たす）。
	const std::string &numberText() const;
	// 数が整数の綴り（小数部と指数部が無い）で、std::int64_t に収まる場合のみ値を返す。
	std::optional<std::int64_t> asInt64() const;
	// integerValue は数の値を、記述された 10 進の通りに正確に読み取る（浮動小数点数に変換しない）。
	// 値が整数ならば表記に関わらず受け付ける（2・2.0・2e0・-0・0e99999999）。指数は飽和させて
	// 数えるため、長い指数でも溢れず、長時間を要しない。
	IntegerValue integerValue() const;
	const Array &items() const;
	const Object &members() const;

	// find はオブジェクトのキーの値を返す。存在しない・オブジェクトでなければ nullptr。
	const Json *find(std::string_view key) const;
	// set はオブジェクトのキーに値を設定する（存在すれば置換する）。オブジェクトでなければ例外を
	// 送出する。
	void set(std::string key, Json value);
	// push は配列の末尾に追加する。配列でなければ例外を送出する。
	void push(Json value);

	// dump は空白を挿入せずに書き出す。
	std::string dump() const;

	// オブジェクトはキーの順序に依存せずに比較する。数は綴りで比較する。
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
