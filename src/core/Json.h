//
//	core/Json.h
//
//	**最小の JSON。** SDK に依らず、値の組み立て・書き出し・読み取りだけを持つ。用途は
//	受け渡しの作法（docs/protocol.md）の要求と応答の器である。
//
//	【なぜ自前か】この 1 用途のために外部ライブラリを足すと、無 SDK でどこでもビルドできる
//	共通部（VwCliCore）に外部の都合が入り込む。要るのは素直な JSON を読み書きすることだけで
//	ある。
//
//	【並び】オブジェクトのメンバは**入れた順のまま**持ち、その順で書き出す。応答の並び
//	（素性 → 中身）を道具の作者が決められるようにするため。引きは線形探索で、扱う要素は
//	数十なので足りる（解析のときだけは重複の判定に表を使う。Json.cpp）。
//
//	【外から来たものを読む】要求は**プラグインの外**（別のプロセス）が書いたテキストである。
//	壊れた入力・悪意ある入力で Vectorworks ごと落ちないことが要件なので:
//	  * 入れ子の深さが kJsonMaxDepth を超えたら、再帰を降りずに失敗する（スタックを
//	    溢れさせない。docs/protocol.md「要求」）。
//	  * 文法は RFC 8259 のとおりに厳しく読む（呼ぶ側の JSON の読み手と、受け付ける要求を
//	    揃える）。テキストは UTF-8 でなければならない。
//	  * 失敗は例外ではなく戻り値と理由の文字列で返す。
//
//	【数値】書き出しも読み取りもロケールに依らない（std::locale::classic）。小数点がカンマの
//	環境で壊れた JSON を出さないため。
//

#pragma once

#include <string>
#include <utility>
#include <vector>

namespace VwCli::core {
// 入れ子（オブジェクトと配列）の深さの上限。外側の値を 1 と数える。作法の上限と同じ値
// （docs/protocol.md「要求」。core/Bridge.h の kBridgeMaxNestingDepth）。
inline constexpr int kJsonMaxDepth = 64;

// JSON の値 1 つ。null / 真偽 / 数 / 文字列 / 配列 / オブジェクトのいずれか。
class Json {
public:
	enum class Kind {
		Null,
		Bool,
		Number,
		String,
		Array,
		Object,
	};

	Json() = default;

	// --- 作る ----------------------------------------------------------
	static Json null();
	static Json boolean(bool value);
	static Json number(double value);
	static Json integer(long long value);
	static Json string(std::string value);
	static Json array();
	static Json object();

	// --- 見る ----------------------------------------------------------
	Kind kind() const {
		return fKind;
	}
	bool isNull() const {
		return fKind == Kind::Null;
	}
	bool isBool() const {
		return fKind == Kind::Bool;
	}
	bool isNumber() const {
		return fKind == Kind::Number;
	}
	bool isString() const {
		return fKind == Kind::String;
	}
	bool isArray() const {
		return fKind == Kind::Array;
	}
	bool isObject() const {
		return fKind == Kind::Object;
	}

	// 型が違えば既定値を返す。型を問う必要があるところ（引数の検査）は is*() で確かめる。
	bool asBool(bool fallback = false) const;
	double asNumber(double fallback = 0.0) const;
	std::string asString(const std::string& fallback = std::string()) const;

	// 配列の要素（配列でなければ空）。
	const std::vector<Json>& items() const {
		return fItems;
	}

	// オブジェクトのメンバ（入れた順）。
	const std::vector<std::pair<std::string, Json>>& members() const {
		return fMembers;
	}

	// オブジェクトのメンバ 1 つ。**無ければ null を返す**。
	const Json& at(const std::string& key) const;
	bool has(const std::string& key) const;

	// --- 足す ----------------------------------------------------------
	// 配列へ 1 つ（配列でなければ何もしない）。
	void push(Json value);
	// オブジェクトへ 1 つ。**同じ鍵が既にあれば置き換える**（並びは最初に入れた位置の
	// まま）。オブジェクトでなければ何もしない。
	void set(std::string key, Json value);

	// --- 書き出す・読み取る --------------------------------------------
	// 1 行の JSON テキスト（整形しない。読むのは機械）。
	std::string dump() const;

	// 読み取り。成功したら true。失敗したら out は変えず、error に理由が入る。
	static bool parse(const std::string& text, Json& out, std::string& error);

	// 型と値が同じか。オブジェクトはメンバの並びを問わない（テストで応答を比べるため）。
	bool equals(const Json& other) const;

private:
	// 解析器はメンバへ直に足す（重複の判定を表で済ませるため。Json.cpp）。
	friend class JsonParser;

	Kind fKind = Kind::Null;
	bool fBool = false;
	double fNumber = 0.0;
	std::string fString;
	std::vector<Json> fItems;
	std::vector<std::pair<std::string, Json>> fMembers;
};

// JSON の文字列リテラル 1 つ分（引用符と傍線を含む）。
std::string jsonQuote(const std::string& text);

// UTF-8 として正しいか（RFC 3629。冗長な表現・サロゲート・U+10FFFF を超えるものは誤り）。
bool isValidUtf8(const std::string& text);
} // namespace VwCli::core
