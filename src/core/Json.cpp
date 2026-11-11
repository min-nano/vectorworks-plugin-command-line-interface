#include "core/Json.h"

#include <algorithm>
#include <charconv>
#include <cmath>
#include <system_error>
#include <type_traits>
#include <unordered_map>

namespace VwCli::core {

namespace {

constexpr char32_t kReplacement = 0xFFFD;

// 同じキーを並びの順に探すのは、キーがこの数より少ない間だけ。
constexpr std::size_t kLinearSearch = 16;

void appendUtf8(std::string &out, char32_t c) {
	if (c < 0x80) {
		out += static_cast<char>(c);
	} else if (c < 0x800) {
		out += static_cast<char>(0xC0 | (c >> 6));
		out += static_cast<char>(0x80 | (c & 0x3F));
	} else if (c < 0x10000) {
		out += static_cast<char>(0xE0 | (c >> 12));
		out += static_cast<char>(0x80 | ((c >> 6) & 0x3F));
		out += static_cast<char>(0x80 | (c & 0x3F));
	} else {
		out += static_cast<char>(0xF0 | (c >> 18));
		out += static_cast<char>(0x80 | ((c >> 12) & 0x3F));
		out += static_cast<char>(0x80 | ((c >> 6) & 0x3F));
		out += static_cast<char>(0x80 | (c & 0x3F));
	}
}

// utf8Length は text[at] から始まる UTF-8 の 1 文字の長さ。正しくなければ 0。
//
// Go の utf8.DecodeRune と同じ判定（長すぎる綴り・サロゲート・U+10FFFF を超えるものは誤り）。
// 誤りは 1 バイトずつ U+FFFD に置き換えるので、encoding/json と同じ数の U+FFFD になる。
std::size_t utf8Length(std::string_view text, std::size_t at) {
	auto byte = [&](std::size_t i) -> unsigned {
		return at + i < text.size() ? static_cast<unsigned char>(text[at + i]) : 0x100;
	};
	auto continuation = [&](std::size_t i, unsigned low = 0x80, unsigned high = 0xBF) {
		unsigned b = byte(i);
		return b >= low && b <= high;
	};
	unsigned b0 = byte(0);
	if (b0 < 0x80) {
		return 1;
	}
	if (b0 >= 0xC2 && b0 <= 0xDF) {
		return continuation(1) ? 2 : 0;
	}
	if (b0 >= 0xE0 && b0 <= 0xEF) {
		unsigned low = b0 == 0xE0 ? 0xA0 : 0x80;
		unsigned high = b0 == 0xED ? 0x9F : 0xBF;
		return continuation(1, low, high) && continuation(2) ? 3 : 0;
	}
	if (b0 >= 0xF0 && b0 <= 0xF4) {
		unsigned low = b0 == 0xF0 ? 0x90 : 0x80;
		unsigned high = b0 == 0xF4 ? 0x8F : 0xBF;
		return continuation(1, low, high) && continuation(2) && continuation(3) ? 4 : 0;
	}
	return 0;
}

// appendValidUtf8 は text[at] の 1 文字を out へ写し、進んだバイト数を返す。正しくない UTF-8 は
// U+FFFD にして 1 バイト進む。
std::size_t appendValidUtf8(std::string &out, std::string_view text, std::size_t at) {
	std::size_t length = utf8Length(text, at);
	if (length == 0) {
		appendUtf8(out, kReplacement);
		return 1;
	}
	out.append(text.substr(at, length));
	return length;
}

void dumpString(std::string &out, std::string_view text) {
	static constexpr char kHex[] = "0123456789abcdef";
	out += '"';
	for (std::size_t at = 0; at < text.size();) {
		unsigned char c = static_cast<unsigned char>(text[at]);
		if (c >= 0x80) {
			at += appendValidUtf8(out, text, at);
			continue;
		}
		switch (c) {
		case '"':
			out += "\\\"";
			break;
		case '\\':
			out += "\\\\";
			break;
		case '\b':
			out += "\\b";
			break;
		case '\f':
			out += "\\f";
			break;
		case '\n':
			out += "\\n";
			break;
		case '\r':
			out += "\\r";
			break;
		case '\t':
			out += "\\t";
			break;
		default:
			if (c < 0x20) {
				out += "\\u00";
				out += kHex[c >> 4];
				out += kHex[c & 0xF];
			} else {
				out += static_cast<char>(c);
			}
		}
		++at;
	}
	out += '"';
}

bool isDigit(char c) { return c >= '0' && c <= '9'; }

// 整数の引数の指数を飽和させる境（±2^31）。要求は 1 MiB までなので、仮数の桁の位置は ±2^20 程度に
// 収まり、指数がこれを超えれば仮数が 0 でない限り答えは決まる。int64 の端で飽和させないのは、
// そのあとで仮数の桁の位置を足すと溢れるため（docs/plugin/tools.md「最初に提供する道具」の冒頭）。
constexpr std::int64_t kExponentLimit = 2147483648;

// kMaxIntegerArg の桁の位置（2147483647 は 10 桁なので、最上位は 10^9 の位）。
constexpr std::int64_t kMaxIntegerPlace = 9;

}

// JsonParser は再帰下降で読む。深さは入れ子に入る前に数え、上限を超えたら降りずに失敗する。
class JsonParser {
public:
	JsonParser(std::string_view text, int maxDepth) : text_(text), maxDepth_(maxDepth) {}

	std::optional<Json> parse(std::string *error) {
		std::optional<Json> value;
		skipSpace();
		value = parseValue(0);
		if (value) {
			skipSpace();
			if (at_ != text_.size()) {
				fail("値のあとに余分な文字があります");
				value.reset();
			}
		}
		if (!value && error) {
			*error = std::to_string(failedAt_ + 1) + " バイト目: " + reason_;
		}
		return value;
	}

private:
	std::optional<Json> parseValue(int depth) {
		if (at_ >= text_.size()) {
			return fail("値がありません");
		}
		switch (text_[at_]) {
		case '{':
		case '[':
			// 入れ子に入る前に深さを数え、上限を超えたら降りない（オブジェクトと配列で同じ）。
			if (depth + 1 > maxDepth_) {
				return fail("入れ子が深すぎます（上限 " + std::to_string(maxDepth_) + "）");
			}
			return text_[at_] == '{' ? parseObject(depth + 1) : parseArray(depth + 1);
		case '"': {
			std::string text;
			if (!parseString(text)) {
				return std::nullopt;
			}
			return Json::string(std::move(text));
		}
		case 't':
			return parseLiteral("true", Json::boolean(true));
		case 'f':
			return parseLiteral("false", Json::boolean(false));
		case 'n':
			return parseLiteral("null", Json());
		default:
			return parseNumber();
		}
	}

	std::optional<Json> parseObject(int depth) {
		++at_;
		Json::Object members;
		// 同じキーを探す索引（キー → members の位置）。キーが少ないうちは並びを順に見るだけにし、
		// 増えたら作る（1 MiB の要求に何万ものキーを並べても、解析が 2 乗の時間にならないように）。
		std::unordered_map<std::string, std::size_t> index;
		skipSpace();
		if (consume('}')) {
			return Json(Json::Value(std::move(members)));
		}
		for (;;) {
			skipSpace();
			if (at_ >= text_.size() || text_[at_] != '"') {
				return fail("キーの文字列がありません");
			}
			std::string key;
			if (!parseString(key)) {
				return std::nullopt;
			}
			skipSpace();
			if (!consume(':')) {
				return fail("キーのあとに : がありません");
			}
			skipSpace();
			std::optional<Json> value = parseValue(depth);
			if (!value) {
				return std::nullopt;
			}
			// 同じキーは後のものを使う（encoding/json と同じ）。
			std::size_t found = members.size();
			if (members.size() < kLinearSearch) {
				for (std::size_t i = 0; i < members.size(); ++i) {
					if (members[i].first == key) {
						found = i;
						break;
					}
				}
			} else {
				if (index.empty()) {
					for (std::size_t i = 0; i < members.size(); ++i) {
						index.emplace(members[i].first, i);
					}
				}
				auto [entry, added] = index.emplace(key, members.size());
				found = added ? members.size() : entry->second;
			}
			if (found < members.size()) {
				members[found].second = std::move(*value);
			} else {
				members.emplace_back(std::move(key), std::move(*value));
			}
			skipSpace();
			if (consume('}')) {
				return Json(Json::Value(std::move(members)));
			}
			if (!consume(',')) {
				return fail("オブジェクトの , か } がありません");
			}
		}
	}

	std::optional<Json> parseArray(int depth) {
		++at_;
		Json::Array items;
		skipSpace();
		if (consume(']')) {
			return Json::array(std::move(items));
		}
		for (;;) {
			skipSpace();
			std::optional<Json> value = parseValue(depth);
			if (!value) {
				return std::nullopt;
			}
			items.push_back(std::move(*value));
			skipSpace();
			if (consume(']')) {
				return Json::array(std::move(items));
			}
			if (!consume(',')) {
				return fail("配列の , か ] がありません");
			}
		}
	}

	// parseString は text_[at_] の " から読み、閉じる " の次へ進む。
	bool parseString(std::string &out) {
		++at_;
		while (at_ < text_.size()) {
			unsigned char c = static_cast<unsigned char>(text_[at_]);
			if (c == '"') {
				++at_;
				return true;
			}
			if (c < 0x20) {
				fail("文字列に制御文字があります");
				return false;
			}
			if (c == '\\') {
				if (!parseEscape(out)) {
					return false;
				}
				continue;
			}
			if (c >= 0x80) {
				at_ += appendValidUtf8(out, text_, at_);
				continue;
			}
			out += static_cast<char>(c);
			++at_;
		}
		fail("文字列が閉じていません");
		return false;
	}

	bool parseEscape(std::string &out) {
		if (at_ + 1 >= text_.size()) {
			fail("エスケープが途切れています");
			return false;
		}
		char c = text_[at_ + 1];
		at_ += 2;
		switch (c) {
		case '"':
		case '\\':
		case '/':
			out += c;
			return true;
		case 'b':
			out += '\b';
			return true;
		case 'f':
			out += '\f';
			return true;
		case 'n':
			out += '\n';
			return true;
		case 'r':
			out += '\r';
			return true;
		case 't':
			out += '\t';
			return true;
		case 'u':
			break;
		default:
			at_ -= 2;
			fail("知らないエスケープです");
			return false;
		}
		std::optional<char32_t> unit = hex4(at_);
		if (!unit) {
			at_ -= 2;
			fail("\\u のあとに 16 進の 4 桁がありません");
			return false;
		}
		at_ += 4;
		char32_t code = *unit;
		if (code >= 0xD800 && code <= 0xDFFF) {
			// 高いサロゲートのすぐあとに低いサロゲートの \u があるときだけ組にする。それ以外は
			// U+FFFD とし、続くエスケープは別に読む（encoding/json と同じ）。
			std::optional<char32_t> low;
			if (code <= 0xDBFF && at_ + 1 < text_.size() && text_[at_] == '\\' &&
			    text_[at_ + 1] == 'u') {
				low = hex4(at_ + 2);
			}
			if (low && *low >= 0xDC00 && *low <= 0xDFFF) {
				code = 0x10000 + ((code - 0xD800) << 10) + (*low - 0xDC00);
				at_ += 6;
			} else {
				code = kReplacement;
			}
		}
		appendUtf8(out, code);
		return true;
	}

	std::optional<char32_t> hex4(std::size_t from) const {
		if (from + 4 > text_.size()) {
			return std::nullopt;
		}
		char32_t value = 0;
		for (std::size_t i = from; i < from + 4; ++i) {
			char c = text_[i];
			value <<= 4;
			if (c >= '0' && c <= '9') {
				value |= static_cast<char32_t>(c - '0');
			} else if (c >= 'a' && c <= 'f') {
				value |= static_cast<char32_t>(c - 'a' + 10);
			} else if (c >= 'A' && c <= 'F') {
				value |= static_cast<char32_t>(c - 'A' + 10);
			} else {
				return std::nullopt;
			}
		}
		return value;
	}

	// parseNumber は JSON の数の文法（-? (0 | [1-9][0-9]*) (.[0-9]+)? ([eE][+-]?[0-9]+)?）を
	// 確かめ、綴りのまま持つ。
	std::optional<Json> parseNumber() {
		std::size_t start = at_;
		consume('-');
		// 先頭の 0 のあとには数字を続けられない。
		if (!consume('0') && !skipDigits()) {
			return fail(start == at_ ? "値として読めない文字です" : "- のあとに数字がありません");
		}
		if (consume('.')) {
			if (!skipDigits()) {
				return fail("小数点のあとに数字がありません");
			}
		}
		if (at_ < text_.size() && (text_[at_] == 'e' || text_[at_] == 'E')) {
			++at_;
			if (!consume('+')) {
				consume('-');
			}
			if (!skipDigits()) {
				return fail("指数に数字がありません");
			}
		}
		Json number;
		number.value_ = Json::Number{std::string(text_.substr(start, at_ - start))};
		return number;
	}

	std::optional<Json> parseLiteral(std::string_view word, Json value) {
		if (text_.substr(at_, word.size()) != word) {
			return fail("値として読めない文字です");
		}
		at_ += word.size();
		return value;
	}

	bool skipDigits() {
		std::size_t start = at_;
		while (at_ < text_.size() && isDigit(text_[at_])) {
			++at_;
		}
		return at_ > start;
	}

	void skipSpace() {
		while (at_ < text_.size()) {
			char c = text_[at_];
			if (c != ' ' && c != '\t' && c != '\n' && c != '\r') {
				return;
			}
			++at_;
		}
	}

	bool consume(char c) {
		if (at_ < text_.size() && text_[at_] == c) {
			++at_;
			return true;
		}
		return false;
	}

	std::nullopt_t fail(std::string reason) {
		// 最初に見つけた誤りを残す（内側の誤りを外側が上書きしない）。
		if (reason_.empty()) {
			reason_ = std::move(reason);
			failedAt_ = at_;
		}
		return std::nullopt;
	}

	std::string_view text_;
	int maxDepth_;
	std::size_t at_ = 0;
	std::string reason_;
	std::size_t failedAt_ = 0;
};

Json Json::boolean(bool value) { return Json(Value(value)); }

Json Json::integer(std::int64_t value) {
	char buffer[24];
	auto [end, ec] = std::to_chars(buffer, buffer + sizeof buffer, value);
	(void)ec;
	return Json(Value(Number{std::string(buffer, end)}));
}

Json Json::real(double value) {
	if (!std::isfinite(value)) {
		return Json();
	}
	// 桁は精度を指定しない
	// to_chars（元の値に戻る最も短いもの）の指数の形から取る。書き方（固定小数点
	// か指数か・指数の綴り）は Go に合わせて組み立て直す。指数の形を選ぶのは、形を指定しない
	// to_chars は短いほうを選び（100000 が 1e+05 になる）、固定小数点は大きな値を最も短い桁では
	// なく正確な値で書く実装がある（999999999999999900000 が 999999999999999868928 になる）ため。
	char buffer[64];
	auto [end, ec] =
	    std::to_chars(buffer, buffer + sizeof buffer, value, std::chars_format::scientific);
	(void)ec;
	std::string_view shortest(buffer, static_cast<std::size_t>(end - buffer));
	bool negative = shortest.starts_with('-');
	// 仮数の数字・小数点の前の桁数・指数に分ける（d.ddde±XX）。
	std::string digits;
	std::int64_t point = 0;
	std::int64_t exponent = 0;
	bool afterPoint = false;
	std::size_t at = negative ? 1 : 0;
	for (; at < shortest.size() && shortest[at] != 'e'; ++at) {
		if (shortest[at] == '.') {
			afterPoint = true;
			continue;
		}
		digits += shortest[at];
		if (!afterPoint) {
			++point;
		}
	}
	if (at < shortest.size()) {
		++at;
		bool negativeExponent = shortest[at] == '-';
		if (shortest[at] == '-' || shortest[at] == '+') {
			++at;
		}
		for (; at < shortest.size(); ++at) {
			exponent = exponent * 10 + (shortest[at] - '0');
		}
		if (negativeExponent) {
			exponent = -exponent;
		}
	}
	std::string out = negative ? "-" : "";
	std::size_t lead = digits.find_first_not_of('0');
	if (lead == std::string::npos) {
		// 0 と -0（Go と同じく符号を残す）。
		out += '0';
		return Json(Value(Number{std::move(out)}));
	}
	digits.erase(0, lead);
	point -= static_cast<std::int64_t>(lead);
	digits.erase(digits.find_last_not_of('0') + 1);
	// 値は 0.<digits> × 10^decimalPoint、指数で書くときの指数は decimalPoint - 1。
	const std::int64_t decimalPoint = point + exponent;
	const std::int64_t scientific = decimalPoint - 1;
	const auto length = static_cast<std::int64_t>(digits.size());
	if (scientific >= -6 && scientific < 21) {
		if (decimalPoint <= 0) {
			out += "0.";
			out.append(static_cast<std::size_t>(-decimalPoint), '0');
			out += digits;
		} else if (decimalPoint >= length) {
			out += digits;
			out.append(static_cast<std::size_t>(decimalPoint - length), '0');
		} else {
			out.append(digits, 0, static_cast<std::size_t>(decimalPoint));
			out += '.';
			out.append(digits, static_cast<std::size_t>(decimalPoint));
		}
	} else {
		out += digits[0];
		if (digits.size() > 1) {
			out += '.';
			out.append(digits, 1);
		}
		out += scientific < 0 ? "e-" : "e+";
		out += std::to_string(scientific < 0 ? -scientific : scientific);
	}
	return Json(Value(Number{std::move(out)}));
}

Json Json::string(std::string value) { return Json(Value(std::move(value))); }

Json Json::array(Array items) { return Json(Value(std::move(items))); }

Json Json::object(Object members) {
	Json result{Value(Object{})};
	for (Member &member : members) {
		result.set(std::move(member.first), std::move(member.second));
	}
	return result;
}

std::optional<Json> Json::parse(std::string_view text, int maxDepth, std::string *error) {
	return JsonParser(text, maxDepth).parse(error);
}

Json::Type Json::type() const {
	// index をそのまま Type にするので、Type の並びを Value の候補の並びに縛る
	// （片方だけを並べ替えるとビルドが通らない）。
	constexpr auto at = [](Type type) { return static_cast<std::size_t>(type); };
	static_assert(std::variant_size_v<Value> == at(Type::Object) + 1);
	static_assert(
	    std::is_same_v<std::variant_alternative_t<at(Type::Null), Value>, std::monostate>);
	static_assert(std::is_same_v<std::variant_alternative_t<at(Type::Bool), Value>, bool>);
	static_assert(std::is_same_v<std::variant_alternative_t<at(Type::Number), Value>, Number>);
	static_assert(std::is_same_v<std::variant_alternative_t<at(Type::String), Value>, std::string>);
	static_assert(std::is_same_v<std::variant_alternative_t<at(Type::Array), Value>, Array>);
	static_assert(std::is_same_v<std::variant_alternative_t<at(Type::Object), Value>, Object>);
	return static_cast<Type>(value_.index());
}

bool Json::asBool() const { return std::get<bool>(value_); }

const std::string &Json::asString() const { return std::get<std::string>(value_); }

const std::string &Json::numberText() const { return std::get<Number>(value_).text; }

std::optional<std::int64_t> Json::asInt64() const {
	const std::string &text = numberText();
	std::int64_t value = 0;
	const char *end = text.data() + text.size();
	auto [stop, ec] = std::from_chars(text.data(), end, value);
	if (ec != std::errc() || stop != end) {
		return std::nullopt;
	}
	return value;
}

IntegerValue Json::integerValue() const {
	std::string_view text = numberText();
	std::size_t at = 0;
	bool negative = text.starts_with('-');
	if (negative) {
		++at;
	}
	std::size_t integerStart = at;
	while (at < text.size() && isDigit(text[at])) {
		++at;
	}
	std::string_view integerPart = text.substr(integerStart, at - integerStart);
	std::string_view fractionPart;
	if (at < text.size() && text[at] == '.') {
		std::size_t fractionStart = ++at;
		while (at < text.size() && isDigit(text[at])) {
			++at;
		}
		fractionPart = text.substr(fractionStart, at - fractionStart);
	}
	std::int64_t exponent = 0;
	if (at < text.size() && (text[at] == 'e' || text[at] == 'E')) {
		++at;
		bool negativeExponent = false;
		if (at < text.size() && (text[at] == '+' || text[at] == '-')) {
			negativeExponent = text[at] == '-';
			++at;
		}
		// 先頭の 0 は値を変えないので、そのまま数えてよい（0 のあいだは 0 のまま）。
		for (; at < text.size(); ++at) {
			exponent = std::min(exponent * 10 + (text[at] - '0'), kExponentLimit);
		}
		if (negativeExponent) {
			exponent = -exponent;
		}
	}

	// 仮数の数字を整数部と小数部をつないだ列として見る。i 番目の数字の位は
	// （整数部の桁数 - 1 - i）+ 指数。
	const auto integerDigits = static_cast<std::int64_t>(integerPart.size());
	const std::size_t count = integerPart.size() + fractionPart.size();
	auto digitAt = [&](std::size_t i) {
		return i < integerPart.size() ? integerPart[i] : fractionPart[i - integerPart.size()];
	};
	auto placeOf = [&](std::size_t i) {
		return integerDigits - 1 - static_cast<std::int64_t>(i) + exponent;
	};
	std::size_t first = 0;
	while (first < count && digitAt(first) == '0') {
		++first;
	}
	if (first == count) {
		// 値は 0（-0・0e99999999 も）。
		return {IntegerCheck::Integer, 0};
	}
	std::size_t last = count - 1;
	while (digitAt(last) == '0') {
		--last;
	}
	if (placeOf(last) < 0) {
		return {IntegerCheck::Fraction, 0};
	}
	if (negative || placeOf(first) > kMaxIntegerPlace) {
		return {IntegerCheck::OutOfRange, 0};
	}
	// ここでは数字は高々 10 桁で、最下位の位も 9 以下なので、int64 に収まる。
	std::int64_t value = 0;
	for (std::size_t i = first; i <= last; ++i) {
		value = value * 10 + (digitAt(i) - '0');
	}
	for (std::int64_t place = placeOf(last); place > 0; --place) {
		value *= 10;
	}
	if (value > kMaxIntegerArg) {
		return {IntegerCheck::OutOfRange, 0};
	}
	return {IntegerCheck::Integer, value};
}

const Json::Array &Json::items() const { return std::get<Array>(value_); }

const Json::Object &Json::members() const { return std::get<Object>(value_); }

const Json *Json::find(std::string_view key) const {
	const Object *members = std::get_if<Object>(&value_);
	if (!members) {
		return nullptr;
	}
	for (const Member &member : *members) {
		if (member.first == key) {
			return &member.second;
		}
	}
	return nullptr;
}

void Json::set(std::string key, Json value) {
	Object &members = std::get<Object>(value_);
	for (Member &member : members) {
		if (member.first == key) {
			member.second = std::move(value);
			return;
		}
	}
	members.emplace_back(std::move(key), std::move(value));
}

void Json::push(Json value) { std::get<Array>(value_).push_back(std::move(value)); }

std::string Json::dump() const {
	std::string out;
	dumpTo(out);
	return out;
}

void Json::dumpTo(std::string &out) const {
	switch (type()) {
	case Type::Null:
		out += "null";
		break;
	case Type::Bool:
		out += asBool() ? "true" : "false";
		break;
	case Type::Number:
		out += numberText();
		break;
	case Type::String:
		dumpString(out, asString());
		break;
	case Type::Array: {
		out += '[';
		bool first = true;
		for (const Json &item : items()) {
			if (!first) {
				out += ',';
			}
			first = false;
			item.dumpTo(out);
		}
		out += ']';
		break;
	}
	case Type::Object: {
		out += '{';
		bool first = true;
		for (const Member &member : members()) {
			if (!first) {
				out += ',';
			}
			first = false;
			dumpString(out, member.first);
			out += ':';
			member.second.dumpTo(out);
		}
		out += '}';
		break;
	}
	}
}

bool operator==(const Json &a, const Json &b) {
	if (a.type() != b.type()) {
		return false;
	}
	switch (a.type()) {
	case Json::Type::Null:
		return true;
	case Json::Type::Bool:
		return a.asBool() == b.asBool();
	case Json::Type::Number:
		return a.numberText() == b.numberText();
	case Json::Type::String:
		return a.asString() == b.asString();
	case Json::Type::Array:
		return a.items() == b.items();
	case Json::Type::Object: {
		if (a.members().size() != b.members().size()) {
			return false;
		}
		// キーは重ならないので、片側のキーをもう片側で引けば足りる。
		for (const Json::Member &member : a.members()) {
			const Json *other = b.find(member.first);
			if (!other || !(*other == member.second)) {
				return false;
			}
		}
		return true;
	}
	}
	return false;
}

}
