#pragma once

// 道具の引数の確かめと、道具が返す失敗の形（docs/plugin/tools.md「最初に提供する道具」の冒頭）。
// SDK に依らない。引数の検査は道具の中で行い（JSON Schema の検証器は持たない）、知らない引数・
// 型の違い・範囲の外・小数部・必須の欠けは invalid_args で返す。

#include "core/Json.h"
#include "core/Serve.h"

#include <cstdint>
#include <initializer_list>
#include <optional>
#include <string>
#include <string_view>

namespace VwCli::core::args {

// 失敗の detail の reason（呼ぶ側が分岐する綴り。docs/plugin/tools.md）。
inline constexpr std::string_view kNoDocument = "no_document";

// fail は status を失敗にする。detail は null（付けない）かオブジェクト。
void fail(ToolStatus &status, ErrorCode code, std::string message, Json detail = Json());

// failWithReason は status を internal の失敗にし、detail に {"reason":<reason>} を付ける。
void failWithReason(ToolStatus &status, std::string_view reason, std::string message);

// noDocument は図面が無いときの失敗（internal。reason は no_document）。
void noDocument(ToolStatus &status);

// Reader は 1 つの道具の引数を読む。最初の誤りで status を invalid_args にし、以後の読みは何も
// せずに既定の値を返す（最初に当たった誤りを返すため）。呼ぶ側は読み終えてから ok() を見る。
class Reader {
public:
	Reader(const Json &args, std::string_view tool, ToolStatus &status);

	// onlyKnown は、known（受ける引数の名前）にないキーがあれば誤りにする。
	void onlyKnown(std::initializer_list<std::string_view> known);

	// boolean は真偽値の引数。無ければ fallback。
	bool boolean(std::string_view key, bool fallback);

	// integer は整数の引数（値で読む。Json::integerValue）。範囲は [min, max] で、max は
	// kMaxIntegerArg 以下。無ければ空。
	std::optional<std::int64_t> integer(std::string_view key, std::int64_t min, std::int64_t max);

	// string は文字列の引数。無ければ空。
	std::optional<std::string> string(std::string_view key);

	// requiredString は必須の文字列の引数。無ければ誤り（空の文字列を返す）。
	std::string requiredString(std::string_view key);

	bool ok() const { return status_.code == ErrorCode::None; }

private:
	// find は key の値。無ければ nullptr（null は値として返し、型の違いとして断る）。
	const Json *find(std::string_view key) const { return args_.find(key); }
	void invalid(std::string message);

	const Json &args_;
	std::string tool_;
	ToolStatus &status_;
};

}
