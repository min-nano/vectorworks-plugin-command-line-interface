#include "core/Args.h"

#include <algorithm>

namespace VwCli::core::args {

void fail(ToolStatus &status, ErrorCode code, std::string message, Json detail) {
	status.code = code;
	status.message = std::move(message);
	status.detail = std::move(detail);
}

void failWithReason(ToolStatus &status, std::string_view reason, std::string message) {
	fail(status, ErrorCode::Internal, std::move(message),
	     Json::object({{"reason", Json::string(std::string(reason))}}));
}

void noDocument(ToolStatus &status) {
	failWithReason(status, kNoDocument, "図面が開かれていません");
}

Reader::Reader(const Json &args, std::string_view tool, ToolStatus &status)
    : args_(args), tool_(tool), status_(status) {}

void Reader::invalid(std::string message) {
	if (ok()) {
		fail(status_, ErrorCode::InvalidArgs, tool_ + " の引数が誤っています: " + message);
	}
}

void Reader::onlyKnown(std::initializer_list<std::string_view> known) {
	for (const Json::Member &member : args_.members()) {
		if (std::find(known.begin(), known.end(), member.first) == known.end()) {
			invalid("知らない引数 " + member.first);
			return;
		}
	}
}

bool Reader::boolean(std::string_view key, bool fallback) {
	const Json *value = find(key);
	if (!ok() || !value) {
		return fallback;
	}
	if (!value->isBool()) {
		invalid(std::string(key) + " は真偽値です");
		return fallback;
	}
	return value->asBool();
}

std::optional<std::int64_t> Reader::integer(std::string_view key, std::int64_t min,
                                            std::int64_t max) {
	const Json *value = find(key);
	if (!ok() || !value) {
		return std::nullopt;
	}
	std::string range = std::to_string(min) + " 以上 " + std::to_string(max) + " 以下";
	if (!value->isNumber()) {
		invalid(std::string(key) + " は " + range + "の整数です");
		return std::nullopt;
	}
	IntegerValue read = value->integerValue();
	if (read.check == IntegerCheck::Fraction) {
		invalid(std::string(key) + " は整数です（小数部があります: " + value->numberText() + "）");
		return std::nullopt;
	}
	if (read.check == IntegerCheck::OutOfRange || read.value < min || read.value > max) {
		invalid(std::string(key) + " は " + range + "です（" + value->numberText() + "）");
		return std::nullopt;
	}
	return read.value;
}

std::optional<std::string> Reader::string(std::string_view key) {
	const Json *value = find(key);
	if (!ok() || !value) {
		return std::nullopt;
	}
	if (!value->isString()) {
		invalid(std::string(key) + " は文字列です");
		return std::nullopt;
	}
	return value->asString();
}

std::string Reader::requiredString(std::string_view key) {
	if (ok() && !find(key)) {
		invalid(std::string(key) + " は必須です");
	}
	return string(key).value_or(std::string());
}

}
