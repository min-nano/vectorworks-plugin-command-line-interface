#pragma once

// ツールの引数の検証と、ツールが返す失敗の形式・結果の共通の形式
// （docs/plugin/tools.md「共通の規則」）。SDK に依存しない。引数の検査はツールの中で行い
// （JSON Schema の検証器は保持しない）、未知の引数・型の相違・範囲外・小数部・必須の引数の欠落は
// invalid_args で返す。

#include "core/Json.h"
#include "core/Serve.h"

#include <cstddef>
#include <cstdint>
#include <initializer_list>
#include <optional>
#include <string>
#include <string_view>
#include <vector>

namespace VwCli::core::args {

// 失敗の detail の reason（呼び出し元が分岐に使用する綴り。docs/plugin/tools.md）。
inline constexpr std::string_view kNoDocument = "no_document";

// fail は status を失敗とする。detail は null（付与しない）かオブジェクト。
void fail(ToolStatus &status, ErrorCode code, std::string message, Json detail = Json());

// failWithReason は status を internal の失敗とし、detail に {"reason":<reason>} を付与する。
void failWithReason(ToolStatus &status, std::string_view reason, std::string message);

// noDocument は図面が存在しない場合の失敗（internal。reason は no_document）。
void noDocument(ToolStatus &status);

// findOne は items の内 match に合致するものが丁度 1 つならば、その位置を返す。存在しなければ
// invalid_args（文言は「<tool> の引数が誤っています: <missing()>」）、2 つ以上ならば internal
// （reason は ambiguousReason。文言は ambiguous(一致した数)）で失敗し、空を返す。名前で 1 つを
// 指定するツールが、同一の名前が 2 つ存在する場合に、通知せずにいずれかを選択しないための共通の
// 手順。文言は失敗する場合のみ構築する（missing・ambiguous は文言を返す関数）。
template <typename T, typename Match, typename Missing, typename Ambiguous>
std::optional<std::size_t> findOne(const std::vector<T> &items, Match match, std::string_view tool,
                                   Missing missing, std::string_view ambiguousReason,
                                   Ambiguous ambiguous, ToolStatus &status) {
	std::optional<std::size_t> found;
	int matches = 0;
	for (std::size_t i = 0; i < items.size(); ++i) {
		if (match(items[i])) {
			found = i;
			++matches;
		}
	}
	if (matches == 0) {
		fail(status, ErrorCode::InvalidArgs,
		     std::string(tool) + " の引数が誤っています: " + missing());
		return std::nullopt;
	}
	if (matches > 1) {
		failWithReason(status, ambiguousReason, ambiguous(matches));
		return std::nullopt;
	}
	return found;
}

// listResult は一覧を返すツールの結果 {"<key>": list, "count": 要素の数}
// （documents・layers・classes）。
Json listResult(std::string_view key, Json list);

// Reader は 1 つのツールの引数を読み取る。最初の誤りで status を invalid_args とし、以後の
// 読み取りは何もせずに既定の値を返す（最初に検出した誤りを返すため）。呼び出し元は読み取りの
// 終了後に ok() を確認する。
class Reader {
public:
	Reader(const Json &args, std::string_view tool, ToolStatus &status);

	// onlyKnown は、known（受け付ける引数の名前）に含まれないキーが存在すれば誤りとする。
	void onlyKnown(std::initializer_list<std::string_view> known);

	// boolean は真偽値の引数。存在しなければ fallback。
	bool boolean(std::string_view key, bool fallback);

	// integer は整数の引数（値で読み取る。Json::integerValue）。範囲は [min, max] で、max は
	// kMaxIntegerArg 以下。存在しなければ空。
	std::optional<std::int64_t> integer(std::string_view key, std::int64_t min, std::int64_t max);

	// string は文字列の引数。存在しなければ空。
	std::optional<std::string> string(std::string_view key);

	// requiredString は必須の文字列の引数。存在しなければ誤り（空の文字列を返す）。
	std::string requiredString(std::string_view key);

	bool ok() const { return status_.code == ErrorCode::None; }

private:
	// find は key の値。存在しなければ nullptr（null は値として返し、型の相違として拒否する）。
	const Json *find(std::string_view key) const { return args_.find(key); }
	void invalid(std::string message);

	const Json &args_;
	std::string tool_;
	ToolStatus &status_;
};

}
